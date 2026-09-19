package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gerrit-go/internal/store"
)

// ExecRunner runs pipeline steps as shell commands in a throwaway workspace.
//
// SECURITY: this executes arbitrary code from pipeline configuration, which is
// only as trustworthy as whoever can edit that configuration. It is therefore
// opt-in (enabled by an explicit server flag) and intended for trusted, private
// deployments. For untrusted repos use a sandboxed runner instead — the Runner
// interface is the seam for that.
type ExecRunner struct {
	// GitBin is the git executable; defaults to "git".
	GitBin string
	// StepTimeout bounds a single step; zero means only the caller's ctx applies.
	StepTimeout time.Duration
}

func (ExecRunner) Name() string { return "exec" }

func (r ExecRunner) Run(ctx context.Context, job Job) Outcome {
	git := r.GitBin
	if git == "" {
		git = "git"
	}
	cfg := job.Config
	started := time.Now().UTC().Format(time.RFC3339Nano)
	var log strings.Builder

	finish := func(status string) Outcome {
		return Outcome{
			Status:   status,
			Log:      log.String(),
			Started:  started,
			Finished: time.Now().UTC().Format(time.RFC3339Nano),
		}
	}

	dir, err := os.MkdirTemp("", "gerritgo-ci-*")
	if err != nil {
		fmt.Fprintf(&log, "workspace: %v\n", err)
		return finish(store.RunError)
	}
	defer os.RemoveAll(dir)
	workspace := filepath.Join(dir, "src")

	if job.RepoPath != "" {
		if out, err := r.command(ctx, dir, nil, git, "clone", "--quiet", job.RepoPath, "src"); err != nil {
			fmt.Fprintf(&log, "clone failed: %v\n%s\n", err, out)
			return finish(store.RunError)
		}
		if job.Ref != "" {
			if out, err := r.command(ctx, workspace, nil, git, "checkout", "--quiet", "--detach", job.Ref); err != nil {
				fmt.Fprintf(&log, "checkout %s failed: %v\n%s\n", job.Ref, err, out)
				return finish(store.RunError)
			}
		}
		fmt.Fprintf(&log, "workspace ready at %s\n\n", workspace)
	} else {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			fmt.Fprintf(&log, "workspace: %v\n", err)
			return finish(store.RunError)
		}
	}

	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	env = append(env,
		"GERRIT_CHANGE_NUMBER="+fmt.Sprint(job.ChangeNumber),
		"GERRIT_PATCHSET_NUMBER="+fmt.Sprint(job.PatchSet),
		"GERRIT_PROJECT="+job.Project,
	)

	if len(cfg.Steps) == 0 {
		fmt.Fprintln(&log, "no steps configured — treating as success")
		return finish(store.RunSuccess)
	}

	for i, step := range cfg.Steps {
		fmt.Fprintf(&log, "[%d/%d] $ %s\n", i+1, len(cfg.Steps), step)
		stepCtx := ctx
		var cancel context.CancelFunc
		if r.StepTimeout > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, r.StepTimeout)
		}
		out, err := r.command(stepCtx, workspace, env, "/bin/sh", "-c", step)
		if cancel != nil {
			cancel()
		}
		if out != "" {
			log.WriteString(strings.TrimRight(out, "\n"))
			log.WriteString("\n")
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				fmt.Fprintf(&log, "[%d/%d] timed out\n", i+1, len(cfg.Steps))
			} else {
				fmt.Fprintf(&log, "[%d/%d] failed: %v\n", i+1, len(cfg.Steps), err)
			}
			return finish(store.RunFailure)
		}
	}
	return finish(store.RunSuccess)
}

func (r ExecRunner) command(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
