package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	flag.Parse()

	if err := os.MkdirAll(filepath.Join(*dataDir, "git"), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	db, err := store.Open(filepath.Join(*dataDir, "gerrit.db"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()

	authSvc := auth.New(db)
	if created := authSvc.BootstrapAdmin(); created {
		log.Printf("created initial admin account: username=admin password=secret (change it after first login)")
	}

	redirectURL := *oauthRedirectURL
	if redirectURL == "" && *webURL != "" {
		redirectURL = strings.TrimSuffix(*webURL, "/") + "/oauth/callback"
	}
	authSvc.ConfigureOAuth(auth.OAuthConfig{
		AuthURL:      *oauthAuthURL,
		TokenURL:     *oauthTokenURL,
		UserInfoURL:  *oauthUserInfoURL,
		ClientID:     *oauthClientID,
		ClientSecret: *oauthClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       *oauthScopes,
		Domain:       *oauthDomain,
	})
	if authSvc.OAuthEnabled() {
		log.Printf("OAuth2/OIDC single sign-on enabled (redirect: %s)", redirectURL)
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
		log.Printf("LDAP/AD authentication enabled (url: %s, base: %s)", *ldapURL, *ldapBaseDN)
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
		log.Printf("email notifications disabled (set -smtp-host to enable); in-app notifications active")
	}
	srv := api.NewServer(db, authSvc, gitSvc, notifier, *staticDir)
	handler := srv.Handler()

	if *sshAddr != "" {
		go func() {
			if err := srv.StartSSH(*sshAddr); err != nil {
				log.Printf("ssh listener stopped: %v", err)
			}
		}()
	}

	log.Printf("gerrit-go listening on %s (data dir: %s)", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}
