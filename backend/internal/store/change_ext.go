package store

import (
	"database/sql"
	"strings"
	"time"
)

// AttentionEntry marks an account as needing to act on a change.
type AttentionEntry struct {
	AccountID int64     `json:"_account_id"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	Email     string    `json:"email,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Added     time.Time `json:"-"`
}

// ---------- hashtags ----------

// AddHashtag attaches a hashtag to a change (idempotent). Hashtags are
// normalized to lower-case without the leading '#'.
func (d *DB) AddHashtag(changeNum int64, tag string) error {
	tag = NormalizeHashtag(tag)
	if tag == "" {
		return nil
	}
	_, err := d.db.Exec(
		`INSERT INTO change_hashtags(change_number, hashtag, added) VALUES(?,?,?)
		 ON CONFLICT(change_number, hashtag) DO NOTHING`, changeNum, tag, now())
	return err
}

// ListHashtags returns the hashtags of a change, alphabetically.
func (d *DB) ListHashtags(changeNum int64) ([]string, error) {
	rows, err := d.db.Query(`SELECT hashtag FROM change_hashtags WHERE change_number=? ORDER BY hashtag`, changeNum)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteHashtag removes a hashtag from a change.
func (d *DB) DeleteHashtag(changeNum int64, tag string) error {
	_, err := d.db.Exec(`DELETE FROM change_hashtags WHERE change_number=? AND hashtag=?`,
		changeNum, NormalizeHashtag(tag))
	return err
}

// NormalizeHashtag strips a leading '#' and surrounding whitespace and
// lower-cases the tag so lookups are case-insensitive.
func NormalizeHashtag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "#")
	return strings.ToLower(strings.TrimSpace(tag))
}

// ---------- assignee ----------

// SetAssignee assigns a change to an account; accountID==0 clears the assignee.
func (d *DB) SetAssignee(changeNum, accountID int64) error {
	_, err := d.db.Exec(`UPDATE changes SET assignee_id=?, updated=? WHERE number=?`,
		accountID, now(), changeNum)
	return err
}

// GetAssignee returns the account currently assigned to a change, or nil when
// unassigned.
func (d *DB) GetAssignee(changeNum int64) (*Account, error) {
	var id int64
	if err := d.db.QueryRow(`SELECT assignee_id FROM changes WHERE number=?`, changeNum).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if id == 0 {
		return nil, nil
	}
	return d.GetAccount(id)
}

// ---------- attention set ----------

// AddAttention adds an account to a change's attention set (idempotent).
func (d *DB) AddAttention(changeNum, accountID int64, reason string) error {
	_, err := d.db.Exec(
		`INSERT INTO change_attention(change_number, account_id, reason, added) VALUES(?,?,?,?)
		 ON CONFLICT(change_number, account_id) DO UPDATE SET reason=excluded.reason`,
		changeNum, accountID, reason, now())
	return err
}

// ListAttention returns the attention set of a change with account details.
func (d *DB) ListAttention(changeNum int64) ([]*AttentionEntry, error) {
	rows, err := d.db.Query(`
		SELECT t.account_id, a.full_name, a.username, a.email, t.reason, t.added
		FROM change_attention t JOIN accounts a ON a.id = t.account_id
		WHERE t.change_number=? ORDER BY t.added`, changeNum)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AttentionEntry
	for rows.Next() {
		e := &AttentionEntry{}
		var added string
		if err := rows.Scan(&e.AccountID, &e.Name, &e.Username, &e.Email, &e.Reason, &added); err != nil {
			return nil, err
		}
		e.Added = parseTime(added)
		out = append(out, e)
	}
	return out, rows.Err()
}

// RemoveAttention drops an account from a change's attention set.
func (d *DB) RemoveAttention(changeNum, accountID int64) error {
	_, err := d.db.Exec(`DELETE FROM change_attention WHERE change_number=? AND account_id=?`,
		changeNum, accountID)
	return err
}

// ---------- project deletion ----------

// DeleteProject removes a project row and all of its changes. Child rows of
// changes (patch sets, votes, comments, reviewers, messages, hashtags,
// attention) cascade from changes; the project row is deleted last. The caller
// is responsible for removing the on-disk repository via gitsvc.DeleteProject.
func (d *DB) DeleteProject(name string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM changes WHERE project=?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM access_rules WHERE project=?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM submit_requirements WHERE project=?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM watched_projects WHERE project=?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM webhooks WHERE project=?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE name=?`, name); err != nil {
		return err
	}
	return tx.Commit()
}
