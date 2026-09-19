package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/pipeline"
	"gerrit-go/internal/store"
)

// CI wiring: pipelines are project-scoped definitions that run on patch-set
// uploads. Results are reported as check runs plus an optional label vote, so
// the existing Checks card and submit-requirement gating need no changes.

const (
	// CIBotUsername is the account that casts CI votes and check runs.
	CIBotUsername   = "ci-bot"
	ciGlobalProject = "*"
	ciMaxSteps      = 50
	ciMaxStepsByte  = 64 << 10
	ciMaxEnvByte    = 8 << 10
	ciRunTimeout    = 30 * time.Minute
)

var errBadPipelineName = errors.New("name must use letters, digits, '-', '_' or '.'")

// setCIExecRunner swaps in the real shell executor. Only called when the server
// was explicitly started with -ci-exec, because it runs pipeline steps as actual
// commands on this host.
func (s *Server) EnableCIExec(timeout time.Duration) {
	s.ci = pipeline.NewService(s.db, pipeline.ExecRunner{StepTimeout: timeout}, slog.Default())
	slog.Info("ci: exec runner enabled (runs pipeline steps on this host)")
}

// ciRunnerName reports the active executor for /config output.
func (s *Server) ciRunnerName() string {
	if s.ci == nil {
		return ""
	}
	return s.ci.RunnerName()
}

// ciActor lazily provisions the CI bot account; 0 means "cannot vote".
func (s *Server) ciActor() int64 {
	if a, err := s.db.GetAccountByUsername(CIBotUsername); err == nil {
		return a.ID
	}
	a := &store.Account{
		Username: CIBotUsername,
		FullName: "CI Bot",
		Email:    CIBotUsername + "@localhost",
	}
	if err := s.db.CreateAccount(a); err != nil {
		if again, err2 := s.db.GetAccountByUsername(CIBotUsername); err2 == nil {
			return again.ID
		}
		slog.Warn("ci: cannot create bot account", "err", err)
		return 0
	}
	return a.ID
}

// triggerCI runs every enabled pipeline for a change's latest patch set. It is
// called from a background goroutine: CI must never break the review path.
func (s *Server) triggerCI(changeNumber int64) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("ci: panic while triggering", "change", changeNumber, "panic", rec)
		}
	}()
	if s.ci == nil {
		return
	}
	c, err := s.db.GetChange(changeNumber)
	if err != nil {
		return
	}
	cfgs, err := s.pipelinesForProject(c.Project)
	if err != nil || len(cfgs) == 0 {
		return
	}
	if c.CurrentPS < 1 {
		c.CurrentPS = 1
	}
	actor := s.ciActor()
	repoDir := s.git.RepoDir(c.Project)
	for _, cfg := range cfgs {
		s.runPipeline(cfg, c, actor, repoDir)
	}
}

// pipelinesForProject returns enabled configs triggered by patchset-created for a
// project, including the shared "*" definitions.
func (s *Server) pipelinesForProject(project string) ([]*store.PipelineConfig, error) {
	scoped, err := s.db.ListEnabledConfigsForTrigger(project, "patchset-created")
	if err != nil {
		return nil, err
	}
	if project == ciGlobalProject {
		return scoped, nil
	}
	global, err := s.db.ListEnabledConfigsForTrigger(ciGlobalProject, "patchset-created")
	if err != nil {
		return scoped, nil
	}
	return append(scoped, global...), nil
}

func (s *Server) runPipeline(cfg *store.PipelineConfig, c *store.Change, actorID int64, repoDir string) {
	ctx, cancel := context.WithTimeout(context.Background(), ciRunTimeout)
	defer cancel()
	ref := fmt.Sprintf("refs/changes/%02d/%d/%d", c.Number%100, c.Number, c.CurrentPS)
	run, err := s.ci.Execute(ctx, cfg, pipeline.Input{
		ChangeNumber: c.Number,
		PatchSet:     c.CurrentPS,
		Project:      c.Project,
		ActorID:      actorID,
		RepoPath:     repoDir,
		Ref:          ref,
	})
	if err != nil {
		slog.Warn("ci: run failed", "pipeline", cfg.Name, "change", c.Number, "err", err)
		return
	}
	slog.Info("ci: run finished", "pipeline", cfg.Name, "change", c.Number,
		"patch_set", c.CurrentPS, "status", run.Status)
}

// ---------- validation ----------

func validPipelineIdent(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, ru := range name {
		ok := ru == '-' || ru == '_' || ru == '.' ||
			(ru >= 'a' && ru <= 'z') || (ru >= 'A' && ru <= 'Z') || (ru >= '0' && ru <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func (s *Server) normalizePipelineConfig(c *store.PipelineConfig) error {
	c.Name = strings.TrimSpace(c.Name)
	c.Project = strings.TrimSpace(c.Project)
	if !validPipelineIdent(c.Name) {
		return errBadPipelineName
	}
	if c.Project != ciGlobalProject {
		if _, err := s.db.GetProject(c.Project); err != nil {
			return errors.New("project not found")
		}
	}
	if len(c.Steps) == 0 {
		return errors.New("at least one step is required")
	}
	if len(c.Steps) > ciMaxSteps {
		return fmt.Errorf("at most %d steps", ciMaxSteps)
	}
	total := 0
	for i, step := range c.Steps {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("step %d is empty", i+1)
		}
		total += len(step)
	}
	if total > ciMaxStepsByte {
		return fmt.Errorf("steps exceed %d bytes in total", ciMaxStepsByte)
	}
	if len(c.Triggers) == 0 {
		c.Triggers = []string{"patchset-created"}
	}
	envBytes := 0
	for k, v := range c.Env {
		if strings.TrimSpace(k) == "" {
			return errors.New("env keys must not be empty")
		}
		envBytes += len(k) + len(v)
	}
	if envBytes > ciMaxEnvByte {
		return errors.New("env exceeds size limit")
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
	return nil
}

// canManagePipelines gates pipeline configuration: a form of project
// administration, so it follows the editAccess permission.
func (s *Server) canManagePipelines(acct *store.Account, project string) bool {
	return s.canEditAccess(acct, project)
}

// ---------- handlers ----------

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project == "" && !acct.Admin {
		writeErr(w, http.StatusBadRequest, "project is required")
		return
	}
	if project != "" && !s.canManagePipelines(acct, project) && !acct.Admin {
		s.forbid(w, r, PermEditAccess)
		return
	}
	cfgs, err := s.db.ListPipelineConfigs(project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfgs == nil {
		cfgs = []*store.PipelineConfig{}
	}
	writeJSON(w, http.StatusOK, cfgs)
}

func (s *Server) pipelineByIDOr404(w http.ResponseWriter, idRaw string) (*store.PipelineConfig, bool) {
	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid pipeline id")
		return nil, false
	}
	cfg, err := s.db.GetPipelineConfig(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "pipeline not found")
		return nil, false
	}
	return cfg, true
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	cfg, ok := s.pipelineByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManagePipelines(acct, cfg.Project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func pipelineRequestBody(r *http.Request) (*store.PipelineConfig, error) {
	var req struct {
		Project   string            `json:"project"`
		Name      string            `json:"name"`
		Triggers  []string          `json:"triggers"`
		Steps     []string          `json:"steps"`
		Env       map[string]string `json:"env"`
		VoteLabel string            `json:"vote_label"`
		PassVote  int               `json:"pass_vote"`
		FailVote  int               `json:"fail_vote"`
		Required  bool              `json:"required"`
		Enabled   bool              `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return nil, errors.New("invalid request body")
	}
	return &store.PipelineConfig{
		Project: req.Project, Name: req.Name, Triggers: req.Triggers, Steps: req.Steps,
		Env: req.Env, VoteLabel: strings.TrimSpace(req.VoteLabel),
		PassVote: req.PassVote, FailVote: req.FailVote,
		Required: req.Required, Enabled: req.Enabled,
	}, nil
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	cfg, err := pipelineRequestBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if cfg.PassVote == 0 && cfg.FailVote == 0 {
		cfg.PassVote, cfg.FailVote = 1, -1
	}
	if err := s.normalizePipelineConfig(cfg); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.canManagePipelines(acct, cfg.Project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	if err := s.db.CreatePipelineConfig(cfg); err != nil {
		writeErr(w, http.StatusConflict, "pipeline name already exists in this project")
		return
	}
	s.audit(acct, "pipeline-create", "pipeline", cfg.Project+"/"+cfg.Name, fmt.Sprintf("steps=%d", len(cfg.Steps)))
	writeJSON(w, http.StatusCreated, cfg)
}

func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	cfg, ok := s.pipelineByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManagePipelines(acct, cfg.Project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	patch, err := pipelineRequestBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Project and identity stay fixed; only behaviour is editable.
	patch.ID = cfg.ID
	patch.Project = cfg.Project
	if patch.PassVote == 0 && patch.FailVote == 0 {
		patch.PassVote, patch.FailVote = cfg.PassVote, cfg.FailVote
	}
	if err := s.normalizePipelineConfig(patch); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.UpdatePipelineConfig(patch); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	patch.Created = cfg.Created
	s.audit(acct, "pipeline-update", "pipeline", cfg.Project+"/"+cfg.Name, "")
	writeJSON(w, http.StatusOK, patch)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	cfg, ok := s.pipelineByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManagePipelines(acct, cfg.Project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	if err := s.db.DeletePipelineConfig(cfg.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "pipeline-delete", "pipeline", cfg.Project+"/"+cfg.Name, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	cfg, ok := s.pipelineByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManagePipelines(acct, cfg.Project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.db.ListPipelineRunsByConfig(cfg.ID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []*store.PipelineRun{}
	}
	for _, run := range runs { // logs belong to the detail endpoint
		run.Log = ""
	}
	writeJSON(w, http.StatusOK, runs)
}

// handleGetPipelineRun returns one run including its full log, for anyone who can
// read the change it belongs to.
func (s *Server) handleGetPipelineRun(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	run, err := s.db.GetPipelineRun(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	c, err := s.db.GetChange(run.ChangeNumber)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if !s.canReadChange(acct, c) {
		s.forbid(w, r, PermRead)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleListChangePipelines lists the current patch set's runs for a change.
func (s *Server) handleListChangePipelines(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	num, err := strconv.ParseInt(r.PathValue("num"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if !s.canReadChange(acct, c) {
		s.forbid(w, r, PermRead)
		return
	}
	ps := c.CurrentPS
	if ps < 1 {
		ps = 1
	}
	runs, err := s.db.ListPipelineRunsForPatchSet(num, ps)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []*store.PipelineRun{}
	}
	for _, run := range runs {
		run.Log = ""
	}
	writeJSON(w, http.StatusOK, runs)
}

// handleTriggerPipeline re-runs a pipeline against an existing change.
func (s *Server) handleTriggerPipeline(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	cfg, ok := s.pipelineByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManagePipelines(acct, cfg.Project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	var req struct {
		ChangeNumber int64 `json:"change_number"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ChangeNumber <= 0 {
		writeErr(w, http.StatusBadRequest, "change_number is required")
		return
	}
	c, err := s.db.GetChange(req.ChangeNumber)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if c.CurrentPS < 1 {
		c.CurrentPS = 1
	}
	go s.runPipeline(cfg, c, s.ciActor(), s.git.RepoDir(c.Project))
	s.audit(acct, "pipeline-trigger", "pipeline", cfg.Project+"/"+cfg.Name, fmt.Sprintf("change=%d", c.Number))
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "change_number": c.Number, "patch_set": c.CurrentPS})
}
