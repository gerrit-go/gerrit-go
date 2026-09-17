package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gerrit-go/internal/store"
)

// audit records a best-effort audit entry. Failures are ignored: auditing must
// never break the action it describes.
func (s *Server) audit(acct *store.Account, action, targetType, targetID, detail string) {
	var id int64
	if acct != nil {
		id = acct.ID
	}
	_ = s.db.AppendAudit(id, action, targetType, targetID, detail)
}

// ---------- checks ----------

func (s *Server) handleListCheckRuns(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	ps := s.parsePS(r, c.CurrentPS)
	runs, err := s.db.ListCheckRuns(c.Number, ps)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []*store.CheckRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleUpsertCheckRun(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	ps := s.parsePS(r, c.CurrentPS)
	var req struct {
		Name     string `json:"check_name"`
		State    string `json:"state"`
		URL      string `json:"url"`
		Message  string `json:"message"`
		Started  string `json:"started"`
		Finished string `json:"finished"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "check_name is required")
		return
	}
	state := strings.ToUpper(strings.TrimSpace(req.State))
	switch state {
	case "":
		state = "NOT_STARTED"
	case "NOT_STARTED", "SCHEDULED", "RUNNING", "SUCCESSFUL", "FAILED":
	default:
		writeErr(w, http.StatusBadRequest, "state must be one of NOT_STARTED, SCHEDULED, RUNNING, SUCCESSFUL, FAILED")
		return
	}
	run := &store.CheckRun{
		ChangeNumber: c.Number,
		PatchSet:     ps,
		Name:         name,
		State:        state,
		URL:          req.URL,
		Message:      req.Message,
		Started:      req.Started,
		Finished:     req.Finished,
	}
	if err := s.db.UpsertCheckRun(run); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "check-upsert", "change", strconv.FormatInt(c.Number, 10), name+"="+state)
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleDeleteCheckRun(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	ps := s.parsePS(r, c.CurrentPS)
	name := r.PathValue("name")
	if err := s.db.DeleteCheckRun(c.Number, ps, name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "check-delete", "change", strconv.FormatInt(c.Number, 10), name)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- webhooks ----------

func (s *Server) handleListProjectWebhooks(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.canEditAccess(acct, project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	hooks, err := s.db.ListWebhooksForProject(project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hooksOrEmpty(hooks))
}

func (s *Server) handleCreateProjectWebhook(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.canEditAccess(acct, project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	hook, ok := s.decodeWebhook(w, r, project)
	if !ok {
		return
	}
	created, err := s.db.CreateWebhook(hook)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "webhook-create", "project", project, created.URL)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteProjectWebhook(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if !s.canEditAccess(acct, project) {
		s.forbid(w, r, PermEditAccess)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	if err := s.db.DeleteWebhook(project, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "webhook-delete", "project", project, strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGlobalWebhooks(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	hooks, err := s.db.ListGlobalWebhooks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hooksOrEmpty(hooks))
}

func (s *Server) handleCreateGlobalWebhook(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	hook, ok := s.decodeWebhook(w, r, "")
	if !ok {
		return
	}
	created, err := s.db.CreateWebhook(hook)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "webhook-create", "global", "", created.URL)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteGlobalWebhook(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	if err := s.db.DeleteWebhook("", id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "webhook-delete", "global", "", strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}

// decodeWebhook parses a webhook create body. events defaults to ["*"]; active
// defaults to true when omitted.
func (s *Server) decodeWebhook(w http.ResponseWriter, r *http.Request, project string) (*store.Webhook, bool) {
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
		Active *bool    `json:"active"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	url := strings.TrimSpace(req.URL)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		writeErr(w, http.StatusBadRequest, "url must be an http(s) endpoint")
		return nil, false
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	events := req.Events
	if len(events) == 0 {
		events = []string{"*"}
	}
	return &store.Webhook{Project: project, URL: url, Events: events, Secret: req.Secret, Active: active}, true
}

func hooksOrEmpty(hooks []*store.Webhook) []*store.Webhook {
	if hooks == nil {
		return []*store.Webhook{}
	}
	return hooks
}

// ---------- audit log ----------

// backupStatus tracks the asynchronous repository backup's state so a large
// backup does not block the HTTP request that triggered it.
type backupStatus struct {
	mu       sync.Mutex
	Running  bool      `json:"running"`
	Archive  string    `json:"archive,omitempty"`
	Error    string    `json:"error,omitempty"`
	Finished time.Time `json:"finished,omitempty"`
}

func (b *backupStatus) start() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Running {
		return false
	}
	b.Running = true
	b.Archive = ""
	b.Error = ""
	return true
}

func (b *backupStatus) finish(archive string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Running = false
	b.Archive = archive
	if err != nil {
		b.Error = err.Error()
	}
	b.Finished = time.Now()
}

func (b *backupStatus) snapshot() backupStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return *b
}

// handleBackup starts a repository backup in the background (admin only) and
// returns immediately; poll /admin/backup/status for the result.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	if !s.backupState.start() {
		writeJSON(w, http.StatusAccepted, map[string]any{"started": false, "running": true})
		return
	}
	go func() {
		path, err := s.git.BackupRepos()
		s.backupState.finish(path, err)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// handleBackupStatus reports the current or last backup state (admin only).
func (s *Server) handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	writeJSON(w, http.StatusOK, s.backupState.snapshot())
}

// handleListBackups lists existing backup archives (admin only).
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	list, err := s.git.ListBackups()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleBackfillFiles backfills patchset_files for older patch sets (admin only).
func (s *Server) handleBackfillFiles(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	filled, err := s.git.BackfillPatchSetFiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backfilled": filled})
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "administrateServer")
		return
	}
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 {
		limit = n
	}
	offset := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("start")); err == nil && n > 0 {
		offset = n
	}
	entries, users, total, err := s.db.ListAudit(limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type row struct {
		*store.AuditEntry
		Username string `json:"username,omitempty"`
	}
	out := make([]row, 0, len(entries))
	for i, e := range entries {
		out = append(out, row{AuditEntry: e, Username: users[i]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "total": total})
}
