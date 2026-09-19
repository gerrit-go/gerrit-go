// Package pipeline implements the built-in CI engine: it executes a project's
// pipeline configurations against a change's patch set and reports the outcome
// back into the systems the review flow already understands — a check run (for
// the Checks UI) and an optional label vote (for submit-requirement gating).
package pipeline

import (
	"context"
	"log/slog"
	"time"

	"gerrit-go/internal/store"
)

// Job describes one execution request handed to a Runner.
type Job struct {
	Config       *store.PipelineConfig
	ChangeNumber int64
	PatchSet     int
	Project      string
	// RepoPath is a local (bare) repository path to seed the workspace from.
	// Empty means the runner works in an empty directory.
	RepoPath string
	// Ref is the git ref or commit to check out inside the workspace.
	Ref string
}

// Outcome is what a Runner reports back.
type Outcome struct {
	Status   string // store.Run* value
	Log      string
	Started  string
	Finished string
}

// Runner executes pipeline steps. Implementations must be safe for concurrent
// use; they are called from background goroutines.
type Runner interface {
	Name() string
	Run(ctx context.Context, job Job) Outcome
}

// Input identifies the change/patch set to build and who to vote as.
type Input struct {
	ChangeNumber int64
	PatchSet     int
	Project      string
	// ActorID is the account used for the check run's vote. Callers resolve a CI
	// bot account here; 0 skips voting.
	ActorID  int64
	RepoPath string
	Ref      string
}

// Service turns a config + change into a recorded run and feeds back results.
type Service struct {
	db     *store.DB
	runner Runner
	log    *slog.Logger
}

func NewService(db *store.DB, runner Runner, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: db, runner: runner, log: log}
}

// RunnerName reports which executor the service is wired to.
func (s *Service) RunnerName() string { return s.runner.Name() }

// Execute creates a run for cfg against in.ChangeNumber/in.PatchSet, runs it to
// completion, and mirrors the outcome into a check run plus an optional label
// vote. It is blocking; call it from a goroutine for background triggers.
func (s *Service) Execute(ctx context.Context, cfg *store.PipelineConfig, in Input) (*store.PipelineRun, error) {
	run := &store.PipelineRun{
		ConfigID:     cfg.ID,
		ChangeNumber: in.ChangeNumber,
		PatchSet:     in.PatchSet,
		Project:      in.Project,
		Status:       store.RunQueued,
		Runner:       s.runner.Name(),
	}
	if err := s.db.CreatePipelineRun(run); err != nil {
		return nil, err
	}

	started := time.Now().UTC().Format(time.RFC3339Nano)
	run.Status = store.RunRunning
	run.Started = started
	if err := s.db.UpdatePipelineRun(run); err != nil {
		return nil, err
	}
	s.writeCheck(cfg, run, "RUNNING", started, "")

	outcome := s.runner.Run(ctx, Job{
		Config:       cfg,
		ChangeNumber: in.ChangeNumber,
		PatchSet:     in.PatchSet,
		Project:      in.Project,
		RepoPath:     in.RepoPath,
		Ref:          in.Ref,
	})
	// Preserve whatever timestamps the runner produced.
	if outcome.Started != "" {
		run.Started = outcome.Started
	}
	if outcome.Finished != "" {
		run.Finished = outcome.Finished
	} else {
		run.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	}
	run.Status = outcome.Status
	run.Log = outcome.Log
	if err := s.db.UpdatePipelineRun(run); err != nil {
		s.log.Warn("pipeline: failed to persist run", "run", run.ID, "err", err)
	}

	s.writeCheck(cfg, run, checkState(run.Status), run.Started, run.Finished)
	s.castVote(cfg, run, in.ActorID)
	return run, nil
}

// writeCheck mirrors a run into the check_runs table so the existing Checks card
// shows CI without any new UI plumbing.
func (s *Service) writeCheck(cfg *store.PipelineConfig, run *store.PipelineRun, state, started, finished string) {
	cr := &store.CheckRun{
		ChangeNumber: run.ChangeNumber,
		PatchSet:     run.PatchSet,
		Name:         cfg.Name,
		State:        state,
		Message:      "pipeline " + cfg.Name + ": " + run.Status,
		Started:      started,
		Finished:     finished,
	}
	if err := s.db.UpsertCheckRun(cr); err != nil {
		s.log.Warn("pipeline: check run write failed", "change", run.ChangeNumber, "err", err)
	}
}

// castVote records the configured label vote so submit requirements can gate on
// CI. A change message/timeline entry is left to the API layer that owns review
// semantics.
func (s *Service) castVote(cfg *store.PipelineConfig, run *store.PipelineRun, actorID int64) {
	if cfg.VoteLabel == "" || actorID == 0 {
		return
	}
	value := cfg.FailVote
	if run.Status == store.RunSuccess {
		value = cfg.PassVote
	}
	v := &store.Vote{
		ChangeNumber: run.ChangeNumber,
		PatchSet:     run.PatchSet,
		AccountID:    actorID,
		Label:        cfg.VoteLabel,
		Value:        value,
	}
	if err := s.db.SetVote(v); err != nil {
		s.log.Warn("pipeline: vote failed", "change", run.ChangeNumber, "label", cfg.VoteLabel, "err", err)
	}
}

// checkState maps internal run status onto the Gerrit vocabulary the Checks UI
// renders (NOT_STARTED | SCHEDULED | RUNNING | SUCCESSFUL | FAILED).
func checkState(status string) string {
	switch status {
	case store.RunQueued:
		return "SCHEDULED"
	case store.RunRunning:
		return "RUNNING"
	case store.RunSuccess:
		return "SUCCESSFUL"
	case store.RunCanceled:
		return "NOT_STARTED"
	default: // FAILURE, ERROR
		return "FAILED"
	}
}
