package store

import "testing"

func TestPipelineConfigCRUD(t *testing.T) {
	db := openTemp(t)
	c := &PipelineConfig{
		Project:   "rk/Linux/kernel",
		Name:      "build",
		Triggers:  []string{"patchset-created"},
		Steps:     []string{"make defconfig", "make -j4"},
		Env:       map[string]string{"CC": "gcc"},
		VoteLabel: "Verified",
		PassVote:  1,
		FailVote:  -1,
		Required:  true,
		Enabled:   true,
	}
	if err := db.CreatePipelineConfig(c); err != nil {
		t.Fatalf("create config: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("config id not set")
	}
	got, err := db.GetPipelineConfig(c.ID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got.Name != "build" || len(got.Steps) != 2 || got.Env["CC"] != "gcc" || !got.Enabled || !got.Required {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.VoteLabel != "Verified" || got.PassVote != 1 || got.FailVote != -1 {
		t.Fatalf("vote fields lost: %+v", got)
	}

	byName, err := db.GetPipelineConfigByName("rk/Linux/kernel", "build")
	if err != nil || byName.ID != c.ID {
		t.Fatalf("get by name: %v err=%v", byName, err)
	}

	// Duplicate (project,name) must be rejected.
	if err := db.CreatePipelineConfig(&PipelineConfig{Project: "rk/Linux/kernel", Name: "build", Enabled: true}); err == nil {
		t.Fatal("expected unique-violation on duplicate (project,name)")
	}

	got.Name = "build-ci"
	got.Steps = []string{"make"}
	got.Enabled = false
	if err := db.UpdatePipelineConfig(got); err != nil {
		t.Fatalf("update config: %v", err)
	}
	after, err := db.GetPipelineConfig(c.ID)
	if err != nil || after.Name != "build-ci" || after.Enabled {
		t.Fatalf("update not persisted: %+v err=%v", after, err)
	}
	if err := db.DeletePipelineConfig(c.ID); err != nil {
		t.Fatalf("delete config: %v", err)
	}
	if _, err := db.GetPipelineConfig(c.ID); err == nil {
		t.Fatal("config should be gone")
	}
}

func TestPipelineTriggerFilter(t *testing.T) {
	db := openTemp(t)
	// enabled, matches
	db.CreatePipelineConfig(&PipelineConfig{Project: "p1", Name: "ok",
		Triggers: []string{"patchset-created"}, Steps: []string{"x"}, Enabled: true})
	// enabled but different trigger
	db.CreatePipelineConfig(&PipelineConfig{Project: "p1", Name: "other",
		Triggers: []string{"change-merged"}, Steps: []string{"x"}, Enabled: true})
	// wildcard trigger
	db.CreatePipelineConfig(&PipelineConfig{Project: "p1", Name: "any",
		Triggers: []string{"*"}, Steps: []string{"x"}, Enabled: true})
	// disabled
	db.CreatePipelineConfig(&PipelineConfig{Project: "p1", Name: "off",
		Triggers: []string{"patchset-created"}, Steps: []string{"x"}, Enabled: false})

	matches, err := db.ListEnabledConfigsForTrigger("p1", "patchset-created")
	if err != nil {
		t.Fatalf("trigger filter: %v", err)
	}
	names := map[string]bool{}
	for _, c := range matches {
		names[c.Name] = true
	}
	if !names["ok"] || !names["any"] {
		t.Fatalf("expected ok+any to match, got %v", names)
	}
	if names["other"] || names["off"] {
		t.Fatalf("wrong matches included: %v", names)
	}
}

func TestPipelineRunLifecycle(t *testing.T) {
	db := openTemp(t)
	c := &PipelineConfig{Project: "p2", Name: "build", Triggers: []string{"patchset-created"}, Enabled: true}
	if err := db.CreatePipelineConfig(c); err != nil {
		t.Fatalf("create config: %v", err)
	}
	r := &PipelineRun{ConfigID: c.ID, ChangeNumber: 42, PatchSet: 1, Project: "p2", Status: RunQueued, Runner: "sim"}
	if err := db.CreatePipelineRun(r); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if r.ID == 0 {
		t.Fatal("run id not set")
	}
	r.Status = RunRunning
	r.Started = "2026-01-01T00:00:00Z"
	if err := db.UpdatePipelineRun(r); err != nil {
		t.Fatalf("update run: %v", err)
	}
	r.Status = RunSuccess
	r.Finished = "2026-01-01T00:01:00Z"
	r.Log = "all tests passed"
	if err := db.UpdatePipelineRun(r); err != nil {
		t.Fatalf("finalize run: %v", err)
	}
	got, err := db.GetPipelineRun(r.ID)
	if err != nil || got.Status != RunSuccess || got.Log != "all tests passed" || got.Started == "" || got.Finished == "" {
		t.Fatalf("run state wrong: %+v err=%v", got, err)
	}

	runs, err := db.ListPipelineRunsForPatchSet(42, 1)
	if err != nil || len(runs) != 1 || runs[0].ConfigName != "build" {
		t.Fatalf("list runs for patchset: %v err=%v", runs, err)
	}

	// Deleting the config cascades away its runs.
	if err := db.DeletePipelineConfig(c.ID); err != nil {
		t.Fatalf("delete config: %v", err)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM pipeline_runs WHERE change_number=?`, int64(42)); n != 0 {
		t.Fatalf("runs should cascade-delete with config, left: %d", n)
	}
}
