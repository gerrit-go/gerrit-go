package store

import (
	"database/sql"
	"time"
)

// SSHKey is a public SSH key registered by an account. Keys are stored for
// management/identification; git transport in this build is HTTP-based.
type SSHKey struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"-"`
	PublicKey string    `json:"public_key"`
	Comment   string    `json:"comment"`
	Created   time.Time `json:"created"`
}

// UpdateAccountProfile updates the display name and email of an account.
func (d *DB) UpdateAccountProfile(id int64, fullName, email string) error {
	_, err := d.db.Exec(`UPDATE accounts SET full_name=?, email=? WHERE id=?`, fullName, email, id)
	return err
}

// GetHTTPPasswordHash returns the bcrypt hash of an account's dedicated HTTP
// password, or "" when none is set.
func (d *DB) GetHTTPPasswordHash(id int64) (string, error) {
	var hash string
	err := d.db.QueryRow(`SELECT http_password_hash FROM accounts WHERE id=?`, id).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

// SetHTTPPasswordHash stores (or clears, when hash=="") an account's HTTP password hash.
func (d *DB) SetHTTPPasswordHash(id int64, hash string) error {
	_, err := d.db.Exec(`UPDATE accounts SET http_password_hash=? WHERE id=?`, hash, id)
	return err
}

// GetAccountByExternalID looks up an account by its OAuth/external identity.
func (d *DB) GetAccountByExternalID(externalID string) (*Account, error) {
	if externalID == "" {
		return nil, sql.ErrNoRows
	}
	a := &Account{}
	var admin int
	var created string
	err := d.db.QueryRow(
		`SELECT id, username, password_hash, full_name, email, admin, created FROM accounts WHERE external_id=?`,
		externalID).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.FullName, &a.Email, &admin, &created)
	if err != nil {
		return nil, err
	}
	a.Admin = admin == 1
	a.Created = parseTime(created)
	return a, nil
}

// GetAccountByEmail looks up an account by email (used to link an OAuth login
// to a pre-existing local account).
func (d *DB) GetAccountByEmail(email string) (*Account, error) {
	if email == "" {
		return nil, sql.ErrNoRows
	}
	a := &Account{}
	var admin int
	var created string
	err := d.db.QueryRow(
		`SELECT id, username, password_hash, full_name, email, admin, created FROM accounts WHERE email=? LIMIT 1`,
		email).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.FullName, &a.Email, &admin, &created)
	if err != nil {
		return nil, err
	}
	a.Admin = admin == 1
	a.Created = parseTime(created)
	return a, nil
}

// SetExternalID links an OAuth/external identity to an account.
func (d *DB) SetExternalID(id int64, externalID string) error {
	_, err := d.db.Exec(`UPDATE accounts SET external_id=? WHERE id=?`, externalID, id)
	return err
}

func (d *DB) CreateSSHKey(k *SSHKey) error {
	id, err := d.insertID(`INSERT INTO ssh_keys(account_id, public_key, comment, created) VALUES(?,?,?,?)`,
		"id",
		k.AccountID, k.PublicKey, k.Comment, now())
	if err != nil {
		return err
	}
	k.ID = id
	k.Created = parseTime(now())
	return nil
}

func (d *DB) ListSSHKeys(accountID int64) ([]*SSHKey, error) {
	rows, err := d.db.Query(`SELECT id, account_id, public_key, comment, created FROM ssh_keys WHERE account_id=? ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SSHKey
	for rows.Next() {
		k := &SSHKey{}
		var created string
		if err := rows.Scan(&k.ID, &k.AccountID, &k.PublicKey, &k.Comment, &created); err != nil {
			return nil, err
		}
		k.Created = parseTime(created)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSSHKey(accountID, id int64) error {
	_, err := d.db.Exec(`DELETE FROM ssh_keys WHERE account_id=? AND id=?`, accountID, id)
	return err
}

// FindAccountBySSHPublicKey resolves an account from a normalized public key
// ("algo base64"), used to authenticate git+ssh sessions.
func (d *DB) FindAccountBySSHPublicKey(publicKey string) (*Account, error) {
	if publicKey == "" {
		return nil, sql.ErrNoRows
	}
	a := &Account{}
	var admin int
	var created string
	err := d.db.QueryRow(
		`SELECT a.id, a.username, a.password_hash, a.full_name, a.email, a.admin, a.created
		   FROM accounts a JOIN ssh_keys k ON k.account_id = a.id
		  WHERE k.public_key=? LIMIT 1`,
		publicKey).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.FullName, &a.Email, &admin, &created)
	if err != nil {
		return nil, err
	}
	a.Admin = admin == 1
	a.Created = parseTime(created)
	return a, nil
}

// GetTOTP returns an account's TOTP secret and whether 2FA is currently
// enforced. A non-empty secret with enabled=false represents a pending
// enrollment that has not yet been confirmed.
func (d *DB) GetTOTP(id int64) (secret string, enabled bool, err error) {
	var en int
	err = d.db.QueryRow(`SELECT totp_secret, totp_enabled FROM accounts WHERE id=?`, id).Scan(&secret, &en)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return secret, en == 1, err
}

// SetTOTPSecret stores (or clears, when secret=="") an account's TOTP secret.
func (d *DB) SetTOTPSecret(id int64, secret string) error {
	_, err := d.db.Exec(`UPDATE accounts SET totp_secret=? WHERE id=?`, secret, id)
	return err
}

// SetTOTPEnabled toggles whether 2FA is enforced at login for an account.
func (d *DB) SetTOTPEnabled(id int64, enabled bool) error {
	_, err := d.db.Exec(`UPDATE accounts SET totp_enabled=? WHERE id=?`, b2i(enabled), id)
	return err
}
