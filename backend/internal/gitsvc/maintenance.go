package gitsvc

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// GC runs `git gc --auto` on a project repository to pack loose objects and
// prune unreachable ones. It returns git's combined output.
func (s *Service) GC(project string) (string, error) {
	if !s.ProjectExists(project) {
		return "", ErrProjectMissing
	}
	out, err := s.runGit(s.RepoDir(project), "gc", "--auto", "--quiet")
	if err != nil {
		return "", err
	}
	return out, nil
}

// Fsck runs `git fsck` on a project repository to verify object integrity and
// reference consistency, returning the report lines (empty when healthy).
// fsck reports problems on stdout and exits non-zero when it finds any, so the
// output is captured directly regardless of exit code.
func (s *Service) Fsck(project string) ([]string, error) {
	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	cmd := exec.Command("git", "fsck", "--no-dangling")
	cmd.Dir = s.RepoDir(project)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run()
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}
