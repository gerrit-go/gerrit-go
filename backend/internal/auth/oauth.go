package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gerrit-go/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// OAuthConfig describes a generic OAuth2 / OpenID Connect authorization-code
// provider. It is provider-agnostic: point the URLs at any compliant IdP
// (Google, GitHub, Keycloak, GitLab, …). Login is disabled unless every
// required field is set.
type OAuthConfig struct {
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
	// Domain, when set, restricts login to verified emails under this domain.
	Domain string
	// Enabled is computed by normalize(); callers need not set it.
	Enabled bool
}

func (c OAuthConfig) normalize() OAuthConfig {
	c.Scopes = strings.TrimSpace(c.Scopes)
	if c.Scopes == "" {
		c.Scopes = "openid email profile"
	}
	c.Enabled = c.AuthURL != "" && c.TokenURL != "" && c.UserInfoURL != "" &&
		c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
	return c
}

// AuthCodeURL builds the provider authorization URL for the given state.
func (c OAuthConfig) AuthCodeURL(state string) string {
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.RedirectURL},
		"scope":         {c.Scopes},
		"state":         {state},
	}
	return c.AuthURL + "?" + params.Encode()
}

// OAuthUser is the normalized identity extracted from the provider's userinfo.
type OAuthUser struct {
	ExternalID string
	Email      string
	Name       string
	Username   string
}

var oauthClient = &http.Client{Timeout: 15 * time.Second}

// Exchange trades an authorization code for the authenticated user's identity.
func (s *Service) Exchange(ctx context.Context, code string) (*OAuthUser, error) {
	cfg := s.oauth
	if !cfg.Enabled {
		return nil, errors.New("oauth login is not configured")
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURL},
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := oauthClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, errors.New("provider returned no access_token")
	}

	uiReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	uiReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uiReq.Header.Set("Accept", "application/json")
	uiResp, err := oauthClient.Do(uiReq)
	if err != nil {
		return nil, err
	}
	defer uiResp.Body.Close()
	uiBody, _ := io.ReadAll(io.LimitReader(uiResp.Body, 1<<20))
	if uiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed: %s", uiResp.Status)
	}
	var claims map[string]any
	if err := json.Unmarshal(uiBody, &claims); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	return parseOAuthUser(claims), nil
}

func parseOAuthUser(claims map[string]any) *OAuthUser {
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := claims[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	u := &OAuthUser{
		ExternalID: str("sub", "id", "user_id", "uid"),
		Email:      strings.ToLower(str("email", "mail")),
		Name:       str("name", "full_name", "displayName"),
		Username:   str("preferred_username", "login", "username", "nickname"),
	}
	return u
}

// ProvisionOAuthUser maps an external identity onto a local account: by linked
// external_id first, then by matching email, otherwise a new account is created.
func (s *Service) ProvisionOAuthUser(u *OAuthUser) (*store.Account, error) {
	if u == nil || u.ExternalID == "" {
		return nil, errors.New("provider did not supply a subject identifier")
	}
	if s.oauth.Domain != "" && u.Email != "" {
		if !strings.HasSuffix(u.Email, "@"+strings.ToLower(s.oauth.Domain)) {
			return nil, fmt.Errorf("email %q is not in the allowed domain %q", u.Email, s.oauth.Domain)
		}
	}
	if a, err := s.db.GetAccountByExternalID(u.ExternalID); err == nil {
		return a, nil
	}
	if u.Email != "" {
		if a, err := s.db.GetAccountByEmail(u.Email); err == nil {
			_ = s.db.SetExternalID(a.ID, u.ExternalID)
			return a, nil
		}
	}

	username := u.Username
	if username == "" {
		username = deriveUsername(u.Email, u.ExternalID)
	}
	base := username
	for i := 2; ; i++ {
		if _, err := s.db.GetAccountByUsername(username); err != nil {
			break
		}
		username = fmt.Sprintf("%s%d", base, i)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(randomToken(16)), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	name := u.Name
	if name == "" {
		name = username
	}
	a := &store.Account{
		Username:     username,
		PasswordHash: string(hash),
		FullName:     name,
		Email:        u.Email,
	}
	if err := s.db.CreateAccount(a); err != nil {
		return nil, err
	}
	_ = s.db.SetExternalID(a.ID, u.ExternalID)
	s.addToGroup(a.ID, "Registered Users")
	return a, nil
}

func deriveUsername(email, externalID string) string {
	if email != "" {
		if local, _, ok := strings.Cut(email, "@"); ok && local != "" {
			return sanitizeUsername(local)
		}
	}
	return sanitizeUsername(externalID)
}

func sanitizeUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_.-")
	if out == "" {
		out = "user"
	}
	return out
}
