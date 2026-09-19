package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gerrit-go/internal/store"
)

func newTestDB(t *testing.T) (*store.DB, int64, int64) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "ci.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.CreateProject(&store.Project{Name: "grp/proj", Description: "d"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	bot := &store.Account{Username: "ci-bot", FullName: "CI Bot", Email: "ci@example.com"}
	if err := db.CreateAccount(bot); err != nil {
		t.Fatalf("create account: %v", err)
	}
	change := &store.Change{Project: "grp/proj", Branch: "main", ChangeID: "I123", Subject: "s", OwnerID: bot.ID, CurrentPS: 1}
	if err := db.CreateChange(change); err != nil {
		t.Fatalf("create change: %v", err)
	}
	return db, bot.ID, change.Number
}

func seedConfig(t *testing.T, db *store.DB, steps []string, voteLabel string) *store.PipelineConfig {
	t.Helper()
	cfg := &store.PipelineConfig{
		Project:   "grp/proj",
		Name:      "build",
		Triggers:  []string{"patchset-created"},
		Steps:     steps,
		Env:       map[string]string{"CI": "true"},
		VoteLabel: voteLabel,
		PassVote:  1,
		FailVote:  -1,
		Enabled:   true,
	}
	if err := db.CreatePipelineConfig(cfg); err != nil {
		t.Fatalf("create config: %v", err)
	}
	return cfg
}

func findCheck(t *testing.T, db *store.DB, change int64, name string) *store.CheckRun {
	t.Helper()
	runs, err := db.ListCheckRuns(change, 1)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	for _, c := range runs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestServiceSuccessWritesCheckAndVote(t *testing.T) {
	db, botID, changeNum := newTestDB(t)
	cfg := seedConfig(t, db, []string{"make", "make test"}, "Verified")
	svc := NewService(db, SimulatedRunner{}, nil)

	run, err := svc.Execute(context.Background(), cfg, Input{
		ChangeNumber: changeNum, PatchSet: 1, Project: "grp/proj", ActorID: botID,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != store.RunSuccess {
		t.Fatalf("want SUCCESS, got %s (log: %s)", run.Status, run.Log)
	}
	// Run row persisted with log + timings.
	stored, err := db.GetPipelineRun(run.ID)
	if err != nil || stored.Log == "" || stored.Started == "" || stored.Finished == "" {
		t.Fatalf("run not persisted: %+v err=%v", stored, err)
	}
	// Check run uses the Gerrit state the UI renders.
	cr := findCheck(t, db, changeNum, "build")
	if cr == nil || cr.State != "SUCCESSFUL" {
		t.Fatalf("expected SUCCESSFUL check run, got %+v", cr)
	}
	votes, err := db.ListVotes(changeNum)
	if err != nil || len(votes) != 1 || votes[0].Label != "Verified" || votes[0].Value != 1 {
		t.Fatalf("expected Verified +1, got %+v err=%v", votes, err)
	}
}

func TestServiceFailureWritesFailedCheckAndNegativeVote(t *testing.T) {
	db, botID, changeNum := newTestDB(t)
	cfg := seedConfig(t, db, []string{"make", FailDirective}, "Verified")
	svc := NewService(db, SimulatedRunner{}, nil)

	run, err := svc.Execute(context.Background(), cfg, Input{
		ChangeNumber: changeNum, PatchSet: 1, Project: "grp/proj", ActorID: botID,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != store.RunFailure {
		t.Fatalf("want FAILURE, got %s", run.Status)
	}
	cr := findCheck(t, db, changeNum, "build")
	if cr == nil || cr.State != "FAILED" {
		t.Fatalf("expected FAILED check run, got %+v", cr)
	}
	votes, _ := db.ListVotes(changeNum)
	if len(votes) != 1 || votes[0].Value != -1 {
		t.Fatalf("expected Verified -1, got %+v", votes)
	}
	// Re-running the same config+patchset updates the same check row (no dupes).
	if _, err := svc.Execute(context.Background(), cfg, Input{
		ChangeNumber: changeNum, PatchSet: 1, Project: "grp/proj", ActorID: botID,
	}); err != nil {
		t.Fatalf("second execute: %v", err)
	}
	runs, _ := db.ListCheckRuns(changeNum, 1)
	n := 0
	for _, c := range runs {
		if c.Name == "build" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("check run should be upserted, got %d rows named build", n)
	}
}

func TestServiceNoVoteWithoutLabelOrActor(t *testing.T) {
	db, botID, changeNum := newTestDB(t)
	cfg := seedConfig(t, db, []string{"make"}, "") // no vote label
	svc := NewService(db, SimulatedRunner{}, nil)
	if _, err := svc.Execute(context.Background(), cfg, Input{
		ChangeNumber: changeNum, PatchSet: 1, Project: "grp/proj", ActorID: botID,
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if votes, _ := db.ListVotes(changeNum); len(votes) != 0 {
		t.Fatalf("no vote expected when vote_label empty, got %+v", votes)
	}

	db2, _, change2 := newTestDB(t)
	cfg2 := seedConfig(t, db2, []string{"make"}, "Verified")
	svc2 := NewService(db2, SimulatedRunner{}, nil)
	if _, err := svc2.Execute(context.Background(), cfg2, Input{
		ChangeNumber: change2, PatchSet: 1, Project: "grp/proj", ActorID: 0, // no actor
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if votes, _ := db2.ListVotes(change2); len(votes) != 0 {
		t.Fatalf("no vote expected without actor, got %+v", votes)
	}
	if cr := findCheck(t, db2, change2, "build"); cr == nil || cr.State != "SUCCESSFUL" {
		t.Fatalf("check run should still be written, got %+v", cr)
	}
}

func TestExecRunnerRunsRealSteps(t *testing.T) {
	db, _, changeNum := newTestDB(t)
	cfg := seedConfig(t, db, []string{"echo hello-ci", "test -d ."}, "")
	svc := NewService(db, ExecRunner{StepTimeout: 30 * time.Second}, nil)

	run, err := svc.Execute(context.Background(), cfg, Input{
		ChangeNumber: changeNum, PatchSet: 1, Project: "grp/proj",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != store.RunSuccess || !strings.Contains(run.Log, "hello-ci") {
		t.Fatalf("exec runner should succeed and capture output: %+v", run)
	}
}

func TestExecRunnerReportsFailingStep(t *testing.T) {
	db, _, changeNum := newTestDB(t)
	cfg := seedConfig(t, db, []string{"exit 3"}, "")
	svc := NewService(db, ExecRunner{StepTimeout: 30 * time.Second}, nil)
	run, err := svc.Execute(context.Background(), cfg, Input{
		ChangeNumber: changeNum, PatchSet: 1, Project: "grp/proj",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != store.RunFailure {
		t.Fatalf("want FAILURE for exit 3, got %s", run.Status)
	}
}
