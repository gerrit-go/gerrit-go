package gitsvc

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
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
	lines := []string{}
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// BackfillPatchSetFiles populates patchset_files for patch sets that predate
// the file: query operator (e.g. changes migrated from the original Gerrit),
// so file: queries work for them. It returns the number of patch sets
// backfilled. Patch sets that already have rows are skipped.
func (s *Service) BackfillPatchSetFiles() (int, error) {
	all, err := s.db.ListAllPatchSets()
	if err != nil {
		return 0, err
	}
	filled := 0
	for _, ps := range all {
		existing, err := s.db.ListPatchSetFiles(ps.ChangeNumber, ps.Number)
		if err == nil && len(existing) > 0 {
			continue
		}
		change, err := s.db.GetChange(ps.ChangeNumber)
		if err != nil {
			continue
		}
		repo, err := s.OpenRepo(change.Project)
		if err != nil {
			continue
		}
		commit, err := repo.CommitObject(plumbing.NewHash(ps.CommitSHA))
		if err != nil {
			continue
		}
		patch, err := patchAgainstParent(commit)
		if err != nil {
			continue
		}
		diffs := patchToFileDiffs(patch)
		paths := make([]string, 0, len(diffs))
		for _, fd := range diffs {
			paths = append(paths, fd.Path)
		}
		if err := s.db.AddPatchSetFiles(ps.ChangeNumber, ps.Number, paths); err == nil {
			filled++
		}
	}
	return filled, nil
}
