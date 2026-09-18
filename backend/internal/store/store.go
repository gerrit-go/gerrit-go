package store

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Account struct {
	ID           int64  `json:"_account_id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	FullName     string `json:"name"`
	Email        string `json:"email"`
	Admin        bool   `json:"-"`
	Created      time.Time
}

type Session struct {
	ID        string
	AccountID int64
	Expires   time.Time
}

type Project struct {
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	Head             string    `json:"-"`
	State            string    `json:"state"`
	SubmitType       string    `json:"submit_type"`
	SubmitWholeTopic bool      `json:"submit_whole_topic"`
	Parent           string    `json:"parent,omitempty"`
	Created          time.Time `json:"-"`
}

type Change struct {
	Number         int64      `json:"_number"`
	Project        string     `json:"project"`
	Branch         string     `json:"branch"`
	ChangeID       string     `json:"change_id"`
	Subject        string     `json:"subject"`
	OwnerID        int64      `json:"-"`
	Status         string     `json:"status"` // NEW, MERGED, ABANDONED
	Topic          string     `json:"topic,omitempty"`
	WorkInProgress bool       `json:"work_in_progress,omitempty"`
	Private        bool       `json:"private,omitempty"`
	Created        time.Time  `json:"created"`
	Updated        time.Time  `json:"updated"`
	Submitted      *time.Time `json:"submitted,omitempty"`
	CurrentPS      int        `json:"-"`
	OwnerName      string     `json:"-"`
	OwnerEmail     string     `json:"-"`
	OwnerUser      string     `json:"-"`
}

type PatchSet struct {
	ChangeNumber int64     `json:"-"`
	Number       int       `json:"_number"`
	CommitSHA    string    `json:"commit"`
	AuthorName   string    `json:"-"`
	AuthorEmail  string    `json:"-"`
	Message      string    `json:"-"`
	Created      time.Time `json:"created"`
}

type Vote struct {
	ChangeNumber int64
	PatchSet     int
	AccountID    int64
	Label        string
	Value        int
}

type Comment struct {
	ID         int64     `json:"id"`
	ChangeNum  int64     `json:"-"`
	PatchSet   int       `json:"patch_set"`
	File       string    `json:"path"`
	Line       int       `json:"line"`
	Message    string    `json:"message"`
	AuthorID   int64     `json:"-"`
	AuthorName string    `json:"-"`
	AuthorUser string    `json:"-"`
	Created    time.Time `json:"updated"`
	InReplyTo  int64     `json:"in_reply_to,omitempty"`
	Resolved   bool      `json:"resolved"`
	RobotID    string    `json:"robot_id,omitempty"`
	RobotRunID string    `json:"robot_run_id,omitempty"`
}

// CommentDraft is a private, unpublished inline comment owned by one account.
// Drafts become real comments when the author posts a review.
type CommentDraft struct {
	ID        int64     `json:"id"`
	ChangeNum int64     `json:"-"`
	PatchSet  int       `json:"patch_set"`
	AccountID int64     `json:"-"`
	File      string    `json:"path"`
	Line      int       `json:"line"`
	Message   string    `json:"message"`
	InReplyTo int64     `json:"in_reply_to,omitempty"`
	Created   time.Time `json:"updated"`
}

// ChangeMessage is one entry in a change's unified timeline (Gerrit
// "change messages"): patch set uploads, votes, cover comments, reviewer
// changes, topic/WIP edits and status transitions.
type ChangeMessage struct {
	ID         int64     `json:"id"`
	ChangeNum  int64     `json:"-"`
	PatchSet   int       `json:"patch_set"`
	Type       string    `json:"type"` // patchset-uploaded | vote | comment | reviewer-added | reviewer-removed | topic | wip | submitted | abandoned | restored
	AuthorID   int64     `json:"-"`
	AuthorName string    `json:"-"`
	AuthorUser string    `json:"-"`
	Message    string    `json:"message"`
	Created    time.Time `json:"date"`
}

// Reviewer is an account asked to review a change.
type Reviewer struct {
	ChangeNumber int64     `json:"-"`
	AccountID    int64     `json:"_account_id"`
	Name         string    `json:"name"`
	Username     string    `json:"username"`
	Email        string    `json:"email,omitempty"`
	Added        time.Time `json:"-"`
}

func Open(dsn string) (*DB, error) {
	if IsPostgresDSN(dsn) {
		db, err := sql.Open(pgDriverName, dsn)
		if err != nil {
			return nil, err
		}
		if err := migrate(db, DriverPostgres); err != nil {
			db.Close()
			return nil, err
		}
		return &DB{db: db, driver: DriverPostgres, perm: newPermCache()}, nil
	}
	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc sqlite + WAL: single writer avoids contention
	if err := migrate(db, DriverSQLite); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db, driver: DriverSQLite, perm: newPermCache()}, nil
}

type DB struct {
	db     *sql.DB
	driver string
	perm   *permCache
}

func (d *DB) Close() error { return d.db.Close() }

// Ping verifies the database connection is alive.
func (d *DB) Ping() error { return d.db.Ping() }

const schemaSQLite = `
CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  full_name TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  admin INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_accounts_email ON accounts(email);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  expires TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS projects (
  name TEXT PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  head TEXT NOT NULL DEFAULT 'master',
  parent TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS changes (
  number INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL REFERENCES projects(name),
  branch TEXT NOT NULL,
  change_id TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  owner_id INTEGER NOT NULL REFERENCES accounts(id),
  status TEXT NOT NULL DEFAULT 'NEW',
  topic TEXT NOT NULL DEFAULT '',
  work_in_progress INTEGER NOT NULL DEFAULT 0,
  private INTEGER NOT NULL DEFAULT 0,
  current_ps INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL,
  updated TEXT NOT NULL,
  submitted TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_changes_cid ON changes(project, branch, change_id);
CREATE INDEX IF NOT EXISTS idx_changes_topic ON changes(topic);
CREATE INDEX IF NOT EXISTS idx_changes_owner ON changes(owner_id, status, updated);
CREATE INDEX IF NOT EXISTS idx_changes_proj_status ON changes(project, status, updated);
CREATE TABLE IF NOT EXISTS patchsets (
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  number INTEGER NOT NULL,
  commit_sha TEXT NOT NULL,
  author_name TEXT NOT NULL DEFAULT '',
  author_email TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL,
  PRIMARY KEY (change_number, number)
);
CREATE TABLE IF NOT EXISTS patchset_files (
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  patch_set INTEGER NOT NULL,
  file_path TEXT NOT NULL,
  PRIMARY KEY (change_number, patch_set, file_path)
);
CREATE INDEX IF NOT EXISTS idx_psfiles_path ON patchset_files(file_path);
CREATE INDEX IF NOT EXISTS idx_psfiles_change ON patchset_files(change_number);
CREATE TABLE IF NOT EXISTS votes (
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  patch_set INTEGER NOT NULL,
  account_id INTEGER NOT NULL REFERENCES accounts(id),
  label TEXT NOT NULL,
  value INTEGER NOT NULL,
  PRIMARY KEY (change_number, patch_set, account_id, label)
);
CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  patch_set INTEGER NOT NULL,
  file TEXT NOT NULL DEFAULT '',
  line INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL,
  author_id INTEGER NOT NULL REFERENCES accounts(id),
  in_reply_to INTEGER NOT NULL DEFAULT 0,
  resolved INTEGER NOT NULL DEFAULT 0,
  robot_id TEXT NOT NULL DEFAULT '',
  robot_run_id TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_comments_change_ps ON comments(change_number, patch_set);
CREATE TABLE IF NOT EXISTS comment_drafts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  patch_set INTEGER NOT NULL,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  file TEXT NOT NULL DEFAULT '',
  line INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL,
  in_reply_to INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_drafts_change ON comment_drafts(change_number, account_id, id);
CREATE TABLE IF NOT EXISTS change_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  patch_set INTEGER NOT NULL DEFAULT 0,
  type TEXT NOT NULL,
  author_id INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_change ON change_messages(change_number, id);
CREATE TABLE IF NOT EXISTS reviewers (
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  added TEXT NOT NULL,
  PRIMARY KEY (change_number, account_id)
);
CREATE TABLE IF NOT EXISTS groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  owner_group_id INTEGER NOT NULL DEFAULT 0,
  system INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS group_members (
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  added TEXT NOT NULL,
  PRIMARY KEY (group_id, account_id)
);
CREATE TABLE IF NOT EXISTS access_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL,
  ref_pattern TEXT NOT NULL,
  permission TEXT NOT NULL,
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  action TEXT NOT NULL DEFAULT 'ALLOW',
  exclusive INTEGER NOT NULL DEFAULT 0,
  min_val INTEGER NOT NULL DEFAULT 0,
  max_val INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_access_project ON access_rules(project, permission);
CREATE TABLE IF NOT EXISTS starred (
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  added TEXT NOT NULL,
  PRIMARY KEY (account_id, change_number)
);
CREATE TABLE IF NOT EXISTS saved_queries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  query TEXT NOT NULL,
  shared INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS submit_requirements (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL,
  label TEXT NOT NULL,
  min_value INTEGER NOT NULL DEFAULT 2,
  block_value INTEGER NOT NULL DEFAULT -2,
  UNIQUE (project, label)
);
CREATE TABLE IF NOT EXISTS watched_projects (
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  project TEXT NOT NULL,
  notify TEXT NOT NULL DEFAULT 'ALL',
  branch TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  added TEXT NOT NULL,
  PRIMARY KEY (account_id, project)
);
CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  type TEXT NOT NULL,
  message TEXT NOT NULL,
  actor_id INTEGER,
  read INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_account ON notifications(account_id, read, id);
CREATE TABLE IF NOT EXISTS ssh_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  public_key TEXT NOT NULL,
  comment TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ssh_keys_account ON ssh_keys(account_id, id);
CREATE TABLE IF NOT EXISTS change_hashtags (
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  hashtag TEXT NOT NULL,
  added TEXT NOT NULL,
  PRIMARY KEY (change_number, hashtag)
);
CREATE INDEX IF NOT EXISTS idx_hashtags_tag ON change_hashtags(hashtag);
CREATE TABLE IF NOT EXISTS change_attention (
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  reason TEXT NOT NULL DEFAULT '',
  added TEXT NOT NULL,
  PRIMARY KEY (change_number, account_id)
);
CREATE TABLE IF NOT EXISTS check_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  change_number INTEGER NOT NULL REFERENCES changes(number) ON DELETE CASCADE,
  patch_set INTEGER NOT NULL,
  name TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'NOT_STARTED',
  url TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  started TEXT NOT NULL DEFAULT '',
  finished TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL,
  UNIQUE (change_number, patch_set, name)
);
CREATE INDEX IF NOT EXISTS idx_check_runs_ps ON check_runs(change_number, patch_set);
CREATE TABLE IF NOT EXISTS webhooks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL,
  events TEXT NOT NULL DEFAULT '*',
  secret TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhooks_project ON webhooks(project);
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL DEFAULT 0,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL DEFAULT '',
  target_id TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_id ON audit_log(id);
CREATE TABLE IF NOT EXISTS project_labels (
  project TEXT NOT NULL REFERENCES projects(name) ON DELETE CASCADE,
  label TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project, label)
);
CREATE INDEX IF NOT EXISTS idx_plabels_label ON project_labels(label, value);
CREATE TABLE IF NOT EXISTS roles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  permissions TEXT NOT NULL DEFAULT '[]',
  created TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS role_bindings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL,
  subject_id INTEGER NOT NULL,
  scope TEXT NOT NULL DEFAULT '*',
  created TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rb_subject ON role_bindings(subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_rb_role ON role_bindings(role_id);
CREATE TABLE IF NOT EXISTS app_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`

func migrate(db *sql.DB, drv string) error {
	schema := schemaSQLite
	if drv == DriverPostgres {
		schema = toPostgresSchema(schemaSQLite)
		// lib/pq cannot prepare a multi-statement string, so run each
		// CREATE statement separately.
		for _, stmt := range splitStatements(schema) {
			if _, err := db.Exec(stmt); err != nil {
				return err
			}
		}
	} else if _, err := db.Exec(schema); err != nil {
		return err
	}
	// Upgrade databases created before topic/WIP/private existed.
	for _, col := range []struct{ name, def string }{
		{"topic", "topic TEXT NOT NULL DEFAULT ''"},
		{"work_in_progress", "work_in_progress INTEGER NOT NULL DEFAULT 0"},
		{"private", "private INTEGER NOT NULL DEFAULT 0"},
		{"assignee_id", "assignee_id INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := addColumnIfMissing(db, drv, "changes", col.name, col.def); err != nil {
			return err
		}
	}
	for _, col := range []struct{ name, def string }{
		{"state", "state TEXT NOT NULL DEFAULT 'ACTIVE'"},
		{"submit_type", "submit_type TEXT NOT NULL DEFAULT 'REBASE_IF_NECESSARY'"},
		{"submit_whole_topic", "submit_whole_topic INTEGER NOT NULL DEFAULT 0"},
		{"parent", "parent TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumnIfMissing(db, drv, "projects", col.name, col.def); err != nil {
			return err
		}
	}
	if err := addColumnIfMissing(db, drv, "comments", "resolved", "resolved INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	for _, col := range []struct{ name, def string }{
		{"robot_id", "robot_id TEXT NOT NULL DEFAULT ''"},
		{"robot_run_id", "robot_run_id TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumnIfMissing(db, drv, "comments", col.name, col.def); err != nil {
			return err
		}
	}
	for _, col := range []struct{ name, def string }{
		{"branch", "branch TEXT NOT NULL DEFAULT ''"},
		{"author", "author TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumnIfMissing(db, drv, "watched_projects", col.name, col.def); err != nil {
			return err
		}
	}
	for _, col := range []struct{ name, def string }{
		{"http_password_hash", "http_password_hash TEXT NOT NULL DEFAULT ''"},
		{"external_id", "external_id TEXT NOT NULL DEFAULT ''"},
		{"totp_secret", "totp_secret TEXT NOT NULL DEFAULT ''"},
		{"totp_enabled", "totp_enabled INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := addColumnIfMissing(db, drv, "accounts", col.name, col.def); err != nil {
			return err
		}
	}
	if err := seedDefaults(db); err != nil {
		return err
	}
	if err := migrateDefaultDeny(db); err != nil {
		return err
	}
	return nil
}

// seedDefaults creates the built-in groups and the global (All-Projects,
// project='*') access rules on first run. It is idempotent.
//
// The permission model is default-deny: only the Administrators group gets a
// global grant. Everything else must come from an explicit RBAC role binding
// or a per-project access rule, so a fresh server isolates tenants until an
// admin grants access.
func seedDefaults(db *sql.DB) error {
	for _, g := range []struct {
		name, desc string
	}{
		{"Administrators", "Built-in group of server administrators"},
		{"Registered Users", "Built-in group of all signed-in accounts"},
		{"Anonymous Users", "Built-in group of not-signed-in callers"},
	} {
		if _, err := db.Exec(
			`INSERT INTO groups(name, description, system, created) VALUES(?,?,1,?)
			 ON CONFLICT(name) DO NOTHING`, g.name, g.desc, now()); err != nil {
			return err
		}
	}
	type rule struct {
		ref, perm, group, action string
		min, max                 int
	}
	rules := []rule{
		{"refs/*", "read", "Administrators", "ALLOW", 0, 0},
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM access_rules WHERE project='*'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for _, r := range rules {
			var gid int64
			if err := db.QueryRow(`SELECT id FROM groups WHERE name=?`, r.group).Scan(&gid); err != nil {
				return err
			}
			if _, err := db.Exec(
				`INSERT INTO access_rules(project, ref_pattern, permission, group_id, action, exclusive, min_val, max_val)
				 VALUES('*',?,?,?,?,0,?,?)`,
				r.ref, r.perm, gid, r.action, r.min, r.max); err != nil {
				return err
			}
		}
	}
	return nil
}

// permissionModelMetaKey gates the one-time default-deny migration.
const permissionModelMetaKey = "permission_model"

// legacyPermissiveSeeds lists the global ALLOW rules that older builds seeded
// for everyone. migrateDefaultDeny deletes exactly these rows so that any
// hand-written rule survives.
var legacyPermissiveSeeds = []struct{ ref, perm, group string }{
	{"refs/*", "read", "Anonymous Users"},
	{"refs/*", "read", "Registered Users"},
	{"refs/for/*", "push", "Registered Users"},
	{"refs/*", "comment", "Registered Users"},
	{"refs/*", "label-Code-Review", "Registered Users"},
	{"refs/*", "label-Verified", "Registered Users"},
	{"*", "createProject", "Registered Users"},
}

// migrateDefaultDeny upgrades databases created under the open-by-default
// model: it removes the permissive global seed rules and records the new
// permission model in app_meta. Idempotent; the removed row count is written
// to the audit log so the change is traceable on production instances.
func migrateDefaultDeny(db *sql.DB) error {
	var model string
	_ = db.QueryRow(`SELECT value FROM app_meta WHERE key=?`, permissionModelMetaKey).Scan(&model)
	if model == "default-deny-v1" {
		return nil
	}
	removed := 0
	for _, s := range legacyPermissiveSeeds {
		res, err := db.Exec(
			`DELETE FROM access_rules
			 WHERE project='*' AND ref_pattern=? AND permission=? AND action='ALLOW'
			   AND group_id IN (SELECT id FROM groups WHERE name=?)`,
			s.ref, s.perm, s.group)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err == nil {
			removed += int(n)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO app_meta(key, value) VALUES(?, 'default-deny-v1')
		 ON CONFLICT(key) DO UPDATE SET value='default-deny-v1'`, permissionModelMetaKey); err != nil {
		return err
	}
	if removed > 0 {
		if _, err := db.Exec(
			`INSERT INTO audit_log(account_id, action, target_type, target_id, detail, created)
			 VALUES(0, 'permission-model-default-deny', 'server', '', ?, ?)`,
			"removed "+strconv.Itoa(removed)+" permissive global rules", now()); err != nil {
			return err
		}
	}
	return nil
}

// PermissionModel reports the active permission model marker, e.g.
// "default-deny-v1". Empty means a database predating the migration.
func (d *DB) PermissionModel() string {
	var v string
	_ = d.db.QueryRow(`SELECT value FROM app_meta WHERE key=?`, permissionModelMetaKey).Scan(&v)
	return v
}

// addColumnIfMissing adds a column to a table when an older schema lacks it.
func addColumnIfMissing(db *sql.DB, drv, table, column, def string) error {
	var rows *sql.Rows
	var err error
	if drv == DriverPostgres {
		rows, err = db.Query(`SELECT column_name FROM information_schema.columns WHERE table_name=$1`, table)
	} else {
		rows, err = db.Query(`PRAGMA table_info(` + table + `)`)
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if drv == DriverPostgres {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			if name == column {
				return nil
			}
			continue
		}
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + def)
	return err
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTime(s.String)
	return &t
}

// ---------- accounts ----------

func (d *DB) CreateAccount(a *Account) error {
	id, err := d.insertID(
		`INSERT INTO accounts(username, password_hash, full_name, email, admin, created) VALUES(?,?,?,?,?,?)`,
		"id",
		a.Username, a.PasswordHash, a.FullName, a.Email, b2i(a.Admin), now())
	if err != nil {
		return err
	}
	a.ID = id
	return nil
}

func (d *DB) GetAccountByUsername(username string) (*Account, error) {
	a := &Account{}
	var admin int
	var created string
	err := d.db.QueryRow(`SELECT id, username, password_hash, full_name, email, admin, created FROM accounts WHERE username=?`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.FullName, &a.Email, &admin, &created)
	if err != nil {
		return nil, err
	}
	a.Admin = admin == 1
	a.Created = parseTime(created)
	return a, nil
}

func (d *DB) GetAccount(id int64) (*Account, error) {
	a := &Account{}
	var admin int
	var created string
	err := d.db.QueryRow(`SELECT id, username, password_hash, full_name, email, admin, created FROM accounts WHERE id=?`, id).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.FullName, &a.Email, &admin, &created)
	if err != nil {
		return nil, err
	}
	a.Admin = admin == 1
	a.Created = parseTime(created)
	return a, nil
}

func (d *DB) UpdateAccountPassword(id int64, hash string) error {
	_, err := d.db.Exec(`UPDATE accounts SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (d *DB) ListAccounts() ([]*Account, error) {
	rows, err := d.db.Query(`SELECT id, username, full_name, email, admin, created FROM accounts ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Account
	for rows.Next() {
		a := &Account{}
		var admin int
		var created string
		if err := rows.Scan(&a.ID, &a.Username, &a.FullName, &a.Email, &admin, &created); err != nil {
			return nil, err
		}
		a.Admin = admin == 1
		a.Created = parseTime(created)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- sessions ----------

func (d *DB) CreateSession(s *Session) error {
	_, err := d.db.Exec(`INSERT INTO sessions(id, account_id, expires) VALUES(?,?,?)`,
		s.ID, s.AccountID, s.Expires.UTC().Format(time.RFC3339Nano))
	return err
}

func (d *DB) GetSession(id string) (*Session, error) {
	s := &Session{}
	var expires string
	err := d.db.QueryRow(`SELECT id, account_id, expires FROM sessions WHERE id=?`, id).Scan(&s.ID, &s.AccountID, &expires)
	if err != nil {
		return nil, err
	}
	s.Expires = parseTime(expires)
	return s, nil
}

func (d *DB) DeleteSession(id string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

// ---------- projects ----------

func (d *DB) CreateProject(p *Project) error {
	state := p.State
	if state == "" {
		state = "ACTIVE"
	}
	submitType := p.SubmitType
	if submitType == "" {
		submitType = "REBASE_IF_NECESSARY"
	}
	_, err := d.db.Exec(`INSERT INTO projects(name, description, head, state, submit_type, submit_whole_topic, parent, created) VALUES(?,?,?,?,?,?,?,?)`,
		p.Name, p.Description, p.Head, state, submitType, b2i(p.SubmitWholeTopic), p.Parent, now())
	return err
}

func (d *DB) GetProject(name string) (*Project, error) {
	p := &Project{}
	var created string
	var whole int
	err := d.db.QueryRow(`SELECT name, description, head, state, submit_type, submit_whole_topic, parent, created FROM projects WHERE name=?`, name).
		Scan(&p.Name, &p.Description, &p.Head, &p.State, &p.SubmitType, &whole, &p.Parent, &created)
	if err != nil {
		return nil, err
	}
	p.SubmitWholeTopic = whole == 1
	p.Created = parseTime(created)
	return p, nil
}

func (d *DB) ListProjects() ([]*Project, error) {
	rows, err := d.db.Query(`SELECT name, description, head, state, submit_type, submit_whole_topic, parent, created FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p := &Project{}
		var created string
		var whole int
		if err := rows.Scan(&p.Name, &p.Description, &p.Head, &p.State, &p.SubmitType, &whole, &p.Parent, &created); err != nil {
			return nil, err
		}
		p.SubmitWholeTopic = whole == 1
		p.Created = parseTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProjectSubmitType updates the submit strategy and whole-topic flag.
func (d *DB) SetProjectSubmitType(name, submitType string, wholeTopic bool) error {
	_, err := d.db.Exec(`UPDATE projects SET submit_type=?, submit_whole_topic=? WHERE name=?`,
		submitType, b2i(wholeTopic), name)
	return err
}

func (d *DB) SetProjectState(name, state string) error {
	_, err := d.db.Exec(`UPDATE projects SET state=? WHERE name=?`, state, name)
	return err
}

// SetProjectParent updates a project's parent. An empty parent means the
// project inherits only from the global '*' defaults.
func (d *DB) SetProjectParent(name, parent string) error {
	_, err := d.db.Exec(`UPDATE projects SET parent=? WHERE name=?`, parent, name)
	if err != nil {
		return err
	}
	d.perm.invalidateRules()
	return nil
}

// SetProjectDescription updates a project's description.
func (d *DB) SetProjectDescription(name, description string) error {
	_, err := d.db.Exec(`UPDATE projects SET description=? WHERE name=?`, description, name)
	return err
}

// ---------- project labels ----------

// SetProjectLabel sets a label on a project. If value is empty the label is
// set with an empty value (acts as a tag).
func (d *DB) SetProjectLabel(project, label, value string) error {
	_, err := d.db.Exec(`INSERT INTO project_labels(project, label, value) VALUES(?,?,?)
		ON CONFLICT(project, label) DO UPDATE SET value=excluded.value`, project, label, value)
	return err
}

// DeleteProjectLabel removes a label from a project.
func (d *DB) DeleteProjectLabel(project, label string) error {
	_, err := d.db.Exec(`DELETE FROM project_labels WHERE project=? AND label=?`, project, label)
	return err
}

// GetProjectLabels returns all labels for a project as a map.
func (d *DB) GetProjectLabels(project string) (map[string]string, error) {
	rows, err := d.db.Query(`SELECT label, value FROM project_labels WHERE project=? ORDER BY label`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var l, v string
		if err := rows.Scan(&l, &v); err != nil {
			return nil, err
		}
		out[l] = v
	}
	return out, rows.Err()
}

// ListProjectsByLabel returns project names that have the given label,
// optionally filtered by value (empty value matches any).
func (d *DB) ListProjectsByLabel(label, value string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if value != "" {
		rows, err = d.db.Query(`SELECT project FROM project_labels WHERE label=? AND value=? ORDER BY project`, label, value)
	} else {
		rows, err = d.db.Query(`SELECT project FROM project_labels WHERE label=? ORDER BY project`, label)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListProjectsByNamespace returns project names under the given namespace
// prefix. The namespace matches the project name up to the last '/'.
// For example, namespace "rk/android/14" matches "rk/android/14/kernel"
// but not "rk/android/14" itself (that would be a project, not a namespace).
func (d *DB) ListProjectsByNamespace(ns string) ([]string, error) {
	pattern := ns + "/%"
	rows, err := d.db.Query(`SELECT name FROM projects WHERE name LIKE ? ESCAPE '\' ORDER BY name`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAllLabels returns all distinct label keys and their values.
func (d *DB) ListAllLabels() (map[string][]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT label, value FROM project_labels ORDER BY label, value`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var l, v string
		if err := rows.Scan(&l, &v); err != nil {
			return nil, err
		}
		out[l] = append(out[l], v)
	}
	return out, rows.Err()
}

// ProjectNamespace returns the namespace of a project name — everything
// before the last '/'. Returns "" for top-level projects without '/'.
func ProjectNamespace(name string) string {
	i := strings.LastIndex(name, "/")
	if i < 0 {
		return ""
	}
	return name[:i]
}

// NamespaceTree builds a tree of namespaces from a list of project names.
// Each node is a path segment; children are sub-namespaces.
type NamespaceNode struct {
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Children []*NamespaceNode `json:"children,omitempty"`
	Count    int              `json:"count"` // number of projects directly under this namespace
}

// BuildNamespaceTree constructs the namespace tree from project names.
func BuildNamespaceTree(projects []string) []*NamespaceNode {
	if len(projects) == 0 {
		return nil
	}
	// Count projects per namespace.
	nsCount := map[string]int{}
	nsSet := map[string]bool{}
	for _, p := range projects {
		ns := ProjectNamespace(p)
		if ns == "" {
			continue
		}
		nsCount[ns]++
		// Register all ancestor namespaces.
		for ns != "" {
			nsSet[ns] = true
			ns = ProjectNamespace(ns)
		}
	}

	// Build tree bottom-up.
	var build func(prefix string) []*NamespaceNode
	build = func(prefix string) []*NamespaceNode {
		var nodes []*NamespaceNode
		seen := map[string]bool{}
		for ns := range nsSet {
			if ProjectNamespace(ns) != prefix {
				continue
			}
			// Get the last segment.
			name := ns
			if i := strings.LastIndex(ns, "/"); i >= 0 {
				name = ns[i+1:]
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			node := &NamespaceNode{
				Name:     name,
				Path:     ns,
				Count:    nsCount[ns],
				Children: build(ns),
			}
			nodes = append(nodes, node)
		}
		// Sort by name.
		for i := 0; i < len(nodes); i++ {
			for j := i + 1; j < len(nodes); j++ {
				if nodes[j].Name < nodes[i].Name {
					nodes[i], nodes[j] = nodes[j], nodes[i]
				}
			}
		}
		return nodes
	}
	return build("")
}

// ---------- changes ----------

func (d *DB) CreateChange(c *Change) error {
	id, err := d.insertID(
		`INSERT INTO changes(project, branch, change_id, subject, owner_id, status, topic, work_in_progress, private, current_ps, created, updated) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		"number",
		c.Project, c.Branch, c.ChangeID, c.Subject, c.OwnerID, "NEW", c.Topic, b2i(c.WorkInProgress), b2i(c.Private), c.CurrentPS, now(), now())
	if err != nil {
		return err
	}
	c.Number = id
	c.Status = "NEW"
	c.Created = parseTime(now())
	c.Updated = c.Created
	return nil
}

func (d *DB) GetChange(number int64) (*Change, error) {
	c := &Change{}
	var created, updated string
	var submitted sql.NullString
	var wip, priv int
	err := d.db.QueryRow(`
		SELECT ch.number, ch.project, ch.branch, ch.change_id, ch.subject, ch.owner_id, ch.status,
		       ch.topic, ch.work_in_progress, ch.private,
		       ch.current_ps, ch.created, ch.updated, ch.submitted,
		       a.full_name, a.email, a.username
		FROM changes ch JOIN accounts a ON a.id = ch.owner_id WHERE ch.number=?`, number).
		Scan(&c.Number, &c.Project, &c.Branch, &c.ChangeID, &c.Subject, &c.OwnerID, &c.Status,
			&c.Topic, &wip, &priv,
			&c.CurrentPS, &created, &updated, &submitted,
			&c.OwnerName, &c.OwnerEmail, &c.OwnerUser)
	if err != nil {
		return nil, err
	}
	c.WorkInProgress = wip == 1
	c.Private = priv == 1
	c.Created = parseTime(created)
	c.Updated = parseTime(updated)
	c.Submitted = parseTimePtr(submitted)
	return c, nil
}

// changeCols is the shared SELECT column list for change queries (alias ch
// for changes, a for the owner account).
const changeCols = `ch.number, ch.project, ch.branch, ch.change_id, ch.subject, ch.owner_id, ch.status,
       ch.topic, ch.work_in_progress, ch.private,
       ch.current_ps, ch.created, ch.updated, ch.submitted,
       a.full_name, a.email, a.username`

// scanChange maps a change row (using changeCols ordering) into a *Change.
func scanChange(scan func(dest ...any) error) (*Change, error) {
	c := &Change{}
	var created, updated string
	var submitted sql.NullString
	var wip, priv int
	if err := scan(&c.Number, &c.Project, &c.Branch, &c.ChangeID, &c.Subject, &c.OwnerID, &c.Status,
		&c.Topic, &wip, &priv,
		&c.CurrentPS, &created, &updated, &submitted,
		&c.OwnerName, &c.OwnerEmail, &c.OwnerUser); err != nil {
		return nil, err
	}
	c.WorkInProgress = wip == 1
	c.Private = priv == 1
	c.Created = parseTime(created)
	c.Updated = parseTime(updated)
	c.Submitted = parseTimePtr(submitted)
	return c, nil
}

func (d *DB) GetChangeByChangeID(project, branch, changeID string) (*Change, error) {
	row := d.db.QueryRow(`
		SELECT `+changeCols+`
		FROM changes ch JOIN accounts a ON a.id = ch.owner_id
		WHERE ch.project=? AND ch.branch=? AND ch.change_id=?`, project, branch, changeID)
	return scanChange(row.Scan)
}

// GetChangeByChangeIDOnly looks up a change by its Change-Id alone.
func (d *DB) GetChangeByChangeIDOnly(changeID string) (*Change, error) {
	row := d.db.QueryRow(`
		SELECT `+changeCols+`
		FROM changes ch JOIN accounts a ON a.id = ch.owner_id
		WHERE ch.change_id=?`, changeID)
	return scanChange(row.Scan)
}

// ChangeQuery is the parsed form of a Gerrit-style change search.
type ChangeQuery struct {
	Status       string
	Project      string
	Branch       string
	Topic        string
	OwnerID      int64
	OwnerUser    string
	ReviewerUser string
	ChangeNumber int64
	ChangeID     string
	WIP          *bool
	Text         string
	HasVote      bool
	Before       *time.Time
	After        *time.Time
	// StarredAccountID filters to changes starred by this account (is:starred).
	StarredAccountID int64
	// WatchedAccountID filters to changes in projects watched by this account
	// (is:watched).
	WatchedAccountID int64
	Limit            int
	Offset           int
}

// SearchChanges returns changes matching q plus the total number of matches
// (ignoring Limit/Offset) for pagination.
func (d *DB) SearchChanges(q ChangeQuery) ([]*Change, int, error) {
	where := []string{}
	args := []any{}
	add := func(cond string, a ...any) {
		where = append(where, cond)
		args = append(args, a...)
	}
	if q.Status != "" {
		add(`ch.status=?`, q.Status)
	}
	if q.Project != "" {
		add(`ch.project=?`, q.Project)
	}
	if q.Branch != "" {
		add(`ch.branch=?`, q.Branch)
	}
	if q.Topic != "" {
		add(`ch.topic=?`, q.Topic)
	}
	if q.OwnerID > 0 {
		add(`ch.owner_id=?`, q.OwnerID)
	}
	if q.OwnerUser != "" {
		add(`(a.username=? OR a.full_name=?)`, q.OwnerUser, q.OwnerUser)
	}
	if q.ReviewerUser != "" {
		add(`EXISTS (SELECT 1 FROM reviewers rv JOIN accounts ra ON ra.id=rv.account_id
		            WHERE rv.change_number=ch.number AND (ra.username=? OR ra.full_name=?))`,
			q.ReviewerUser, q.ReviewerUser)
	}
	if q.ChangeNumber > 0 {
		add(`ch.number=?`, q.ChangeNumber)
	}
	if q.ChangeID != "" {
		add(`ch.change_id=?`, q.ChangeID)
	}
	if q.WIP != nil {
		add(`ch.work_in_progress=?`, b2i(*q.WIP))
	}
	if q.HasVote {
		add(`EXISTS (SELECT 1 FROM votes v WHERE v.change_number=ch.number)`)
	}
	if q.Before != nil {
		add(`ch.updated < ?`, q.Before.UTC().Format(time.RFC3339Nano))
	}
	if q.After != nil {
		add(`ch.updated > ?`, q.After.UTC().Format(time.RFC3339Nano))
	}
	if q.Text != "" {
		like := "%" + q.Text + "%"
		add(`(ch.subject LIKE ? OR ch.project LIKE ? OR ch.change_id LIKE ? OR CAST(ch.number AS TEXT) LIKE ?)`,
			like, like, like, like)
	}
	if q.StarredAccountID > 0 {
		add(`EXISTS (SELECT 1 FROM starred st WHERE st.change_number=ch.number AND st.account_id=?)`,
			q.StarredAccountID)
	}
	if q.WatchedAccountID > 0 {
		add(`EXISTS (SELECT 1 FROM watched_projects wp WHERE wp.project=ch.project AND wp.account_id=?)`,
			q.WatchedAccountID)
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ") + " "
	}
	fromSQL := `FROM changes ch JOIN accounts a ON a.id = ch.owner_id `

	var total int
	if err := d.db.QueryRow(`SELECT COUNT(*) `+fromSQL+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := d.db.Query(`SELECT `+changeCols+` `+fromSQL+whereSQL+
		`ORDER BY ch.updated DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Change
	for rows.Next() {
		c, err := scanChange(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (d *DB) UpdateChangeStatus(number int64, status string, submitted *time.Time) error {
	var sub any
	if submitted != nil {
		sub = submitted.UTC().Format(time.RFC3339Nano)
	}
	_, err := d.db.Exec(`UPDATE changes SET status=?, submitted=?, updated=? WHERE number=?`, status, sub, now(), number)
	return err
}

func (d *DB) SetCurrentPatchSet(number int64, ps int) error {
	_, err := d.db.Exec(`UPDATE changes SET current_ps=?, updated=? WHERE number=?`, ps, now(), number)
	return err
}

func (d *DB) TouchChange(number int64) error {
	_, err := d.db.Exec(`UPDATE changes SET updated=? WHERE number=?`, now(), number)
	return err
}

// ---------- patch sets ----------

func (d *DB) CreatePatchSet(p *PatchSet) error {
	_, err := d.db.Exec(
		`INSERT INTO patchsets(change_number, number, commit_sha, author_name, author_email, message, created) VALUES(?,?,?,?,?,?,?)`,
		p.ChangeNumber, p.Number, p.CommitSHA, p.AuthorName, p.AuthorEmail, p.Message, now())
	if err != nil {
		return err
	}
	p.Created = parseTime(now())
	return nil
}

func (d *DB) GetPatchSet(changeNumber int64, number int) (*PatchSet, error) {
	p := &PatchSet{}
	var created string
	err := d.db.QueryRow(`SELECT change_number, number, commit_sha, author_name, author_email, message, created FROM patchsets WHERE change_number=? AND number=?`,
		changeNumber, number).Scan(&p.ChangeNumber, &p.Number, &p.CommitSHA, &p.AuthorName, &p.AuthorEmail, &p.Message, &created)
	if err != nil {
		return nil, err
	}
	p.Created = parseTime(created)
	return p, nil
}

func (d *DB) ListPatchSets(changeNumber int64) ([]*PatchSet, error) {
	rows, err := d.db.Query(`SELECT change_number, number, commit_sha, author_name, author_email, message, created FROM patchsets WHERE change_number=? ORDER BY number`, changeNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PatchSet
	for rows.Next() {
		p := &PatchSet{}
		var created string
		if err := rows.Scan(&p.ChangeNumber, &p.Number, &p.CommitSHA, &p.AuthorName, &p.AuthorEmail, &p.Message, &created); err != nil {
			return nil, err
		}
		p.Created = parseTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetCurrentPatchSetsBatch returns the current patch set for each change in one query.
func (d *DB) GetCurrentPatchSetsBatch(changes []*Change) (map[int64]*PatchSet, error) {
	out := map[int64]*PatchSet{}
	if len(changes) == 0 {
		return out, nil
	}
	ph := make([]string, len(changes))
	args := make([]any, 0, len(changes)*2)
	for i, c := range changes {
		ph[i] = "(?,?)"
		args = append(args, c.Number, c.CurrentPS)
	}
	rows, err := d.db.Query(`
		SELECT change_number, number, commit_sha, author_name, author_email, message, created
		FROM patchsets WHERE (change_number, number) IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p := &PatchSet{}
		var created string
		if err := rows.Scan(&p.ChangeNumber, &p.Number, &p.CommitSHA, &p.AuthorName, &p.AuthorEmail, &p.Message, &created); err != nil {
			return nil, err
		}
		p.Created = parseTime(created)
		out[p.ChangeNumber] = p
	}
	return out, rows.Err()
}

// ListAllPatchSets returns every patch set across all changes, for the
// patchset_files backfill.
func (d *DB) ListAllPatchSets() ([]*PatchSet, error) {
	rows, err := d.db.Query(`SELECT change_number, number, commit_sha, author_name, author_email, message, created FROM patchsets ORDER BY change_number, number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PatchSet
	for rows.Next() {
		p := &PatchSet{}
		var created string
		if err := rows.Scan(&p.ChangeNumber, &p.Number, &p.CommitSHA, &p.AuthorName, &p.AuthorEmail, &p.Message, &created); err != nil {
			return nil, err
		}
		p.Created = parseTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddPatchSetFiles records the file paths a patch set touches, replacing any
// existing rows for that patch set.
func (d *DB) AddPatchSetFiles(changeNumber int64, psNumber int, paths []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM patchset_files WHERE change_number=? AND patch_set=?`, changeNumber, psNumber); err != nil {
		return err
	}
	for _, p := range paths {
		if _, err := tx.Exec(
			`INSERT INTO patchset_files(change_number, patch_set, file_path) VALUES(?,?,?)
			 ON CONFLICT(change_number, patch_set, file_path) DO NOTHING`, changeNumber, psNumber, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListPatchSetFiles returns the file paths a patch set touches.
func (d *DB) ListPatchSetFiles(changeNumber int64, psNumber int) ([]string, error) {
	rows, err := d.db.Query(`SELECT file_path FROM patchset_files WHERE change_number=? AND patch_set=? ORDER BY file_path`, changeNumber, psNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- votes ----------

func (d *DB) SetVote(v *Vote) error {
	_, err := d.db.Exec(`
		INSERT INTO votes(change_number, patch_set, account_id, label, value) VALUES(?,?,?,?,?)
		ON CONFLICT(change_number, patch_set, account_id, label) DO UPDATE SET value=excluded.value`,
		v.ChangeNumber, v.PatchSet, v.AccountID, v.Label, v.Value)
	return err
}

func (d *DB) DeleteVote(changeNumber int64, patchSet int, accountID int64, label string) error {
	_, err := d.db.Exec(`DELETE FROM votes WHERE change_number=? AND patch_set=? AND account_id=? AND label=?`,
		changeNumber, patchSet, accountID, label)
	return err
}

type VoteInfo struct {
	Vote
	AccountName string
	AccountUser string
}

func (d *DB) ListVotes(changeNumber int64) ([]*VoteInfo, error) {
	rows, err := d.db.Query(`
		SELECT v.change_number, v.patch_set, v.account_id, v.label, v.value, a.full_name, a.username
		FROM votes v JOIN accounts a ON a.id = v.account_id WHERE v.change_number=? ORDER BY v.patch_set`, changeNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*VoteInfo
	for rows.Next() {
		v := &VoteInfo{}
		if err := rows.Scan(&v.ChangeNumber, &v.PatchSet, &v.AccountID, &v.Label, &v.Value, &v.AccountName, &v.AccountUser); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListVotesBatch returns votes for many changes in one query, grouped by change number.
func (d *DB) ListVotesBatch(changeNumbers []int64) (map[int64][]*VoteInfo, error) {
	out := map[int64][]*VoteInfo{}
	if len(changeNumbers) == 0 {
		return out, nil
	}
	ph := make([]string, len(changeNumbers))
	args := make([]any, len(changeNumbers))
	for i, n := range changeNumbers {
		ph[i] = "?"
		args[i] = n
	}
	rows, err := d.db.Query(`
		SELECT v.change_number, v.patch_set, v.account_id, v.label, v.value, a.full_name, a.username
		FROM votes v JOIN accounts a ON a.id = v.account_id
		WHERE v.change_number IN (`+strings.Join(ph, ",")+`) ORDER BY v.change_number, v.patch_set`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		v := &VoteInfo{}
		if err := rows.Scan(&v.ChangeNumber, &v.PatchSet, &v.AccountID, &v.Label, &v.Value, &v.AccountName, &v.AccountUser); err != nil {
			return nil, err
		}
		out[v.ChangeNumber] = append(out[v.ChangeNumber], v)
	}
	return out, rows.Err()
}

// ---------- comments ----------

func (d *DB) CreateComment(c *Comment) error {
	id, err := d.insertID(`
		INSERT INTO comments(change_number, patch_set, file, line, message, author_id, in_reply_to, robot_id, robot_run_id, created)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"id",
		c.ChangeNum, c.PatchSet, c.File, c.Line, c.Message, c.AuthorID, c.InReplyTo, c.RobotID, c.RobotRunID, now())
	if err != nil {
		return err
	}
	c.ID = id
	c.Created = parseTime(now())
	return nil
}

func (d *DB) ListComments(changeNumber int64) ([]*Comment, error) {
	rows, err := d.db.Query(`
		SELECT c.id, c.change_number, c.patch_set, c.file, c.line, c.message, c.author_id, c.in_reply_to, c.resolved, c.robot_id, c.robot_run_id, c.created,
		       a.full_name, a.username
		FROM comments c JOIN accounts a ON a.id = c.author_id
		WHERE c.change_number=? ORDER BY c.created`, changeNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Comment
	for rows.Next() {
		c := &Comment{}
		var created string
		var resolved int
		if err := rows.Scan(&c.ID, &c.ChangeNum, &c.PatchSet, &c.File, &c.Line, &c.Message, &c.AuthorID, &c.InReplyTo, &resolved, &c.RobotID, &c.RobotRunID, &created,
			&c.AuthorName, &c.AuthorUser); err != nil {
			return nil, err
		}
		c.Resolved = resolved != 0
		c.Created = parseTime(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetCommentResolved toggles the resolved flag of a comment thread.
func (d *DB) SetCommentResolved(id int64, resolved bool) error {
	_, err := d.db.Exec(`UPDATE comments SET resolved=? WHERE id=?`, b2i(resolved), id)
	return err
}

// GetComment returns a single comment by ID (without author join).
func (d *DB) GetComment(id int64) (*Comment, error) {
	c := &Comment{}
	var created string
	var resolved int
	err := d.db.QueryRow(`
		SELECT id, change_number, patch_set, file, line, message, author_id, in_reply_to, resolved, created
		FROM comments WHERE id=?`, id).
		Scan(&c.ID, &c.ChangeNum, &c.PatchSet, &c.File, &c.Line, &c.Message, &c.AuthorID, &c.InReplyTo, &resolved, &created)
	if err != nil {
		return nil, err
	}
	c.Resolved = resolved != 0
	c.Created = parseTime(created)
	return c, nil
}

// ---------- comment drafts ----------

func (d *DB) CreateDraft(dr *CommentDraft) error {
	id, err := d.insertID(`
		INSERT INTO comment_drafts(change_number, patch_set, account_id, file, line, message, in_reply_to, created)
		VALUES(?,?,?,?,?,?,?,?)`,
		"id",
		dr.ChangeNum, dr.PatchSet, dr.AccountID, dr.File, dr.Line, dr.Message, dr.InReplyTo, now())
	if err != nil {
		return err
	}
	dr.ID = id
	dr.Created = parseTime(now())
	return nil
}

func (d *DB) UpdateDraft(accountID, id int64, message string, inReplyTo int64) error {
	_, err := d.db.Exec(`UPDATE comment_drafts SET message=?, in_reply_to=?, created=? WHERE id=? AND account_id=?`,
		message, inReplyTo, now(), id, accountID)
	return err
}

func (d *DB) DeleteDraft(accountID, id int64) error {
	_, err := d.db.Exec(`DELETE FROM comment_drafts WHERE id=? AND account_id=?`, id, accountID)
	return err
}

func (d *DB) ListDrafts(changeNumber, accountID int64) ([]*CommentDraft, error) {
	rows, err := d.db.Query(`
		SELECT id, change_number, patch_set, account_id, file, line, message, in_reply_to, created
		FROM comment_drafts WHERE change_number=? AND account_id=? ORDER BY id`, changeNumber, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CommentDraft
	for rows.Next() {
		dr := &CommentDraft{}
		var created string
		if err := rows.Scan(&dr.ID, &dr.ChangeNum, &dr.PatchSet, &dr.AccountID, &dr.File, &dr.Line, &dr.Message, &dr.InReplyTo, &created); err != nil {
			return nil, err
		}
		dr.Created = parseTime(created)
		out = append(out, dr)
	}
	return out, rows.Err()
}

// PublishDrafts converts all of an account's drafts on a change into published
// comments and deletes the drafts. It returns the number published.
func (d *DB) PublishDrafts(changeNumber, accountID int64) (int, error) {
	drafts, err := d.ListDrafts(changeNumber, accountID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, dr := range drafts {
		if err := d.CreateComment(&Comment{
			ChangeNum: dr.ChangeNum, PatchSet: dr.PatchSet, File: dr.File,
			Line: dr.Line, Message: dr.Message, AuthorID: accountID, InReplyTo: dr.InReplyTo,
		}); err != nil {
			return n, err
		}
		if _, err := d.db.Exec(`DELETE FROM comment_drafts WHERE id=?`, dr.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ---------- change metadata (topic / WIP / private) ----------

func (d *DB) SetTopic(number int64, topic string) error {
	_, err := d.db.Exec(`UPDATE changes SET topic=?, updated=? WHERE number=?`, topic, now(), number)
	return err
}

func (d *DB) SetWorkInProgress(number int64, wip bool) error {
	_, err := d.db.Exec(`UPDATE changes SET work_in_progress=?, updated=? WHERE number=?`, b2i(wip), now(), number)
	return err
}

func (d *DB) SetPrivate(number int64, priv bool) error {
	_, err := d.db.Exec(`UPDATE changes SET private=?, updated=? WHERE number=?`, b2i(priv), now(), number)
	return err
}

// ---------- change messages (unified timeline) ----------

func (d *DB) AddChangeMessage(m *ChangeMessage) error {
	id, err := d.insertID(
		`INSERT INTO change_messages(change_number, patch_set, type, author_id, message, created) VALUES(?,?,?,?,?,?)`,
		"id",
		m.ChangeNum, m.PatchSet, m.Type, m.AuthorID, m.Message, now())
	if err != nil {
		return err
	}
	m.ID = id
	m.Created = parseTime(now())
	return nil
}

func (d *DB) ListChangeMessages(changeNumber int64) ([]*ChangeMessage, error) {
	rows, err := d.db.Query(`
		SELECT m.id, m.change_number, m.patch_set, m.type, m.author_id, m.message, m.created,
		       COALESCE(a.full_name, ''), COALESCE(a.username, '')
		FROM change_messages m LEFT JOIN accounts a ON a.id = m.author_id
		WHERE m.change_number=? ORDER BY m.id`, changeNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ChangeMessage
	for rows.Next() {
		m := &ChangeMessage{}
		var created string
		if err := rows.Scan(&m.ID, &m.ChangeNum, &m.PatchSet, &m.Type, &m.AuthorID, &m.Message, &created,
			&m.AuthorName, &m.AuthorUser); err != nil {
			return nil, err
		}
		m.Created = parseTime(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------- reviewers ----------

func (d *DB) AddReviewer(changeNumber, accountID int64) error {
	_, err := d.db.Exec(
		`INSERT INTO reviewers(change_number, account_id, added) VALUES(?,?,?)
		 ON CONFLICT(change_number, account_id) DO NOTHING`,
		changeNumber, accountID, now())
	return err
}

func (d *DB) RemoveReviewer(changeNumber, accountID int64) error {
	_, err := d.db.Exec(`DELETE FROM reviewers WHERE change_number=? AND account_id=?`, changeNumber, accountID)
	return err
}

func (d *DB) ListReviewers(changeNumber int64) ([]*Reviewer, error) {
	rows, err := d.db.Query(`
		SELECT rv.change_number, rv.account_id, a.full_name, a.username, a.email, rv.added
		FROM reviewers rv JOIN accounts a ON a.id = rv.account_id
		WHERE rv.change_number=? ORDER BY rv.added`, changeNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Reviewer
	for rows.Next() {
		r := &Reviewer{}
		var added string
		if err := rows.Scan(&r.ChangeNumber, &r.AccountID, &r.Name, &r.Username, &r.Email, &added); err != nil {
			return nil, err
		}
		r.Added = parseTime(added)
		if r.Name == "" {
			r.Name = r.Username
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListReviewersBatch returns reviewers for many changes in one query, grouped by change number.
func (d *DB) ListReviewersBatch(changeNumbers []int64) (map[int64][]*Reviewer, error) {
	out := map[int64][]*Reviewer{}
	if len(changeNumbers) == 0 {
		return out, nil
	}
	ph := make([]string, len(changeNumbers))
	args := make([]any, len(changeNumbers))
	for i, n := range changeNumbers {
		ph[i] = "?"
		args[i] = n
	}
	rows, err := d.db.Query(`
		SELECT rv.change_number, rv.account_id, a.full_name, a.username, a.email, rv.added
		FROM reviewers rv JOIN accounts a ON a.id = rv.account_id
		WHERE rv.change_number IN (`+strings.Join(ph, ",")+`) ORDER BY rv.change_number, rv.added`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := &Reviewer{}
		var added string
		if err := rows.Scan(&r.ChangeNumber, &r.AccountID, &r.Name, &r.Username, &r.Email, &added); err != nil {
			return nil, err
		}
		r.Added = parseTime(added)
		if r.Name == "" {
			r.Name = r.Username
		}
		out[r.ChangeNumber] = append(out[r.ChangeNumber], r)
	}
	return out, rows.Err()
}

// SuggestReviewers ranks candidate reviewers for a change by how frequently
// each account has participated in the same project's recent changes (as a
// reviewer or as an owner), excluding the given account IDs (the change owner
// and its current reviewers). It returns up to limit accounts, most active
// first.
func (d *DB) SuggestReviewers(project string, excludeChange int64, excludeIDs []int64, limit int) ([]*Account, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	// Count participation across the project's other changes, weighting
	// reviewer and owner activity equally.
	rows, err := d.db.Query(`
		SELECT account_id, COUNT(*) AS freq FROM (
			SELECT rv.account_id AS account_id FROM reviewers rv
			JOIN changes c ON c.number = rv.change_number
			WHERE c.project = ? AND c.number != ?
			UNION ALL
			SELECT c.owner_id AS account_id FROM changes c
			WHERE c.project = ? AND c.number != ?
		) GROUP BY account_id ORDER BY freq DESC LIMIT ?`, project, excludeChange, project, excludeChange, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	excluded := map[int64]bool{}
	for _, id := range excludeIDs {
		excluded[id] = true
	}
	var out []*Account
	for rows.Next() {
		var id, freq int64
		if err := rows.Scan(&id, &freq); err != nil {
			return nil, err
		}
		if excluded[id] {
			continue
		}
		a, err := d.GetAccount(id)
		if err != nil {
			continue
		}
		out = append(out, a)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// insertID executes an INSERT and returns the new row's id column. SQLite
// reports it via LastInsertId; PostgreSQL requires a RETURNING clause.
func (d *DB) insertID(q, idCol string, args ...any) (int64, error) {
	if d.driver == DriverPostgres {
		var id int64
		err := d.db.QueryRow(q+" RETURNING "+idCol, args...).Scan(&id)
		return id, err
	}
	res, err := d.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
