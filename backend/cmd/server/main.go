package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"gerrit-go/internal/api"
	"gerrit-go/internal/auth"
	"gerrit-go/internal/gitsvc"
	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "data", "data directory (db + git repos)")
	staticDir := flag.String("static", "", "directory of built frontend assets (optional)")
	webURL := flag.String("web-url", "", "canonical web URL used in notification emails (optional)")
	smtpHost := flag.String("smtp-host", "", "SMTP host for email notifications (empty disables email)")
	smtpPort := flag.Int("smtp-port", 587, "SMTP port")
	smtpUser := flag.String("smtp-user", "", "SMTP username")
	smtpPass := flag.String("smtp-pass", "", "SMTP password")
	smtpFrom := flag.String("smtp-from", "", "From address for notification emails")
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
	handler := api.NewRouter(db, authSvc, gitSvc, notifier, *staticDir)

	log.Printf("gerrit-go listening on %s (data dir: %s)", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}
