package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/store"
)

const oauthStateCookie = "GerritGoOAuthState"

// ---------- self profile ----------

func (s *Server) handleUpdateSelf(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = acct.FullName
	}
	email := strings.TrimSpace(req.Email)
	if err := s.db.UpdateAccountProfile(acct.ID, name, email); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	updated, err := s.db.GetAccount(acct.ID)
	if err != nil {
		updated = acct
	}
	writeJSON(w, http.StatusOK, accountInfo(updated))
}

// ---------- HTTP password ----------

func (s *Server) handleGenerateHTTPPassword(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	plain, err := s.auth.GenerateHTTPPassword(acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate HTTP password")
		return
	}
	// Returned once in plaintext; only the bcrypt hash is persisted.
	writeJSON(w, http.StatusOK, map[string]any{"http_password": plain})
}

func (s *Server) handleClearHTTPPassword(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if err := s.auth.ClearHTTPPassword(acct.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to clear HTTP password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHTTPPasswordStatus(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	hash, _ := s.db.GetHTTPPasswordHash(acct.ID)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": hash != ""})
}

// ---------- SSH keys ----------

func sshKeyInfo(k *store.SSHKey) map[string]any {
	return map[string]any{
		"id":         k.ID,
		"public_key": k.PublicKey,
		"comment":    k.Comment,
		"created":    k.Created.Format(time.RFC3339),
	}
}

func (s *Server) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	keys, err := s.db.ListSSHKeys(acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshKeyInfo(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddSSHKey(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	key, comment := parsePublicKey(strings.TrimSpace(req.Key))
	if key == "" {
		writeErr(w, http.StatusBadRequest, "a valid SSH public key is required")
		return
	}
	k := &store.SSHKey{AccountID: acct.ID, PublicKey: key, Comment: comment}
	if err := s.db.CreateSSHKey(k); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store key")
		return
	}
	writeJSON(w, http.StatusCreated, sshKeyInfo(k))
}

func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid key id")
		return
	}
	if err := s.db.DeleteSSHKey(acct.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to delete key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parsePublicKey normalizes an "algo base64 [comment]" public key line. It
// returns the key (algo + base64) and the trailing comment, or "" when the
// input does not look like a supported public key.
func parsePublicKey(s string) (key, comment string) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return "", ""
	}
	algo := fields[0]
	switch {
	case strings.HasPrefix(algo, "ssh-"),
		strings.HasPrefix(algo, "ecdsa-"),
		strings.HasPrefix(algo, "sk-"):
	default:
		return "", ""
	}
	key = algo + " " + fields[1]
	if len(fields) > 2 {
		comment = strings.Join(fields[2:], " ")
	}
	return key, comment
}

// ---------- saved queries ----------

func (s *Server) handleListSavedQueries(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	list, err := s.db.ListSavedQueries(acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateSavedQuery(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	var req struct {
		Name   string `json:"name"`
		Query  string `json:"query"`
		Shared bool   `json:"shared"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Query) == "" {
		writeErr(w, http.StatusBadRequest, "name and query are required")
		return
	}
	q, err := s.db.CreateSavedQuery(acct.ID, strings.TrimSpace(req.Name), strings.TrimSpace(req.Query), req.Shared)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

func (s *Server) handleDeleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.DeleteSavedQuery(acct.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- public config + OAuth ----------

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"auth": map[string]any{"oauth": s.auth.OAuthEnabled(), "ldap": s.auth.LDAPEnabled(), "register": s.allowRegister},
		"ssh":  map[string]any{"port": strings.TrimPrefix(s.sshAddr, ":")},
	})
}

func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.OAuthEnabled() {
		http.Error(w, "oauth login is not configured", http.StatusNotFound)
		return
	}
	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, s.auth.OAuthAuthCodeURL(state), http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(reason string) {
		http.Redirect(w, r, "/login?error="+reason, http.StatusFound)
	}
	if !s.auth.OAuthEnabled() {
		fail("oauth_disabled")
		return
	}
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(oauthStateCookie)
	if err != nil || c.Value == "" || state == "" || state != c.Value {
		fail("bad_state")
		return
	}
	// Clear the one-time state cookie.
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: "", Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		fail("missing_code")
		return
	}
	user, err := s.auth.Exchange(r.Context(), code)
	if err != nil {
		fail("exchange_failed")
		return
	}
	acct, err := s.auth.ProvisionOAuthUser(user)
	if err != nil {
		fail("provision_failed")
		return
	}
	sid, err := s.auth.CreateSession(acct.ID)
	if err != nil {
		fail("session_failed")
		return
	}
	s.auth.SetSessionCookie(w, sid)
	http.Redirect(w, r, "/", http.StatusFound)
}

func randomState() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}
