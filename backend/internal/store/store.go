package store

import (
	"database/sql"
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
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Head        string    `json:"-"`
	Created     time.Time `json:"-"`
}

type Change struct {
	Number      int64     `json:"_number"`
	Project     string    `json:"project"`
	Branch      string    `json:"branch"`
	ChangeID    string    `json:"change_id"`
	Subject     string    `json:"subject"`
	OwnerID     int64     `json:"-"`
	Status      string    `json:"status"` // NEW, MERGED, ABANDONED
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	Submitted   *time.Time `json:"submitted,omitempty"`
	CurrentPS   int       `json:"-"`
	OwnerName   string    `json:"-"`
	OwnerEmail  string    `json:"-"`
	OwnerUser   string    `json:"-"`
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
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc sqlite + WAL: single writer avoids contention
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

type DB struct {
	db *sql.DB
}

func (d *DB) Close() error { return d.db.Close() }

func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  full_name TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  admin INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  expires TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS projects (
  name TEXT PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  head TEXT NOT NULL DEFAULT 'master',
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
  current_ps INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL,
  updated TEXT NOT NULL,
  submitted TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_changes_cid ON changes(project, branch, change_id);
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
  created TEXT NOT NULL
);`
	_, err := db.Exec(schema)
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
	res, err := d.db.Exec(
		`INSERT INTO accounts(username, password_hash, full_name, email, admin, created) VALUES(?,?,?,?,?,?)`,
		a.Username, a.PasswordHash, a.FullName, a.Email, b2i(a.Admin), now())
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
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
	_, err := d.db.Exec(`INSERT INTO projects(name, description, head, created) VALUES(?,?,?,?)`,
		p.Name, p.Description, p.Head, now())
	return err
}

func (d *DB) GetProject(name string) (*Project, error) {
	p := &Project{}
	var created string
	err := d.db.QueryRow(`SELECT name, description, head, created FROM projects WHERE name=?`, name).
		Scan(&p.Name, &p.Description, &p.Head, &created)
	if err != nil {
		return nil, err
	}
	p.Created = parseTime(created)
	return p, nil
}

func (d *DB) ListProjects() ([]*Project, error) {
	rows, err := d.db.Query(`SELECT name, description, head, created FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p := &Project{}
		var created string
		if err := rows.Scan(&p.Name, &p.Description, &p.Head, &created); err != nil {
			return nil, err
		}
		p.Created = parseTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- changes ----------

func (d *DB) CreateChange(c *Change) error {
	res, err := d.db.Exec(
		`INSERT INTO changes(project, branch, change_id, subject, owner_id, status, current_ps, created, updated) VALUES(?,?,?,?,?,?,?,?,?)`,
		c.Project, c.Branch, c.ChangeID, c.Subject, c.OwnerID, "NEW", c.CurrentPS, now(), now())
	if err != nil {
		return err
	}
	c.Number, _ = res.LastInsertId()
	c.Status = "NEW"
	c.Created = parseTime(now())
	c.Updated = c.Created
	return nil
}

func (d *DB) GetChange(number int64) (*Change, error) {
	c := &Change{}
	var created, updated string
	var submitted sql.NullString
	err := d.db.QueryRow(`
		SELECT ch.number, ch.project, ch.branch, ch.change_id, ch.subject, ch.owner_id, ch.status,
		       ch.current_ps, ch.created, ch.updated, ch.submitted,
		       a.full_name, a.email, a.username
		FROM changes ch JOIN accounts a ON a.id = ch.owner_id WHERE ch.number=?`, number).
		Scan(&c.Number, &c.Project, &c.Branch, &c.ChangeID, &c.Subject, &c.OwnerID, &c.Status,
			&c.CurrentPS, &created, &updated, &submitted,
			&c.OwnerName, &c.OwnerEmail, &c.OwnerUser)
	if err != nil {
		return nil, err
	}
	c.Created = parseTime(created)
	c.Updated = parseTime(updated)
	c.Submitted = parseTimePtr(submitted)
	return c, nil
}

func (d *DB) GetChangeByChangeID(project, branch, changeID string) (*Change, error) {
	c := &Change{}
	var created, updated string
	var submitted sql.NullString
	err := d.db.QueryRow(`
		SELECT ch.number, ch.project, ch.branch, ch.change_id, ch.subject, ch.owner_id, ch.status,
		       ch.current_ps, ch.created, ch.updated, ch.submitted,
		       a.full_name, a.email, a.username
		FROM changes ch JOIN accounts a ON a.id = ch.owner_id
		WHERE ch.project=? AND ch.branch=? AND ch.change_id=?`, project, branch, changeID).
		Scan(&c.Number, &c.Project, &c.Branch, &c.ChangeID, &c.Subject, &c.OwnerID, &c.Status,
			&c.CurrentPS, &created, &updated, &submitted,
			&c.OwnerName, &c.OwnerEmail, &c.OwnerUser)
	if err != nil {
		return nil, err
	}
	c.Created = parseTime(created)
	c.Updated = parseTime(updated)
	c.Submitted = parseTimePtr(submitted)
	return c, nil
}

func (d *DB) ListChanges(status string, limit int) ([]*Change, error) {
	q := `
		SELECT ch.number, ch.project, ch.branch, ch.change_id, ch.subject, ch.owner_id, ch.status,
		       ch.current_ps, ch.created, ch.updated, ch.submitted,
		       a.full_name, a.email, a.username
		FROM changes ch JOIN accounts a ON a.id = ch.owner_id `
	args := []any{}
	if status != "" {
		q += `WHERE ch.status=? `
		args = append(args, status)
	}
	q += `ORDER BY ch.updated DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Change
	for rows.Next() {
		c := &Change{}
		var created, updated string
		var submitted sql.NullString
		if err := rows.Scan(&c.Number, &c.Project, &c.Branch, &c.ChangeID, &c.Subject, &c.OwnerID, &c.Status,
			&c.CurrentPS, &created, &updated, &submitted,
			&c.OwnerName, &c.OwnerEmail, &c.OwnerUser); err != nil {
			return nil, err
		}
		c.Created = parseTime(created)
		c.Updated = parseTime(updated)
		c.Submitted = parseTimePtr(submitted)
		out = append(out, c)
	}
	return out, rows.Err()
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

// ---------- comments ----------

func (d *DB) CreateComment(c *Comment) error {
	res, err := d.db.Exec(`
		INSERT INTO comments(change_number, patch_set, file, line, message, author_id, in_reply_to, created)
		VALUES(?,?,?,?,?,?,?,?)`,
		c.ChangeNum, c.PatchSet, c.File, c.Line, c.Message, c.AuthorID, c.InReplyTo, now())
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	c.Created = parseTime(now())
	return nil
}

func (d *DB) ListComments(changeNumber int64) ([]*Comment, error) {
	rows, err := d.db.Query(`
		SELECT c.id, c.change_number, c.patch_set, c.file, c.line, c.message, c.author_id, c.in_reply_to, c.created,
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
		if err := rows.Scan(&c.ID, &c.ChangeNum, &c.PatchSet, &c.File, &c.Line, &c.Message, &c.AuthorID, &c.InReplyTo, &created,
			&c.AuthorName, &c.AuthorUser); err != nil {
			return nil, err
		}
		c.Created = parseTime(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
