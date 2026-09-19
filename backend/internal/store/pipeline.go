package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// PipelineConfig is a project-scoped CI definition. Runs are triggered by change
// events (Triggers), execute Steps, and optionally cast a VoteLabel so that the
// existing submit-requirement gating can require CI to pass. Each run is mirrored
// into a check_run (see the api layer) so the current Checks UI shows CI status
// for free.
type PipelineConfig struct {
	ID       int64             `json:"id"`
	Project  string            `json:"project"`
	Name     string            `json:"name"`
	Triggers []string          `json:"triggers"`
	Steps    []string          `json:"steps"`
	Env      map[string]string `json:"env"`
	// VoteLabel, when non-empty (e.g. "Verified"), records pass/fail as a label
	// vote on the change so submit-requirements can gate on it.
	VoteLabel string    `json:"vote_label"`
	PassVote  int       `json:"pass_vote"`
	FailVote  int       `json:"fail_vote"`
	Required  bool      `json:"required"`
	Enabled   bool      `json:"enabled"`
	Created   time.Time `json:"created"`
}

// PipelineRun is one execution of a config against a specific patch set.
type PipelineRun struct {
	ID           int64  `json:"id"`
	ConfigID     int64  `json:"config_id"`
	ChangeNumber int64  `json:"change_number"`
	PatchSet     int    `json:"patch_set"`
	Project      string `json:"project"`
	Status       string `json:"status"` // QUEUED | RUNNING | SUCCESS | FAILURE | CANCELED | ERROR
	Runner       string `json:"runner"`
	Log          string `json:"log,omitempty"`
	Started      string `json:"started,omitempty"`
	Finished     string `json:"finished,omitempty"`
	Created      string `json:"created"`

	ConfigName string `json:"config_name,omitempty"` // enriched on list
}

// Pipeline run status values.
const (
	RunQueued   = "QUEUED"
	RunRunning  = "RUNNING"
	RunSuccess  = "SUCCESS"
	RunFailure  = "FAILURE"
	RunCanceled = "CANCELED"
	RunError    = "ERROR"
)

// ---------- pipeline configs ----------

func (d *DB) CreatePipelineConfig(c *PipelineConfig) error {
	triggers, _ := json.Marshal(c.Triggers)
	steps, _ := json.Marshal(c.Steps)
	env, _ := json.Marshal(c.Env)
	id, err := d.insertID(
		`INSERT INTO pipeline_configs(project, name, triggers, steps, env, vote_label, pass_vote, fail_vote, required, enabled, created)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"id", c.Project, c.Name, string(triggers), string(steps), string(env),
		c.VoteLabel, c.PassVote, c.FailVote, b2i(c.Required), b2i(c.Enabled), now())
	if err != nil {
		return err
	}
	c.ID = id
	return nil
}

func (d *DB) UpdatePipelineConfig(c *PipelineConfig) error {
	triggers, _ := json.Marshal(c.Triggers)
	steps, _ := json.Marshal(c.Steps)
	env, _ := json.Marshal(c.Env)
	_, err := d.db.Exec(
		`UPDATE pipeline_configs SET name=?, triggers=?, steps=?, env=?, vote_label=?, pass_vote=?, fail_vote=?, required=?, enabled=? WHERE id=?`,
		c.Name, string(triggers), string(steps), string(env),
		c.VoteLabel, c.PassVote, c.FailVote, b2i(c.Required), b2i(c.Enabled), c.ID)
	return err
}

func (d *DB) DeletePipelineConfig(id int64) error {
	_, err := d.db.Exec(`DELETE FROM pipeline_configs WHERE id=?`, id)
	return err
}

const pipelineConfigCols = `id, project, name, triggers, steps, env, vote_label, pass_vote, fail_vote, required, enabled, created`

func (d *DB) scanPipelineConfig(row interface{ Scan(...any) error }) (*PipelineConfig, error) {
	c := &PipelineConfig{}
	var triggers, steps, env string
	var required, enabled int
	var created string
	if err := row.Scan(&c.ID, &c.Project, &c.Name, &triggers, &steps, &env,
		&c.VoteLabel, &c.PassVote, &c.FailVote, &required, &enabled, &created); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(triggers), &c.Triggers)
	json.Unmarshal([]byte(steps), &c.Steps)
	json.Unmarshal([]byte(env), &c.Env)
	c.Required = required != 0
	c.Enabled = enabled != 0
	c.Created = parseTime(created)
	return c, nil
}

func (d *DB) GetPipelineConfig(id int64) (*PipelineConfig, error) {
	return d.scanPipelineConfig(d.db.QueryRow(`SELECT `+pipelineConfigCols+` FROM pipeline_configs WHERE id=?`, id))
}

func (d *DB) GetPipelineConfigByName(project, name string) (*PipelineConfig, error) {
	return d.scanPipelineConfig(d.db.QueryRow(
		`SELECT `+pipelineConfigCols+` FROM pipeline_configs WHERE project=? AND name=?`, project, name))
}

// ListPipelineConfigs returns configs for a project; with project=="" it returns
// every config (admin view).
func (d *DB) ListPipelineConfigs(project string) ([]*PipelineConfig, error) {
	query := `SELECT ` + pipelineConfigCols + ` FROM pipeline_configs`
	var args []any
	if project != "" {
		query += ` WHERE project=?`
		args = append(args, project)
	}
	query += " ORDER BY project, name"
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PipelineConfig
	for rows.Next() {
		c, err := d.scanPipelineConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- pipeline runs ----------

func (d *DB) CreatePipelineRun(r *PipelineRun) error {
	id, err := d.insertID(
		`INSERT INTO pipeline_runs(config_id, change_number, patch_set, project, status, runner, created)
		 VALUES(?,?,?,?,?,?,?)`,
		"id", r.ConfigID, r.ChangeNumber, r.PatchSet, r.Project, orDefault(r.Status, RunQueued), r.Runner, now())
	if err != nil {
		return err
	}
	r.ID = id
	return nil
}

func (d *DB) UpdatePipelineRun(r *PipelineRun) error {
	_, err := d.db.Exec(
		`UPDATE pipeline_runs SET status=?, runner=?, log=?, started=?, finished=? WHERE id=?`,
		r.Status, r.Runner, r.Log, r.Started, r.Finished, r.ID)
	return err
}

func (d *DB) GetPipelineRun(id int64) (*PipelineRun, error) {
	r := &PipelineRun{}
	var started, finished sql.NullString
	err := d.db.QueryRow(
		`SELECT id, config_id, change_number, patch_set, project, status, runner, log, started, finished, created
		 FROM pipeline_runs WHERE id=?`, id).
		Scan(&r.ID, &r.ConfigID, &r.ChangeNumber, &r.PatchSet, &r.Project, &r.Status,
			&r.Runner, &r.Log, &started, &finished, &r.Created)
	if err != nil {
		return nil, err
	}
	r.Started = started.String
	r.Finished = finished.String
	return r, nil
}

func (d *DB) scanPipelineRuns(rows *sql.Rows) ([]*PipelineRun, error) {
	var out []*PipelineRun
	for rows.Next() {
		r := &PipelineRun{}
		var started, finished sql.NullString
		if err := rows.Scan(&r.ID, &r.ConfigID, &r.ChangeNumber, &r.PatchSet, &r.Project, &r.Status,
			&r.Runner, &r.Log, &started, &finished, &r.Created, &r.ConfigName); err != nil {
			return nil, err
		}
		r.Started = started.String
		r.Finished = finished.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListPipelineRunsForPatchSet returns runs (with config names) for a change's
// patch set, newest first.
func (d *DB) ListPipelineRunsForPatchSet(changeNumber int64, patchSet int) ([]*PipelineRun, error) {
	rows, err := d.db.Query(
		`SELECT pr.id, pr.config_id, pr.change_number, pr.patch_set, pr.project, pr.status, pr.runner,
		        pr.log, pr.started, pr.finished, pr.created, pc.name
		 FROM pipeline_runs pr JOIN pipeline_configs pc ON pc.id = pr.config_id
		 WHERE pr.change_number=? AND pr.patch_set=? ORDER BY pr.id DESC`, changeNumber, patchSet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return d.scanPipelineRuns(rows)
}

// ListPipelineRunsByConfig returns a config's runs, newest first.
func (d *DB) ListPipelineRunsByConfig(configID int64, limit int) ([]*PipelineRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := d.db.Query(
		`SELECT pr.id, pr.config_id, pr.change_number, pr.patch_set, pr.project, pr.status, pr.runner,
		        pr.log, pr.started, pr.finished, pr.created, pc.name
		 FROM pipeline_runs pr JOIN pipeline_configs pc ON pc.id = pr.config_id
		 WHERE pr.config_id=? ORDER BY pr.id DESC LIMIT ?`, configID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return d.scanPipelineRuns(rows)
}

// ListEnabledConfigsForTrigger returns enabled configs on a project whose
// triggers include the given event (e.g. "patchset-created").
func (d *DB) ListEnabledConfigsForTrigger(project, event string) ([]*PipelineConfig, error) {
	all, err := d.ListPipelineConfigs(project)
	if err != nil {
		return nil, err
	}
	var out []*PipelineConfig
	for _, c := range all {
		if !c.Enabled {
			continue
		}
		for _, t := range c.Triggers {
			if t == event || t == "*" {
				out = append(out, c)
				break
			}
		}
	}
	return out, nil
}

// helpers

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
