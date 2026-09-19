package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gerrit-go/internal/store"
)

// FailDirective marks a simulated step as failing, so the whole green/red
// pipeline path (check run + vote + submit gate) can be exercised without running
// any repository code.
const FailDirective = "#ci-fail"

// SimulatedRunner never executes user code: it replays the configured steps into
// a log and decides success/failure from FailDirective. It is the default runner
// and the safe choice for shared or internet-facing instances.
type SimulatedRunner struct {
	// Delay is an optional artificial build duration (zero = instant).
	Delay time.Duration
}

func (SimulatedRunner) Name() string { return "simulated" }

func (r SimulatedRunner) Run(_ context.Context, job Job) Outcome {
	started := time.Now().UTC().Format(time.RFC3339Nano)
	var log strings.Builder
	cfg := job.Config

	fmt.Fprintf(&log, "simulated pipeline %q for %s (change %d, patch set %d)\n",
		cfg.Name, job.Project, job.ChangeNumber, job.PatchSet)

	keys := make([]string, 0, len(cfg.Env))
	for k := range cfg.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		fmt.Fprintf(&log, "env: %s\n", strings.Join(keys, ", "))
	}

	failed := false
	for i, step := range cfg.Steps {
		if r.Delay > 0 {
			time.Sleep(r.Delay)
		}
		fmt.Fprintf(&log, "[%d/%d] $ %s\n", i+1, len(cfg.Steps), step)
		if strings.Contains(step, FailDirective) {
			failed = true
			fmt.Fprintf(&log, "[%d/%d] step failed (exit status 1)\n", i+1, len(cfg.Steps))
			break
		}
	}
	if len(cfg.Steps) == 0 {
		fmt.Fprintln(&log, "no steps configured — treating as success")
	}

	status := store.RunSuccess
	if failed {
		status = store.RunFailure
	}
	return Outcome{
		Status:   status,
		Log:      log.String(),
		Started:  started,
		Finished: time.Now().UTC().Format(time.RFC3339Nano),
	}
}
