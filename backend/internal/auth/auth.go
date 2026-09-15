package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"gerrit-go/internal/store"

	"golang.org/x/crypto/bcrypt"
)

const CookieName = "GerritGoSession"
const sessionTTL = 30 * 24 * time.Hour

var ErrUnauthorized = errors.New("unauthorized")

type Service struct {
	db *store.DB
}

func New(db *store.DB) *Service { return &Service{db: db} }

func (s *Service) BootstrapAdmin() bool {
	if _, err := s.db.GetAccountByUsername("admin"); err == nil {
		return false
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	a := &store.Account{
		Username:     "admin",
		PasswordHash: string(hash),
		FullName:     "Administrator",
		Email:        "admin@localhost",
		Admin:        true,
	}
	s.db.CreateAccount(a)
	s.addToGroup(a.ID, "Administrators")
	return true
}

// addToGroup places an account in a built-in group, ignoring errors (the
// group always exists after migration seeding).
func (s *Service) addToGroup(accountID int64, groupName string) {
	if g, err := s.db.GetGroupByName(groupName); err == nil {
		s.db.AddGroupMember(g.ID, accountID)
	}
}

func (s *Service) Register(username, password, fullName, email string) (*store.Account, error) {
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}
	if _, err := s.db.GetAccountByUsername(username); err == nil {
		return nil, errors.New("username already exists")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	a := &store.Account{Username: username, PasswordHash: string(hash), FullName: fullName, Email: email}
	if a.FullName == "" {
		a.FullName = username
	}
	if err := s.db.CreateAccount(a); err != nil {
		return nil, err
	}
	s.addToGroup(a.ID, "Registered Users")
	return a, nil
}

func (s *Service) Authenticate(username, password string) (*store.Account, error) {
	a, err := s.db.GetAccountByUsername(username)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) != nil {
		return nil, ErrUnauthorized
	}
	return a, nil
}

func (s *Service) CreateSession(accountID int64) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	err := s.db.CreateSession(&store.Session{ID: id, AccountID: accountID, Expires: time.Now().Add(sessionTTL)})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) SetSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

// CurrentAccount resolves the caller from session cookie or HTTP basic auth
// (basic auth doubles as Gerrit's "HTTP password" for git and /a/ endpoints).
func (s *Service) CurrentAccount(r *http.Request) (*store.Account, error) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		sess, err := s.db.GetSession(c.Value)
		if err == nil && sess.Expires.After(time.Now()) {
			return s.db.GetAccount(sess.AccountID)
		}
	}
	if user, pass, ok := r.BasicAuth(); ok && user != "" {
		return s.Authenticate(user, pass)
	}
	return nil, ErrUnauthorized
}

func (s *Service) Logout(r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		s.db.DeleteSession(c.Value)
	}
}
