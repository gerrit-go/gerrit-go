package store

import (
	"database/sql"
	"time"
)

// Group is an account group used by the access-control system.
type Group struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	OwnerGroupID int64     `json:"owner_group_id,omitempty"`
	System       bool      `json:"system,omitempty"`
	Created      time.Time `json:"-"`
}

// AccessRule grants (or denies) a permission on a ref pattern to a group.
// Project "*" holds the global defaults inherited by every project.
type AccessRule struct {
	ID         int64  `json:"id"`
	Project    string `json:"project"`
	RefPattern string `json:"ref_pattern"`
	Permission string `json:"permission"`
	GroupID    int64  `json:"group_id"`
	GroupName  string `json:"group_name,omitempty"`
	Action     string `json:"action"` // ALLOW | DENY | BLOCK
	Exclusive  bool   `json:"exclusive,omitempty"`
	Min        int    `json:"min,omitempty"`
	Max        int    `json:"max,omitempty"`
}

// ---------- groups ----------

func (d *DB) CreateGroup(g *Group) error {
	res, err := d.db.Exec(
		`INSERT INTO groups(name, description, owner_group_id, system, created) VALUES(?,?,?,?,?)`,
		g.Name, g.Description, g.OwnerGroupID, b2i(g.System), now())
	if err != nil {
		return err
	}
	g.ID, _ = res.LastInsertId()
	g.Created = parseTime(now())
	return nil
}

func (d *DB) GetGroup(id int64) (*Group, error) {
	g := &Group{}
	var sys int
	var created string
	err := d.db.QueryRow(`SELECT id, name, description, owner_group_id, system, created FROM groups WHERE id=?`, id).
		Scan(&g.ID, &g.Name, &g.Description, &g.OwnerGroupID, &sys, &created)
	if err != nil {
		return nil, err
	}
	g.System = sys == 1
	g.Created = parseTime(created)
	return g, nil
}

func (d *DB) GetGroupByName(name string) (*Group, error) {
	g := &Group{}
	var sys int
	var created string
	err := d.db.QueryRow(`SELECT id, name, description, owner_group_id, system, created FROM groups WHERE name=?`, name).
		Scan(&g.ID, &g.Name, &g.Description, &g.OwnerGroupID, &sys, &created)
	if err != nil {
		return nil, err
	}
	g.System = sys == 1
	g.Created = parseTime(created)
	return g, nil
}

func (d *DB) ListGroups() ([]*Group, error) {
	rows, err := d.db.Query(`SELECT id, name, description, owner_group_id, system, created FROM groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Group
	for rows.Next() {
		g := &Group{}
		var sys int
		var created string
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.OwnerGroupID, &sys, &created); err != nil {
			return nil, err
		}
		g.System = sys == 1
		g.Created = parseTime(created)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (d *DB) DeleteGroup(id int64) error {
	_, err := d.db.Exec(`DELETE FROM groups WHERE id=? AND system=0`, id)
	return err
}

func (d *DB) AddGroupMember(groupID, accountID int64) error {
	_, err := d.db.Exec(
		`INSERT INTO group_members(group_id, account_id, added) VALUES(?,?,?)
		 ON CONFLICT(group_id, account_id) DO NOTHING`, groupID, accountID, now())
	return err
}

func (d *DB) RemoveGroupMember(groupID, accountID int64) error {
	_, err := d.db.Exec(`DELETE FROM group_members WHERE group_id=? AND account_id=?`, groupID, accountID)
	return err
}

func (d *DB) ListGroupMembers(groupID int64) ([]*Account, error) {
	rows, err := d.db.Query(`
		SELECT a.id, a.username, a.full_name, a.email, a.admin
		FROM group_members m JOIN accounts a ON a.id = m.account_id
		WHERE m.group_id=? ORDER BY a.username`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Account
	for rows.Next() {
		a := &Account{}
		var admin int
		if err := rows.Scan(&a.ID, &a.Username, &a.FullName, &a.Email, &admin); err != nil {
			return nil, err
		}
		a.Admin = admin == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// GroupsForAccount returns the groups an account belongs to. A nil account
// (anonymous) maps to the "Anonymous Users" group only.
func (d *DB) GroupsForAccount(accountID int64) ([]*Group, error) {
	rows, err := d.db.Query(`
		SELECT g.id, g.name, g.description, g.owner_group_id, g.system
		FROM group_members m JOIN groups g ON g.id = m.group_id
		WHERE m.account_id=? ORDER BY g.name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Group
	for rows.Next() {
		g := &Group{}
		var sys int
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.OwnerGroupID, &sys); err != nil {
			return nil, err
		}
		g.System = sys == 1
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---------- access rules ----------

// SetAccessRules replaces all rules of a project with the given set.
func (d *DB) SetAccessRules(project string, rules []*AccessRule) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM access_rules WHERE project=?`, project); err != nil {
		return err
	}
	for _, r := range rules {
		action := r.Action
		if action == "" {
			action = "ALLOW"
		}
		if _, err := tx.Exec(
			`INSERT INTO access_rules(project, ref_pattern, permission, group_id, action, exclusive, min_val, max_val)
			 VALUES(?,?,?,?,?,?,?,?)`,
			project, r.RefPattern, r.Permission, r.GroupID, action, b2i(r.Exclusive), r.Min, r.Max); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListAccessRules returns rules for a project plus the global ('*') defaults.
func (d *DB) ListAccessRules(project string) ([]*AccessRule, error) {
	rows, err := d.db.Query(`
		SELECT r.id, r.project, r.ref_pattern, r.permission, r.group_id, g.name,
		       r.action, r.exclusive, r.min_val, r.max_val
		FROM access_rules r JOIN groups g ON g.id = r.group_id
		WHERE r.project=? OR r.project='*'
		ORDER BY (r.project='*'), r.permission, r.ref_pattern`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AccessRule
	for rows.Next() {
		r := &AccessRule{}
		var excl int
		if err := rows.Scan(&r.ID, &r.Project, &r.RefPattern, &r.Permission, &r.GroupID, &r.GroupName,
			&r.Action, &excl, &r.Min, &r.Max); err != nil {
			return nil, err
		}
		r.Exclusive = excl == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- submit requirements ----------

// SubmitRequirement is a per-project label rule that must be satisfied before a
// change can be submitted: some reviewer must vote at least MinValue, and no
// reviewer may vote at or below BlockValue (a veto).
type SubmitRequirement struct {
	ID         int64  `json:"id"`
	Project    string `json:"project"`
	Label      string `json:"label"`
	MinValue   int    `json:"min_value"`
	BlockValue int    `json:"block_value"`
}

// SetSubmitRequirements replaces a project's submit requirements.
func (d *DB) SetSubmitRequirements(project string, reqs []*SubmitRequirement) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM submit_requirements WHERE project=?`, project); err != nil {
		return err
	}
	for _, r := range reqs {
		if _, err := tx.Exec(
			`INSERT INTO submit_requirements(project, label, min_value, block_value) VALUES(?,?,?,?)`,
			project, r.Label, r.MinValue, r.BlockValue); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListSubmitRequirements returns a project's submit requirements, falling back
// to the default Code-Review +2 / -2 veto rule when none are configured.
func (d *DB) ListSubmitRequirements(project string) ([]*SubmitRequirement, error) {
	rows, err := d.db.Query(`SELECT id, project, label, min_value, block_value FROM submit_requirements WHERE project=? ORDER BY label`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SubmitRequirement
	for rows.Next() {
		r := &SubmitRequirement{}
		if err := rows.Scan(&r.ID, &r.Project, &r.Label, &r.MinValue, &r.BlockValue); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		out = append(out, &SubmitRequirement{Project: project, Label: "Code-Review", MinValue: 2, BlockValue: -2})
	}
	return out, nil
}

// ---------- starred changes ----------

func (d *DB) StarChange(accountID, changeNumber int64) error {
	_, err := d.db.Exec(
		`INSERT INTO starred(account_id, change_number, added) VALUES(?,?,?)
		 ON CONFLICT(account_id, change_number) DO NOTHING`, accountID, changeNumber, now())
	return err
}

func (d *DB) UnstarChange(accountID, changeNumber int64) error {
	_, err := d.db.Exec(`DELETE FROM starred WHERE account_id=? AND change_number=?`, accountID, changeNumber)
	return err
}

func (d *DB) IsStarred(accountID, changeNumber int64) bool {
	var n int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE account_id=? AND change_number=?`,
		accountID, changeNumber).Scan(&n)
	return n > 0
}

func (d *DB) ListStarred(accountID int64) ([]int64, error) {
	rows, err := d.db.Query(`SELECT change_number FROM starred WHERE account_id=?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---------- watched projects ----------

// WatchProject records that accountID watches project. notify controls the email
// verbosity (ALL, NONE, OWN_COMMENTS); only ALL/OWN_COMMENTS generate in-app
// notifications, NONE is stored but suppressed.
func (d *DB) WatchProject(accountID int64, project, notify string) error {
	if notify == "" {
		notify = "ALL"
	}
	_, err := d.db.Exec(
		`INSERT INTO watched_projects(account_id, project, notify, added) VALUES(?,?,?,?)
		 ON CONFLICT(account_id, project) DO UPDATE SET notify=excluded.notify`,
		accountID, project, notify, now())
	return err
}

func (d *DB) UnwatchProject(accountID int64, project string) error {
	_, err := d.db.Exec(`DELETE FROM watched_projects WHERE account_id=? AND project=?`, accountID, project)
	return err
}

func (d *DB) IsWatching(accountID int64, project string) bool {
	var n int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM watched_projects WHERE account_id=? AND project=?`,
		accountID, project).Scan(&n)
	return n > 0
}

// WatchSetting returns the notify setting for accountID on project, or "" when
// the project is not watched.
func (d *DB) WatchSetting(accountID int64, project string) string {
	var notify string
	_ = d.db.QueryRow(`SELECT notify FROM watched_projects WHERE account_id=? AND project=?`,
		accountID, project).Scan(&notify)
	return notify
}

// WatchedProject pairs a project name with its notify setting.
type WatchedProject struct {
	Project string `json:"project"`
	Notify  string `json:"notify"`
}

func (d *DB) ListWatchedProjects(accountID int64) ([]WatchedProject, error) {
	rows, err := d.db.Query(`SELECT project, notify FROM watched_projects WHERE account_id=? ORDER BY project`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchedProject
	for rows.Next() {
		var wp WatchedProject
		if err := rows.Scan(&wp.Project, &wp.Notify); err != nil {
			return nil, err
		}
		out = append(out, wp)
	}
	return out, rows.Err()
}

// ListProjectWatchers returns account IDs watching project, excluding accounts
// whose notify setting is NONE.
func (d *DB) ListProjectWatchers(project string) ([]int64, error) {
	rows, err := d.db.Query(`SELECT account_id FROM watched_projects WHERE project=? AND notify != 'NONE'`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------- notifications ----------

// Notification is an in-app event delivered to one account about a change.
type Notification struct {
	ID           int64     `json:"id"`
	AccountID    int64     `json:"-"`
	ChangeNumber int64     `json:"change_number"`
	Type         string    `json:"type"`
	Message      string    `json:"message"`
	ActorID      int64     `json:"actor_id,omitempty"`
	Read         bool      `json:"read"`
	Created      time.Time `json:"created"`
}

func (d *DB) CreateNotification(n *Notification) error {
	res, err := d.db.Exec(
		`INSERT INTO notifications(account_id, change_number, type, message, actor_id, read, created) VALUES(?,?,?,?,?,0,?)`,
		n.AccountID, n.ChangeNumber, n.Type, n.Message, n.ActorID, now())
	if err != nil {
		return err
	}
	n.ID, _ = res.LastInsertId()
	n.Created = parseTime(now())
	n.Read = false
	return nil
}

func (d *DB) ListNotifications(accountID int64, limit int, unreadOnly bool) ([]*Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, account_id, change_number, type, message, actor_id, read, created FROM notifications WHERE account_id=?`
	args := []any{accountID}
	if unreadOnly {
		q += ` AND read=0`
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Notification
	for rows.Next() {
		n := &Notification{}
		var actor sql.NullInt64
		var read int
		var created string
		if err := rows.Scan(&n.ID, &n.AccountID, &n.ChangeNumber, &n.Type, &n.Message, &actor, &read, &created); err != nil {
			return nil, err
		}
		n.ActorID = actor.Int64
		n.Read = read != 0
		n.Created = parseTime(created)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (d *DB) CountUnreadNotifications(accountID int64) int {
	var n int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE account_id=? AND read=0`, accountID).Scan(&n)
	return n
}

func (d *DB) MarkNotificationRead(accountID, id int64) error {
	_, err := d.db.Exec(`UPDATE notifications SET read=1 WHERE account_id=? AND id=?`, accountID, id)
	return err
}

func (d *DB) MarkAllNotificationsRead(accountID int64) error {
	_, err := d.db.Exec(`UPDATE notifications SET read=1 WHERE account_id=? AND read=0`, accountID)
	return err
}
