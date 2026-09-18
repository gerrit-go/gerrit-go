package store

import (
	"strings"
)

// CheckRun is the state of an external CI check on a patch set. State follows
// the Gerrit Checks API vocabulary: NOT_STARTED, SCHEDULED, RUNNING,
// SUCCESSFUL, FAILED.
type CheckRun struct {
	ID           int64  `json:"-"`
	ChangeNumber int64  `json:"-"`
	PatchSet     int    `json:"-"`
	Name         string `json:"check_name"`
	State        string `json:"state"`
	URL          string `json:"url,omitempty"`
	Message      string `json:"message,omitempty"`
	Started      string `json:"started,omitempty"`
	Finished     string `json:"finished,omitempty"`
	Created      string `json:"-"`
}

// Webhook is an outbound HTTP callback registered for a project (project==""
// means global). Events is stored comma-separated; "*" matches every event.
type Webhook struct {
	ID      int64    `json:"id"`
	Project string   `json:"project"`
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Secret  string   `json:"-"`
	Active  bool     `json:"active"`
	Created string   `json:"created"`
}

// AuditEntry records a privileged or state-changing action for later review.
type AuditEntry struct {
	ID         int64  `json:"id"`
	AccountID  int64  `json:"account_id"`
	Action     string `json:"action"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Created    string `json:"created"`
}

// ---------- check runs ----------

// UpsertCheckRun creates or updates the check run keyed by
// (change, patch set, name).
func (d *DB) UpsertCheckRun(c *CheckRun) error {
	if strings.TrimSpace(c.State) == "" {
		c.State = "NOT_STARTED"
	}
	_, err := d.db.Exec(
		`INSERT INTO check_runs(change_number, patch_set, name, state, url, message, started, finished, created)
		 VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(change_number, patch_set, name) DO UPDATE SET
		   state=excluded.state, url=excluded.url, message=excluded.message,
		   started=excluded.started, finished=excluded.finished`,
		c.ChangeNumber, c.PatchSet, c.Name, c.State, c.URL, c.Message, c.Started, c.Finished, now())
	return err
}

// ListCheckRuns returns the check runs of a patch set, ordered by name.
func (d *DB) ListCheckRuns(changeNum int64, patchSet int) ([]*CheckRun, error) {
	rows, err := d.db.Query(
		`SELECT id, change_number, patch_set, name, state, url, message, started, finished, created
		 FROM check_runs WHERE change_number=? AND patch_set=? ORDER BY name`, changeNum, patchSet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CheckRun
	for rows.Next() {
		c := &CheckRun{}
		if err := rows.Scan(&c.ID, &c.ChangeNumber, &c.PatchSet, &c.Name, &c.State,
			&c.URL, &c.Message, &c.Started, &c.Finished, &c.Created); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCheckRun removes a check run by name from a patch set.
func (d *DB) DeleteCheckRun(changeNum int64, patchSet int, name string) error {
	_, err := d.db.Exec(`DELETE FROM check_runs WHERE change_number=? AND patch_set=? AND name=?`,
		changeNum, patchSet, name)
	return err
}

// ---------- webhooks ----------

func splitEvents(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{"*"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}

func joinEvents(ev []string) string {
	if len(ev) == 0 {
		return "*"
	}
	return strings.Join(ev, ",")
}

// CreateWebhook registers a webhook and returns it with its assigned ID.
func (d *DB) CreateWebhook(w *Webhook) (*Webhook, error) {
	id, err := d.insertID(
		`INSERT INTO webhooks(project, url, events, secret, active, created) VALUES(?,?,?,?,?,?)`,
		"id",
		w.Project, w.URL, joinEvents(w.Events), w.Secret, b2i(w.Active), now())
	if err != nil {
		return nil, err
	}
	w.ID = id
	w.Created = now()
	w.Events = splitEvents(joinEvents(w.Events))
	return w, nil
}

func scanWebhook(rows interface {
	Scan(...any) error
}) (*Webhook, error) {
	w := &Webhook{}
	var events string
	var active int
	if err := rows.Scan(&w.ID, &w.Project, &w.URL, &events, &w.Secret, &active, &w.Created); err != nil {
		return nil, err
	}
	w.Events = splitEvents(events)
	w.Active = active != 0
	return w, nil
}

// ListWebhooksForProject returns every webhook registered for a project
// (excluding global ones) for management purposes.
func (d *DB) ListWebhooksForProject(project string) ([]*Webhook, error) {
	rows, err := d.db.Query(
		`SELECT id, project, url, events, secret, active, created FROM webhooks WHERE project=? ORDER BY id`,
		project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListGlobalWebhooks returns webhooks registered with an empty project scope.
func (d *DB) ListGlobalWebhooks() ([]*Webhook, error) {
	rows, err := d.db.Query(
		`SELECT id, project, url, events, secret, active, created FROM webhooks WHERE project='' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListActiveWebhooks returns the enabled webhooks that apply to a project,
// including global ones. The dispatcher filters these by event type.
func (d *DB) ListActiveWebhooks(project string) ([]*Webhook, error) {
	rows, err := d.db.Query(
		`SELECT id, project, url, events, secret, active, created FROM webhooks
		 WHERE active=1 AND (project=? OR project='') ORDER BY id`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWebhook removes a webhook. Project-scoped callers should verify the
// webhook's project matches before deleting; global webhooks pass project="".
func (d *DB) DeleteWebhook(project string, id int64) error {
	_, err := d.db.Exec(`DELETE FROM webhooks WHERE id=? AND project=?`, id, project)
	return err
}

// ---------- audit log ----------

// AppendAudit records an audit entry. accountID may be 0 for anonymous/system
// actions.
func (d *DB) AppendAudit(accountID int64, action, targetType, targetID, detail string) error {
	_, err := d.db.Exec(
		`INSERT INTO audit_log(account_id, action, target_type, target_id, detail, created) VALUES(?,?,?,?,?,?)`,
		accountID, action, targetType, targetID, detail, now())
	return err
}

// AuditFilter narrows an audit-log query. Empty fields are ignored.
type AuditFilter struct {
	Action     string
	TargetType string
	TargetID   string
	Actor      string // username, case-insensitive substring
}

func (f AuditFilter) whereAndArgs() (string, []any) {
	var conds []string
	var args []any
	if f.Action != "" {
		conds = append(conds, "e.action=?")
		args = append(args, f.Action)
	}
	if f.TargetType != "" {
		conds = append(conds, "e.target_type=?")
		args = append(args, f.TargetType)
	}
	if f.TargetID != "" {
		conds = append(conds, "e.target_id=?")
		args = append(args, f.TargetID)
	}
	if f.Actor != "" {
		conds = append(conds, `LOWER(a.username) LIKE ?`)
		args = append(args, "%"+strings.ToLower(f.Actor)+"%")
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListAudit returns the most recent audit entries (newest first) with the
// acting account's username, plus the total row count for pagination.
func (d *DB) ListAudit(f AuditFilter, limit, offset int) ([]*AuditEntry, []string, int, error) {
	where, args := f.whereAndArgs()
	const from = ` FROM audit_log e LEFT JOIN accounts a ON a.id = e.account_id`
	var total int
	if err := d.db.QueryRow(`SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(
		`SELECT e.id, e.account_id, e.action, e.target_type, e.target_id, e.detail, e.created,
		        COALESCE(a.username, '')`+from+where+`
		 ORDER BY e.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	var out []*AuditEntry
	var users []string
	for rows.Next() {
		e := &AuditEntry{}
		var user string
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Action, &e.TargetType, &e.TargetID,
			&e.Detail, &e.Created, &user); err != nil {
			return nil, nil, 0, err
		}
		out = append(out, e)
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	return out, users, total, nil
}

// ListAuditActions returns the distinct recorded actions for filter dropdowns.
func (d *DB) ListAuditActions() ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT action FROM audit_log ORDER BY action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountChangesByStatus returns open/merged/abandoned counts for metrics gauges.
func (d *DB) CountChangesByStatus() (open, merged, abandoned int, err error) {
	rows, err := d.db.Query(`SELECT status, COUNT(*) FROM changes GROUP BY status`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return 0, 0, 0, err
		}
		switch status {
		case "NEW":
			open = n
		case "MERGED":
			merged = n
		case "ABANDONED":
			abandoned = n
		}
	}
	return open, merged, abandoned, rows.Err()
}

// CountTables returns the row counts of accounts and projects for metrics.
func (d *DB) CountTables() (accounts, projects int, err error) {
	if err = d.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&accounts); err != nil {
		return 0, 0, err
	}
	if err = d.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&projects); err != nil {
		return 0, 0, err
	}
	return accounts, projects, nil
}
