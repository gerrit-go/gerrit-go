package gitsvc

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gerrit-go/internal/store"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

var (
	ErrMergeConflict      = errors.New("merge conflict; the change must be rebased or merged manually")
	ErrRebaseConflict     = errors.New("rebase conflict; the change must be rebased manually")
	ErrCherryPickConflict = errors.New("cherry-pick conflict; the change must be rebased manually")
)

// Submit merges a change into its destination branch using the project's
// configured submit strategy and returns the resulting commit SHA.
func (s *Service) Submit(changeNumber int64) (string, error) {
	change, err := s.db.GetChange(changeNumber)
	if err != nil {
		return "", err
	}
	strategy := "REBASE_IF_NECESSARY"
	if p, err := s.db.GetProject(change.Project); err == nil && p.SubmitType != "" {
		strategy = p.SubmitType
	}
	return s.SubmitWithType(changeNumber, strategy)
}

// SubmitWithType merges a change using an explicit Gerrit submit strategy:
// FAST_FORWARD_ONLY, REBASE_IF_NECESSARY, REBASE_ALWAYS, MERGE_IF_NECESSARY,
// MERGE_ALWAYS or CHERRY_PICK.
func (s *Service) SubmitWithType(changeNumber int64, strategy string) (string, error) {
	change, err := s.db.GetChange(changeNumber)
	if err != nil {
		return "", err
	}
	if change.Status != "NEW" {
		return "", ErrNotSubmittable
	}
	ps, err := s.db.GetPatchSet(change.Number, change.CurrentPS)
	if err != nil {
		return "", err
	}
	repo, err := s.OpenRepo(change.Project)
	if err != nil {
		return "", err
	}
	changeSHA := plumbing.NewHash(ps.CommitSHA)
	branchRef := plumbing.NewBranchReferenceName(change.Branch)

	var tip plumbing.Hash
	if r, err := repo.Reference(branchRef, true); err == nil {
		tip = r.Hash()
	}
	if tip == changeSHA {
		return changeSHA.String(), s.markSubmitted(change.Number)
	}

	ffPossible := tip.IsZero()
	if !ffPossible {
		ok, err := isAncestor(repo, tip, changeSHA)
		if err != nil {
			return "", err
		}
		ffPossible = ok
	}

	switch strategy {
	case "FAST_FORWARD_ONLY":
		if !ffPossible {
			return "", ErrNotFastForward
		}
		return s.ffSubmit(repo, branchRef, changeSHA, change.Number)
	case "REBASE_IF_NECESSARY":
		if ffPossible {
			return s.ffSubmit(repo, branchRef, changeSHA, change.Number)
		}
		return s.rebaseSubmit(change, ps, tip, changeSHA)
	case "MERGE_IF_NECESSARY":
		if ffPossible {
			return s.ffSubmit(repo, branchRef, changeSHA, change.Number)
		}
		return s.mergeSubmit(change, ps, tip, changeSHA)
	case "MERGE_ALWAYS":
		if tip.IsZero() {
			return s.ffSubmit(repo, branchRef, changeSHA, change.Number)
		}
		return s.mergeSubmit(change, ps, tip, changeSHA)
	case "REBASE_ALWAYS":
		if tip.IsZero() {
			return s.ffSubmit(repo, branchRef, changeSHA, change.Number)
		}
		return s.rebaseSubmit(change, ps, tip, changeSHA)
	case "CHERRY_PICK":
		if tip.IsZero() {
			return s.ffSubmit(repo, branchRef, changeSHA, change.Number)
		}
		return s.cherryPickSubmit(change, ps, tip, changeSHA)
	default:
		return "", fmt.Errorf("unknown submit type %q", strategy)
	}
}

func (s *Service) ffSubmit(repo *git.Repository, branchRef plumbing.ReferenceName, sha plumbing.Hash, num int64) (string, error) {
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, sha)); err != nil {
		return "", err
	}
	return sha.String(), s.markSubmitted(num)
}

// IsAncestor reports whether commit `ancestor` is reachable from `descendant`
// within a project's repository.
func (s *Service) IsAncestor(project, ancestor, descendant string) (bool, error) {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return false, err
	}
	return isAncestor(repo, plumbing.NewHash(ancestor), plumbing.NewHash(descendant))
}

// runGit executes the system git binary in dir, returning trimmed stdout.
func (s *Service) runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=gerrit-go", "GIT_AUTHOR_EMAIL=gerrit-go@localhost",
		"GIT_COMMITTER_NAME=gerrit-go", "GIT_COMMITTER_EMAIL=gerrit-go@localhost",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true",
	)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// runGitRaw is runGit without trailing-whitespace trimming, for commands whose
// stdout is file content that must be preserved byte-for-byte.
func (s *Service) runGitRaw(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true",
	)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return out.String(), nil
}

// prepareWork clones the bare repository into a temporary work tree, fetches the
// change's patch-set ref, and checks out the destination branch at its current
// tip so a merge/rebase/cherry-pick can be performed.
func (s *Service) prepareWork(change *store.Change, psNum int, tip plumbing.Hash) (work string, cleanup func(), err error) {
	bareDir := s.RepoDir(change.Project)
	tmp, err := os.MkdirTemp("", "gerritgo-submit-*")
	if err != nil {
		return "", nil, err
	}
	work = filepath.Join(tmp, "repo")
	cleanup = func() { os.RemoveAll(tmp) }
	if _, err := s.runGit(tmp, "clone", "--quiet", "--no-checkout", bareDir, work); err != nil {
		cleanup()
		return "", nil, err
	}
	changeRef := fmt.Sprintf("refs/changes/%02d/%d/%d", change.Number%100, change.Number, psNum)
	if _, err := s.runGit(work, "fetch", "--quiet", "origin", changeRef); err != nil {
		cleanup()
		return "", nil, err
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "-B", change.Branch, tip.String()); err != nil {
		cleanup()
		return "", nil, err
	}
	return work, cleanup, nil
}

// pushResult writes the computed commit to the bare repo's branch ref and marks
// the change submitted.
func (s *Service) pushResult(change *store.Change, work, sha string) (string, error) {
	if _, err := s.runGit(work, "push", "--quiet", "origin", sha+":refs/heads/"+change.Branch); err != nil {
		return "", err
	}
	return sha, s.markSubmitted(change.Number)
}

func (s *Service) mergeSubmit(change *store.Change, ps *store.PatchSet, tip, changeSHA plumbing.Hash) (string, error) {
	work, cleanup, err := s.prepareWork(change, ps.Number, tip)
	if err != nil {
		return "", err
	}
	defer cleanup()
	msg := fmt.Sprintf("Merge change %d into %s", change.Number, change.Branch)
	if _, err := s.runGit(work, "merge", "--no-ff", "--no-edit", "-m", msg, changeSHA.String()); err != nil {
		s.runGit(work, "merge", "--abort")
		return "", ErrMergeConflict
	}
	sha, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return s.pushResult(change, work, sha)
}

func (s *Service) rebaseSubmit(change *store.Change, ps *store.PatchSet, tip, changeSHA plumbing.Hash) (string, error) {
	work, cleanup, err := s.prepareWork(change, ps.Number, tip)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if _, err := s.runGit(work, "checkout", "--quiet", "-b", "gerrit-submit", changeSHA.String()); err != nil {
		return "", err
	}
	if _, err := s.runGit(work, "rebase", change.Branch); err != nil {
		s.runGit(work, "rebase", "--abort")
		return "", ErrRebaseConflict
	}
	sha, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return s.pushResult(change, work, sha)
}

func (s *Service) cherryPickSubmit(change *store.Change, ps *store.PatchSet, tip, changeSHA plumbing.Hash) (string, error) {
	work, cleanup, err := s.prepareWork(change, ps.Number, tip)
	if err != nil {
		return "", err
	}
	defer cleanup()
	// Cherry-pick the whole change range when it is a chain, else the commit.
	args := []string{"cherry-pick", changeSHA.String()}
	if base, berr := s.runGit(work, "merge-base", change.Branch, changeSHA.String()); berr == nil && base != "" && base != changeSHA.String() {
		args = []string{"cherry-pick", base + ".." + changeSHA.String()}
	}
	if _, err := s.runGit(work, args...); err != nil {
		s.runGit(work, "cherry-pick", "--abort")
		return "", ErrCherryPickConflict
	}
	sha, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return s.pushResult(change, work, sha)
}
