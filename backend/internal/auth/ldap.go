package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"gerrit-go/internal/store"

	"github.com/go-ldap/ldap/v3"
	"golang.org/x/crypto/bcrypt"
)

// LDAPConfig describes an LDAP/AD directory used to authenticate logins.
// Authentication is search-then-bind: optionally bind a service account, locate
// the user's DN via UserFilter, then re-bind as that DN with the supplied
// password. Successful logins JIT-provision a local account.
type LDAPConfig struct {
	URL        string // ldap://host:389 or ldaps://host:636
	BindDN     string // service account DN (empty => anonymous search)
	BindPass   string // service account password
	BaseDN     string // search base, e.g. ou=people,dc=example,dc=com
	UserFilter string // filter with a single %s for the username, e.g. (uid=%s)
	Attr       string // username attribute (default uid)
	EmailAttr  string // email attribute (default mail)
	NameAttr   string // display-name attribute (default cn)
	Insecure   bool   // skip TLS certificate verification for ldaps
}

// Enabled reports whether enough of the config is present to authenticate.
func (c LDAPConfig) Enabled() bool { return c.URL != "" && c.BaseDN != "" }

func (c LDAPConfig) normalize() LDAPConfig {
	if c.Attr == "" {
		c.Attr = "uid"
	}
	if c.EmailAttr == "" {
		c.EmailAttr = "mail"
	}
	if c.NameAttr == "" {
		c.NameAttr = "cn"
	}
	if c.UserFilter == "" {
		c.UserFilter = "(" + c.Attr + "=%s)"
	}
	if !strings.Contains(c.UserFilter, "%s") {
		c.UserFilter = "(" + c.Attr + "=%s)"
	}
	return c
}

// ConfigureLDAP enables directory authentication. It is a no-op when cfg is not
// sufficiently populated (see LDAPConfig.Enabled).
func (s *Service) ConfigureLDAP(cfg LDAPConfig) {
	s.ldap = cfg.normalize()
}

// LDAPEnabled reports whether an LDAP directory is configured.
func (s *Service) LDAPEnabled() bool { return s.ldap.Enabled() }

const ldapExternalPrefix = "ldap:"

// authenticateLDAP verifies username/password against the directory and returns
// the matching (JIT-provisioned) local account.
func (s *Service) authenticateLDAP(username, password string) (*store.Account, error) {
	if !s.ldap.Enabled() || username == "" || password == "" {
		return nil, ErrUnauthorized
	}
	cfg := s.ldap

	var conn *ldap.Conn
	var err error
	if cfg.Insecure {
		conn, err = ldap.DialURL(cfg.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	} else {
		conn, err = ldap.DialURL(cfg.URL)
	}
	if err != nil {
		return nil, ErrUnauthorized
	}
	defer conn.Close()

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPass); err != nil {
			return nil, ErrUnauthorized
		}
	}

	filter := fmt.Sprintf(cfg.UserFilter, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		1, 0, false, filter, []string{cfg.Attr, cfg.EmailAttr, cfg.NameAttr}, nil)
	res, err := conn.Search(req)
	if err != nil || len(res.Entries) != 1 {
		return nil, ErrUnauthorized
	}
	entry := res.Entries[0]

	// Bind as the user to verify the password.
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, ErrUnauthorized
	}

	return s.provisionLDAPAccount(username, entry.GetAttributeValue(cfg.EmailAttr), entry.GetAttributeValue(cfg.NameAttr))
}

// provisionLDAPAccount links or creates the local account for a directory user.
func (s *Service) provisionLDAPAccount(username, email, fullName string) (*store.Account, error) {
	extID := ldapExternalPrefix + username
	if acct, err := s.db.GetAccountByExternalID(extID); err == nil {
		if fullName != "" || email != "" {
			s.db.UpdateAccountProfile(acct.ID, orDefaultStr(fullName, acct.FullName), orDefaultStr(email, acct.Email))
			acct.FullName, acct.Email = orDefaultStr(fullName, acct.FullName), orDefaultStr(email, acct.Email)
		}
		return acct, nil
	}
	// A local account with the same username predates LDAP: adopt it.
	if acct, err := s.db.GetAccountByUsername(username); err == nil {
		s.db.SetExternalID(acct.ID, extID)
		return acct, nil
	}
	if fullName == "" {
		fullName = username
	}
	// Random unusable password: directory users never authenticate locally.
	randomPassword := randomToken(16)
	hash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("failed to provision account")
	}
	a := &store.Account{Username: username, PasswordHash: string(hash), FullName: fullName, Email: email}
	if err := s.db.CreateAccount(a); err != nil {
		return nil, ErrUnauthorized
	}
	s.db.SetExternalID(a.ID, extID)
	s.addToGroup(a.ID, "Registered Users")
	return a, nil
}

func orDefaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
