package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gerrit-go/internal/api"
	"gerrit-go/internal/auth"
	"gerrit-go/internal/gitsvc"
	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	sshAddr := flag.String("ssh-addr", ":29418", "git+ssh listen address (empty disables SSH)")
	dataDir := flag.String("data", "data", "data directory (db + git repos)")
	dbDSN := flag.String("db", "", "database DSN: postgres(ql)://... for PostgreSQL, otherwise a SQLite file path (empty => <data>/gerrit.db)")
	staticDir := flag.String("static", "", "directory of built frontend assets (optional)")
	webURL := flag.String("web-url", "", "canonical web URL used in notification emails (optional)")
	smtpHost := flag.String("smtp-host", "", "SMTP host for email notifications (empty disables email)")
	smtpPort := flag.Int("smtp-port", 587, "SMTP port")
	smtpUser := flag.String("smtp-user", "", "SMTP username")
	smtpPass := flag.String("smtp-pass", "", "SMTP password")
	smtpFrom := flag.String("smtp-from", "", "From address for notification emails")
	oauthAuthURL := flag.String("oauth-auth-url", "", "OAuth2/OIDC authorization endpoint (empty disables SSO)")
	oauthTokenURL := flag.String("oauth-token-url", "", "OAuth2/OIDC token endpoint")
	oauthUserInfoURL := flag.String("oauth-userinfo-url", "", "OAuth2/OIDC userinfo endpoint")
	oauthClientID := flag.String("oauth-client-id", "", "OAuth2/OIDC client ID")
	oauthClientSecret := flag.String("oauth-client-secret", "", "OAuth2/OIDC client secret")
	oauthRedirectURL := flag.String("oauth-redirect-url", "", "OAuth2/OIDC redirect URL (default: <web-url>/oauth/callback)")
	oauthScopes := flag.String("oauth-scopes", "openid email profile", "OAuth2/OIDC scopes")
	oauthDomain := flag.String("oauth-domain", "", "restrict SSO login to this email domain (optional)")
	ldapURL := flag.String("ldap-url", "", "LDAP/AD URL, e.g. ldap://host:389 or ldaps://host:636 (empty disables LDAP)")
	ldapBindDN := flag.String("ldap-bind-dn", "", "service account DN for the user search (empty => anonymous search)")
	ldapBindPass := flag.String("ldap-bind-pass", "", "service account password")
	ldapBaseDN := flag.String("ldap-base-dn", "", "search base DN, e.g. ou=people,dc=example,dc=com")
	ldapUserFilter := flag.String("ldap-user-filter", "(uid=%s)", "user search filter; %s is replaced by the username")
	ldapAttr := flag.String("ldap-attr", "uid", "username attribute (AD: sAMAccountName)")
	ldapEmailAttr := flag.String("ldap-email-attr", "mail", "email attribute")
	ldapNameAttr := flag.String("ldap-name-attr", "cn", "display-name attribute")
	ldapInsecure := flag.Bool("ldap-insecure", false, "skip LDAP TLS certificate verification (ldaps)")
	allowRegistration := flag.Bool("allow-registration", false, "allow open self-registration via /register (default off; admins can always create accounts)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := os.MkdirAll(filepath.Join(*dataDir, "git"), 0o755); err != nil {
		slog.Error("create data dir", "err", err)
		os.Exit(1)
	}

	dsn := *dbDSN
	if dsn == "" {
		dsn = os.Getenv("GERRIT_GO_DB")
	}
	if dsn == "" {
		dsn = filepath.Join(*dataDir, "gerrit.db")
	}
	if store.IsPostgresDSN(dsn) {
		slog.Info("using PostgreSQL backend")
	} else {
		slog.Info("using SQLite backend", "dsn", dsn)
	}
	db, err := store.Open(dsn)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	authSvc := auth.New(db)
	if pw, created := authSvc.BootstrapAdmin(); created {
		slog.Info("created initial admin account", "username", "admin", "password", pw)
	}

	envOr := func(v, key string) string {
		if v != "" {
			return v
		}
		return os.Getenv(key)
	}
	redirectURL := envOr(*oauthRedirectURL, "GERRIT_GO_OAUTH_REDIRECT_URL")
	if redirectURL == "" && *webURL != "" {
		redirectURL = strings.TrimSuffix(*webURL, "/") + "/oauth/callback"
	}
	authSvc.ConfigureOAuth(auth.OAuthConfig{
		AuthURL:      envOr(*oauthAuthURL, "GERRIT_GO_OAUTH_AUTH_URL"),
		TokenURL:     envOr(*oauthTokenURL, "GERRIT_GO_OAUTH_TOKEN_URL"),
		UserInfoURL:  envOr(*oauthUserInfoURL, "GERRIT_GO_OAUTH_USERINFO_URL"),
		ClientID:     envOr(*oauthClientID, "GERRIT_GO_OAUTH_CLIENT_ID"),
		ClientSecret: envOr(*oauthClientSecret, "GERRIT_GO_OAUTH_CLIENT_SECRET"),
		RedirectURL:  redirectURL,
		Scopes:       *oauthScopes,
		Domain:       envOr(*oauthDomain, "GERRIT_GO_OAUTH_DOMAIN"),
	})
	if authSvc.OAuthEnabled() {
		slog.Info("OAuth2/OIDC single sign-on enabled", "redirect", redirectURL)
	}

	authSvc.ConfigureLDAP(auth.LDAPConfig{
		URL:        *ldapURL,
		BindDN:     *ldapBindDN,
		BindPass:   *ldapBindPass,
		BaseDN:     *ldapBaseDN,
		UserFilter: *ldapUserFilter,
		Attr:       *ldapAttr,
		EmailAttr:  *ldapEmailAttr,
		NameAttr:   *ldapNameAttr,
		Insecure:   *ldapInsecure,
	})
	if authSvc.LDAPEnabled() {
		slog.Info("LDAP/AD authentication enabled", "url", *ldapURL, "base", *ldapBaseDN)
	}

	gitSvc := gitsvc.New(filepath.Join(*dataDir, "git"), db)
	notifier := notify.New(db, notify.SMTPConfig{
		Host:     *smtpHost,
		Port:     *smtpPort,
		Username: *smtpUser,
		Password: *smtpPass,
		From:     *smtpFrom,
	}, *webURL)
	if *smtpHost == "" {
		slog.Info("email notifications disabled; in-app notifications active")
	}
	srv := api.NewServer(db, authSvc, gitSvc, notifier, *staticDir, *allowRegistration)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *sshAddr != "" {
		go func() {
			if err := srv.StartSSH(*sshAddr); err != nil {
				slog.Info("ssh listener stopped", "err", err)
			}
		}()
	}

	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP shutdown", "err", err)
		}
		srv.CloseSSH()
	}()

	slog.Info("gerrit-go listening", "addr", *addr, "data", *dataDir)
	if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("HTTP server", "err", err)
		os.Exit(1)
	}
}
