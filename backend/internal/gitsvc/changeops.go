package gitsvc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
)

// NewCommit is the result of a rebase/cherry-pick/revert operation: the freshly
// created commit and the patch-set number it should be recorded as.
type NewCommit struct {
	SHA         string
	AuthorName  string
	AuthorEmail string
	Message     string
	NewPatchSet int
}

// GenerateChangeID returns a fresh Gerrit-style Change-Id token.
func (s *Service) GenerateChangeID() string { return generateChangeID() }

func (s *Service) cloneWork(project string) (string, func(), error) {
	bareDir := s.RepoDir(project)
	tmp, err := os.MkdirTemp("", "gerritgo-op-*")
	if err != nil {
		return "", nil, err
	}
	work := filepath.Join(tmp, "repo")
	cleanup := func() { os.RemoveAll(tmp) }
	if _, err := s.runGit(tmp, "clone", "--quiet", "--no-checkout", bareDir, work); err != nil {
		cleanup()
		return "", nil, err
	}
	return work, cleanup, nil
}

func (s *Service) branchTip(project, branch string) plumbing.Hash {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return plumbing.ZeroHash
	}
	if r, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true); err == nil {
		return r.Hash()
	}
	return plumbing.ZeroHash
}

func (s *Service) changeRef(changeNumber int64, ps int) string {
	return fmt.Sprintf("refs/changes/%02d/%d/%d", changeNumber%100, changeNumber, ps)
}

func (s *Service) readCommit(work, sha string) (NewCommit, error) {
	out, err := s.runGit(work, "show", "-s", "--format=%H%x1f%an%x1f%ae%x1f%B", sha)
	if err != nil {
		return NewCommit{}, err
	}
	parts := strings.SplitN(out, "\x1f", 4)
	if len(parts) < 4 {
		return NewCommit{}, errors.New("unexpected git output")
	}
	return NewCommit{SHA: parts[0], AuthorName: parts[1], AuthorEmail: parts[2], Message: parts[3]}, nil
}

// setChangeID replaces (or appends) the Change-Id trailer in a commit message.
func setChangeID(msg, id string) string {
	if changeIDRe.MatchString(msg) {
		return changeIDRe.ReplaceAllString(msg, "Change-Id: "+id)
	}
	return strings.TrimRight(msg, "\n") + "\n\nChange-Id: " + id + "\n"
}

func (s *Service) amendMessage(work, msg string) error {
	f, err := os.CreateTemp("", "gerritgo-msg-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(msg); err != nil {
		f.Close()
		return err
	}
	f.Close()
	_, err = s.runGit(work, "commit", "--quiet", "--amend", "-F", f.Name())
	return err
}

// Rebase replays a change's commits onto the current tip of its destination
// branch, pushing the result as the next patch set.
func (s *Service) Rebase(changeNumber int64) (NewCommit, error) {
	change, err := s.db.GetChange(changeNumber)
	if err != nil {
		return NewCommit{}, err
	}
	if change.Status != "NEW" {
		return NewCommit{}, ErrNotSubmittable
	}
	ps, err := s.db.GetPatchSet(change.Number, change.CurrentPS)
	if err != nil {
		return NewCommit{}, err
	}
	tip := s.branchTip(change.Project, change.Branch)
	if tip.IsZero() {
		return NewCommit{}, errors.New("destination branch does not exist")
	}
	changeSHA := plumbing.NewHash(ps.CommitSHA)
	repo, err := s.OpenRepo(change.Project)
	if err != nil {
		return NewCommit{}, err
	}
	if tip == changeSHA {
		return NewCommit{}, errors.New("change is already up to date")
	}
	if ok, _ := isAncestor(repo, tip, changeSHA); ok {
		return NewCommit{}, errors.New("change is already up to date")
	}

	work, cleanup, err := s.cloneWork(change.Project)
	if err != nil {
		return NewCommit{}, err
	}
	defer cleanup()
	if _, err := s.runGit(work, "fetch", "--quiet", "origin", s.changeRef(change.Number, ps.Number)); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "-B", change.Branch, tip.String()); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "-b", "gerrit-rebase", changeSHA.String()); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "rebase", change.Branch); err != nil {
		s.runGit(work, "rebase", "--abort")
		return NewCommit{}, ErrRebaseConflict
	}
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

// CherryPick applies a change's current commit onto targetBranch as a new commit
// carrying newChangeID, pushed as patch set 1 of newChangeNumber.
func (s *Service) CherryPick(srcChangeNumber int64, targetBranch, newChangeID string, newChangeNumber int64) (NewCommit, error) {
	src, err := s.db.GetChange(srcChangeNumber)
	if err != nil {
		return NewCommit{}, err
	}
	ps, err := s.db.GetPatchSet(src.Number, src.CurrentPS)
	if err != nil {
		return NewCommit{}, err
	}
	tip := s.branchTip(src.Project, targetBranch)
	if tip.IsZero() {
		return NewCommit{}, errors.New("destination branch does not exist")
	}
	work, cleanup, err := s.cloneWork(src.Project)
	if err != nil {
		return NewCommit{}, err
	}
	defer cleanup()
	if _, err := s.runGit(work, "fetch", "--quiet", "origin", s.changeRef(src.Number, ps.Number)); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "-B", targetBranch, tip.String()); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "cherry-pick", "-x", ps.CommitSHA); err != nil {
		s.runGit(work, "cherry-pick", "--abort")
		return NewCommit{}, ErrCherryPickConflict
	}
	cur, err := s.runGit(work, "log", "-1", "--format=%B")
	if err != nil {
		return NewCommit{}, err
	}
	if err := s.amendMessage(work, setChangeID(cur, newChangeID)); err != nil {
		return NewCommit{}, err
	}
	return s.finishNewChange(work, src.Project, targetBranch, newChangeNumber, newChangeID)
}

// Revert creates a commit reverting a merged change on its own branch, pushed as
// patch set 1 of newChangeNumber.
func (s *Service) Revert(srcChangeNumber int64, newChangeID string, newChangeNumber int64) (NewCommit, error) {
	src, err := s.db.GetChange(srcChangeNumber)
	if err != nil {
		return NewCommit{}, err
	}
	if src.Status != "MERGED" {
		return NewCommit{}, errors.New("only merged changes can be reverted")
	}
	ps, err := s.db.GetPatchSet(src.Number, src.CurrentPS)
	if err != nil {
		return NewCommit{}, err
	}
	tip := s.branchTip(src.Project, src.Branch)
	if tip.IsZero() {
		return NewCommit{}, errors.New("destination branch does not exist")
	}
	work, cleanup, err := s.cloneWork(src.Project)
	if err != nil {
		return NewCommit{}, err
	}
	defer cleanup()
	if _, err := s.runGit(work, "checkout", "--quiet", "-B", src.Branch, tip.String()); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "revert", "--no-edit", ps.CommitSHA); err != nil {
		s.runGit(work, "revert", "--abort")
		return NewCommit{}, errors.New("revert conflict; the change cannot be reverted cleanly")
	}
	cur, err := s.runGit(work, "log", "-1", "--format=%B")
	if err != nil {
		return NewCommit{}, err
	}
	if err := s.amendMessage(work, setChangeID(cur, newChangeID)); err != nil {
		return NewCommit{}, err
	}
	return s.finishNewChange(work, src.Project, src.Branch, newChangeNumber, newChangeID)
}

// finishNewChange reads HEAD, pushes it as patch set 1 of a new change, and
// returns the commit metadata.
func (s *Service) finishNewChange(work, project, branch string, newChangeNumber int64, newChangeID string) (NewCommit, error) {
	sha, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return NewCommit{}, err
	}
	nc, err := s.readCommit(work, sha)
	if err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(work, "push", "--quiet", "origin", sha+":"+s.changeRef(newChangeNumber, 1)); err != nil {
		return NewCommit{}, err
	}
	nc.NewPatchSet = 1
	return nc, nil
}
