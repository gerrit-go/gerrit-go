// Package notify fans out change events to interested accounts as in-app
// notifications and, when SMTP is configured, as email.
package notify

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"

	"gerrit-go/internal/i18n"
	"gerrit-go/internal/store"
)

// SMTPConfig holds the outgoing mail settings. When Host is empty, email
// delivery is disabled and only in-app notifications are produced.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (c SMTPConfig) enabled() bool { return strings.TrimSpace(c.Host) != "" }

// Event describes something that happened to a change. The Notifier derives the
// recipient set from the change's owner, reviewers and project watchers.
type Event struct {
	Type         string // created, comment, review, submitted, abandoned, restored, reviewer-added
	ChangeNumber int64
	Project      string
	Branch       string
	Subject      string
	OwnerID      int64
	ActorID      int64
	Message      string
	// Lang is the request language used to localize the email body. Empty means
	// English.
	Lang string
	// NotifyOwner includes the change owner in the recipient set.
	NotifyOwner bool
	// IncludeReviewers adds every current reviewer of the change.
	IncludeReviewers bool
	// ExtraRecipients are account IDs always included (e.g. a newly added reviewer).
	ExtraRecipients []int64
}

// Notifier delivers events. A nil *Notifier is a no-op so callers need not guard.
type Notifier struct {
	db      *store.DB
	smtp    SMTPConfig
	baseURL string
}

// New builds a Notifier. baseURL is used to build change links in emails.
func New(db *store.DB, cfg SMTPConfig, baseURL string) *Notifier {
	return &Notifier{db: db, smtp: cfg, baseURL: strings.TrimRight(baseURL, "/")}
}

// Notify delivers ev to its recipients. In-app notifications are written
// synchronously; email is dispatched in the background so a slow or unreachable
// SMTP server never blocks the request.
func (n *Notifier) Notify(ev Event) {
	if n == nil || n.db == nil {
		return
	}
	recipients := n.recipients(ev)
	if len(recipients) == 0 {
		return
	}

	var emails []string
	for id := range recipients {
		if err := n.db.CreateNotification(&store.Notification{
			AccountID:    id,
			ChangeNumber: ev.ChangeNumber,
			Type:         ev.Type,
			Message:      ev.Message,
			ActorID:      ev.ActorID,
		}); err != nil {
			log.Printf("notify: create notification for account %d: %v", id, err)
			continue
		}
		if n.smtp.enabled() {
			if acct, err := n.db.GetAccount(id); err == nil && strings.TrimSpace(acct.Email) != "" {
				emails = append(emails, acct.Email)
			}
		}
	}

	if len(emails) > 0 {
		subject, body := n.renderEmail(ev)
		go n.sendEmails(emails, subject, body)
	}
}

// recipients computes the de-duplicated account IDs that should hear about ev,
// always excluding the actor who triggered it.
func (n *Notifier) recipients(ev Event) map[int64]bool {
	set := map[int64]bool{}
	add := func(id int64) {
		if id > 0 {
			set[id] = true
		}
	}
	if ev.NotifyOwner {
		add(ev.OwnerID)
	}
	for _, id := range ev.ExtraRecipients {
		add(id)
	}
	if ev.IncludeReviewers {
		if reviewers, err := n.db.ListReviewers(ev.ChangeNumber); err == nil {
			for _, rv := range reviewers {
				add(rv.AccountID)
			}
		}
	}
	actorUsername := ""
	if actor, err := n.db.GetAccount(ev.ActorID); err == nil {
		actorUsername = actor.Username
	}
	if watchers, err := n.db.ListProjectWatchers(ev.Project, ev.Branch, actorUsername); err == nil {
		for _, id := range watchers {
			add(id)
		}
	}
	delete(set, ev.ActorID)
	return set
}

func (n *Notifier) renderEmail(ev Event) (subject, body string) {
	subject = fmt.Sprintf("[%s] %s (%s)", ev.Project, ev.Subject, ev.Type)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", ev.Message)
	fmt.Fprintf(&b, "%s\n%s\n%s\n",
		i18n.T(ev.Lang, "email.project", ev.Project),
		i18n.T(ev.Lang, "email.branch", ev.Branch),
		i18n.T(ev.Lang, "email.change", ev.ChangeNumber))

	// Change context: owner, current patch set, vote summary, comment count.
	if c, err := n.db.GetChange(ev.ChangeNumber); err == nil {
		if owner, err := n.db.GetAccount(c.OwnerID); err == nil {
			name := owner.FullName
			if name == "" {
				name = owner.Username
			}
			fmt.Fprintf(&b, "%s\n", i18n.T(ev.Lang, "email.owner", name))
		}
		fmt.Fprintf(&b, "%s\n", i18n.T(ev.Lang, "email.patchSet", c.CurrentPS))
		if votes, err := n.db.ListVotes(ev.ChangeNumber); err == nil {
			if summary := summarizeVotes(votes, c.CurrentPS); summary != "" {
				fmt.Fprintf(&b, "%s\n", i18n.T(ev.Lang, "email.votes", summary))
			}
		}
		if comments, err := n.db.ListComments(ev.ChangeNumber); err == nil && len(comments) > 0 {
			fmt.Fprintf(&b, "%s\n", i18n.T(ev.Lang, "email.comments", len(comments)))
		}
	}
	if n.baseURL != "" {
		fmt.Fprintf(&b, "\n%s/c/%d\n", n.baseURL, ev.ChangeNumber)
	}
	return subject, b.String()
}

// summarizeVotes renders the net vote per label on the given patch set, e.g.
// "Code-Review+2, Verified+1". Returns "" when there are no votes.
func summarizeVotes(votes []*store.VoteInfo, ps int) string {
	net := map[string]int{}
	var order []string
	for _, v := range votes {
		if v.PatchSet != ps {
			continue
		}
		if _, seen := net[v.Label]; !seen {
			order = append(order, v.Label)
		}
		net[v.Label] += v.Value
	}
	var parts []string
	for _, label := range order {
		if net[label] == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s%+d", label, net[label]))
	}
	return strings.Join(parts, ", ")
}

func (n *Notifier) sendEmails(to []string, subject, body string) {
	addr := fmt.Sprintf("%s:%d", n.smtp.Host, n.smtp.Port)
	from := n.smtp.From
	if from == "" {
		from = "gerrit-go@localhost"
	}
	var auth smtp.Auth
	if n.smtp.Username != "" {
		auth = smtp.PlainAuth("", n.smtp.Username, n.smtp.Password, n.smtp.Host)
	}
	msg := []byte(strings.Join([]string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n"))
	if err := smtp.SendMail(addr, auth, from, to, msg); err != nil {
		log.Printf("notify: send email to %v: %v", to, err)
	}
}
