// Package webhook fans out change/project events to registered HTTP endpoints.
// Delivery is asynchronous so a slow or unreachable receiver never blocks the
// request that triggered the event. When a webhook has a secret, the JSON body
// is signed with HMAC-SHA256 and the digest is sent in a header so receivers
// can verify authenticity.
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/store"
)

// Dispatcher delivers events to matching webhooks. A nil *Dispatcher is a
// no-op so callers need not guard.
type Dispatcher struct {
	db     *store.DB
	client *http.Client
	// onSent and onFail are optional counters wired to the metrics registry.
	onSent func()
	onFail func()
}

// New builds a Dispatcher with the given per-request timeout.
func New(db *store.DB, timeout time.Duration) *Dispatcher {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Dispatcher{
		db:     db,
		client: &http.Client{Timeout: timeout},
	}
}

// SetCounters registers callbacks invoked once per successful and failed
// delivery. Used by the metrics package.
func (d *Dispatcher) SetCounters(onSent, onFail func()) {
	if d == nil {
		return
	}
	d.onSent, d.onFail = onSent, onFail
}

// Dispatch delivers eventType for a project asynchronously. payload is
// marshalled once and POSTed to every active webhook that matches the project
// and subscribes to the event (a "*" subscription matches everything).
func (d *Dispatcher) Dispatch(eventType, project string, payload any) {
	if d == nil || d.db == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("webhook: marshal %s payload: %v", eventType, err)
		return
	}
	hooks, err := d.db.ListActiveWebhooks(project)
	if err != nil {
		log.Printf("webhook: list hooks for %q: %v", project, err)
		return
	}
	var targets []*store.Webhook
	for _, h := range hooks {
		if matches(h.Events, eventType) {
			targets = append(targets, h)
		}
	}
	if len(targets) == 0 {
		return
	}
	go func() {
		for _, h := range targets {
			d.deliver(h, eventType, body)
		}
	}()
}

func matches(events []string, eventType string) bool {
	for _, e := range events {
		if e == "*" || strings.EqualFold(e, eventType) {
			return true
		}
	}
	return false
}

func (d *Dispatcher) deliver(h *store.Webhook, eventType string, body []byte) {
	req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		d.countFail()
		log.Printf("webhook: build request for %q: %v", h.URL, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gerrit-go-webhook")
	req.Header.Set("X-GerritGo-Event", eventType)
	req.Header.Set("X-GerritGo-Delivery", strconv.FormatInt(time.Now().UnixNano(), 16))
	if h.Secret != "" {
		mac := hmac.New(sha256.New, []byte(h.Secret))
		mac.Write(body)
		req.Header.Set("X-GerritGo-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.countFail()
		log.Printf("webhook: deliver to %q: %v", h.URL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.countSent()
		return
	}
	d.countFail()
	log.Printf("webhook: deliver to %q: unexpected status %s", h.URL, fmt.Sprint(resp.Status))
}

func (d *Dispatcher) countSent() {
	if d.onSent != nil {
		d.onSent()
	}
}

func (d *Dispatcher) countFail() {
	if d.onFail != nil {
		d.onFail()
	}
}
