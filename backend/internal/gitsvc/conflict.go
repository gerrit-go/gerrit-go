package gitsvc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gerrit-go/internal/store"

	"github.com/go-git/go-git/v5/plumbing"
)

// ConflictFile carries the three-way merge inputs for one path that conflicted
// during a rebase, plus the working-tree content with conflict markers. Ours is
// the destination-branch side, Theirs is the change being rebased.
type ConflictFile struct {
	Path     string `json:"path"`
	Base     string `json:"base"`
	Ours     string `json:"ours"`
	Theirs   string `json:"theirs"`
	Conflict string `json:"conflict"`
}

// beginRebase clones the project, replays the change onto the current branch
// tip and reports whether the rebase stopped on a conflict. On success the work
// tree is mid/post-rebase and the caller must finishRebase; on conflict the
// caller must resolve or abort. cleanup is always non-nil when err == nil.
func (s *Service) beginRebase(changeNumber int64) (change *store.Change, work string, cleanup func(), conflicted bool, err error) {
	change, err = s.db.GetChange(changeNumber)
	if err != nil {
		return nil, "", func() {}, false, err
	}
	if change.Status != "NEW" {
		return nil, "", func() {}, false, ErrNotSubmittable
	}
	ps, err := s.db.GetPatchSet(change.Number, change.CurrentPS)
	if err != nil {
		return nil, "", func() {}, false, err
	}
	tip := s.branchTip(change.Project, change.Branch)
	if tip.IsZero() {
		return nil, "", func() {}, false, errors.New("destination branch does not exist")
	}
	changeSHA := plumbing.NewHash(ps.CommitSHA)
	repo, err := s.OpenRepo(change.Project)
	if err != nil {
		return nil, "", func() {}, false, err
	}
	if tip == changeSHA {
		return nil, "", func() {}, false, errors.New("change is already up to date")
	}
	if ok, _ := isAncestor(repo, tip, changeSHA); ok {
		return nil, "", func() {}, false, errors.New("change is already up to date")
	}

	work, cleanup, err = s.cloneWork(change.Project)
	if err != nil {
		return nil, "", func() {}, false, err
	}
	fail := func(e error) (*store.Change, string, func(), bool, error) {
		cleanup()
		return nil, "", func() {}, false, e
	}
	if _, err := s.runGit(work, "fetch", "--quiet", "origin", s.changeRef(change.Number, ps.Number)); err != nil {
		return fail(err)
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "-B", change.Branch, tip.String()); err != nil {
		return fail(err)
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "-b", "gerrit-rebase", changeSHA.String()); err != nil {
		return fail(err)
	}
	if _, err := s.runGit(work, "rebase", change.Branch); err != nil {
		// Leave the work tree in the conflicted state for the caller.
		return change, work, cleanup, true, nil
	}
	return change, work, cleanup, false, nil
}

// finishRebase reads HEAD after a clean rebase and pushes it as the next patch
// set of the change.
func (s *Service) finishRebase(change *store.Change, work string) (NewCommit, error) {
	sha, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return NewCommit{}, err
	}
	nc, err := s.readCommit(work, sha)
	if err != nil {
		return NewCommit{}, err
	}
	newPS := change.CurrentPS + 1
	if _, err := s.runGit(work, "push", "--quiet", "origin", sha+":"+s.changeRef(change.Number, newPS)); err != nil {
		return NewCommit{}, err
	}
	nc.NewPatchSet = newPS
	return nc, nil
}

// unmergedPaths lists paths still in conflict in the work tree.
func (s *Service) unmergedPaths(work string) ([]string, error) {
	out, err := s.runGit(work, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// rebaseInProgress reports whether a rebase is still underway in the work tree.
func rebaseInProgress(work string) bool {
	gitDir := filepath.Join(work, ".git")
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if fi, err := os.Stat(filepath.Join(gitDir, d)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// stageContent returns a conflicted path's content at a merge stage (1=base,
// 2=ours, 3=theirs), or "" when that stage is absent (add/add, delete, etc.).
func (s *Service) stageContent(work, path string, stage int) string {
	out, err := s.runGitRaw(work, "show", fmt.Sprintf(":%d:%s", stage, path))
	if err != nil {
		return ""
	}
	return out
}

// RebaseConflicts performs a trial rebase and returns the conflicting files with
// their three-way content, or nil when the change rebases cleanly. The trial is
// always aborted; nothing is pushed.
func (s *Service) RebaseConflicts(changeNumber int64) ([]ConflictFile, error) {
	_, work, cleanup, conflicted, err := s.beginRebase(changeNumber)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if !conflicted {
		return nil, nil
	}
	files, cerr := s.collectConflicts(work)
	s.runGit(work, "rebase", "--abort")
	if cerr != nil {
		return nil, cerr
	}
	return files, nil
}

func (s *Service) collectConflicts(work string) ([]ConflictFile, error) {
	paths, err := s.unmergedPaths(work)
	if err != nil {
		return nil, err
	}
	files := make([]ConflictFile, 0, len(paths))
	for _, p := range paths {
		cf := ConflictFile{
			Path:   p,
			Base:   s.stageContent(work, p, 1),
			Ours:   s.stageContent(work, p, 2),
			Theirs: s.stageContent(work, p, 3),
		}
		if b, err := os.ReadFile(filepath.Join(work, p)); err == nil {
			cf.Conflict = string(b)
		}
		files = append(files, cf)
	}
	return files, nil
}

// ResolveRebase rebases the change and applies the caller-provided resolved
// content for each conflicting path, then pushes the result as the next patch
// set. Every conflicting path must have a resolution.
func (s *Service) ResolveRebase(changeNumber int64, resolutions map[string]string) (NewCommit, error) {
	change, work, cleanup, conflicted, err := s.beginRebase(changeNumber)
	if err != nil {
		return NewCommit{}, err
	}
	defer cleanup()

	if conflicted {
		for {
			paths, uerr := s.unmergedPaths(work)
			if uerr != nil {
				s.runGit(work, "rebase", "--abort")
				return NewCommit{}, uerr
			}
			for _, p := range paths {
				content, ok := resolutions[p]
				if !ok {
					s.runGit(work, "rebase", "--abort")
					return NewCommit{}, fmt.Errorf("no resolution provided for %s", p)
				}
				abs := filepath.Join(work, filepath.FromSlash(p))
				if !strings.HasPrefix(abs, filepath.Clean(work)+string(os.PathSeparator)) {
					s.runGit(work, "rebase", "--abort")
					return NewCommit{}, fmt.Errorf("invalid path %s", p)
				}
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					s.runGit(work, "rebase", "--abort")
					return NewCommit{}, err
				}
				if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
					s.runGit(work, "rebase", "--abort")
					return NewCommit{}, err
				}
				if _, err := s.runGit(work, "add", "--", p); err != nil {
					s.runGit(work, "rebase", "--abort")
					return NewCommit{}, err
				}
			}
			if _, err := s.runGit(work, "rebase", "--continue"); err != nil && !rebaseInProgress(work) {
				s.runGit(work, "rebase", "--abort")
				return NewCommit{}, ErrRebaseConflict
			}
			if !rebaseInProgress(work) {
				break
			}
		}
	}
	return s.finishRebase(change, work)
}
