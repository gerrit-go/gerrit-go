// Package metrics exposes lightweight Prometheus-style counters and gauges for
// the server. Counters are in-process atomics; gauges are computed on demand
// from the database when the /metrics endpoint is scraped.
package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"gerrit-go/internal/store"
)

// Registry holds the process-wide counters. The zero value is not usable;
// construct one with New.
type Registry struct {
	httpRequests atomic.Uint64
	webhookSent  atomic.Uint64
	webhookFail  atomic.Uint64
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

func (r *Registry) IncHTTP()        { r.httpRequests.Add(1) }
func (r *Registry) IncWebhookSent() { r.webhookSent.Add(1) }
func (r *Registry) IncWebhookFail() { r.webhookFail.Add(1) }

// Handler renders the registry in the Prometheus text exposition format. When
// db is non-nil, database-derived gauges are included.
func (r *Registry) Handler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		counter(w, "gerrit_go_http_requests_total", "Total HTTP requests served.", r.httpRequests.Load())
		counter(w, "gerrit_go_webhook_deliveries_total", "Total webhook deliveries that succeeded.", r.webhookSent.Load())
		counter(w, "gerrit_go_webhook_failures_total", "Total webhook deliveries that failed.", r.webhookFail.Load())
		if db == nil {
			return
		}
		if accounts, projects, err := db.CountTables(); err == nil {
			gauge(w, "gerrit_go_accounts", "Number of registered accounts.", uint64(accounts))
			gauge(w, "gerrit_go_projects", "Number of projects.", uint64(projects))
		}
		if open, merged, abandoned, err := db.CountChangesByStatus(); err == nil {
			gauge(w, "gerrit_go_changes_open", "Number of open changes.", uint64(open))
			gauge(w, "gerrit_go_changes_merged", "Number of merged changes.", uint64(merged))
			gauge(w, "gerrit_go_changes_abandoned", "Number of abandoned changes.", uint64(abandoned))
		}
	}
}

func counter(w http.ResponseWriter, name, help string, v uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
}

func gauge(w http.ResponseWriter, name, help string, v uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, v)
}
