package gitsvc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrEditStale is returned when publishing an edit whose base patch set is no
// longer the change's current patch set.
var ErrEditStale = errors.New("edit is based on an older patch set")

// EditInfo describes an open change edit. The edit is stored as a real commit
// at refs/edits/<NN>/<change>; BaseSHA is the patch-set commit the edit was
// started from (the edit commit's parent), and BasePS is its patch-set number.
type EditInfo struct {
	SHA         string `json:"commit"`
	BaseSHA     string `json:"base_commit"`
	BasePS      int    `json:"base_ps"`
	AuthorName  string `json:"-"`
	AuthorEmail string `json:"-"`
	Message     string `json:"-"`
}

// editRef returns the ref holding the single open edit for a change. Edits are
// per-change (not per-user) to keep the model simple; a second editor gets a
// conflict until the first edit is published or deleted.
func (s *Service) editRef(changeNumber int64) string {
	return fmt.Sprintf("refs/edits/%02d/%d", changeNumber%100, changeNumber)
}

func (s *Service) lockEdit(changeNumber int64) *sync.Mutex {
	v, _ := s.editLocks.LoadOrStore(changeNumber, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m
}

// EditExists reports whether a change currently has an open edit.
func (s *Service) EditExists(project string, changeNumber int64) bool {
	return s.refExists(project, s.editRef(changeNumber))
}

// GetEdit returns the open edit for a change, or ErrRefMissing when none
// exists. It works against the bare repo (no clone).
func (s *Service) GetEdit(project string, changeNumber int64) (*EditInfo, error) {
	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	ref := s.editRef(changeNumber)
	sha, err := s.runGit(s.RepoDir(project), "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return nil, ErrRefMissing
	}
	return s.editInfoFromSHA(project, changeNumber, sha)
}

// editInfoFromSHA builds an EditInfo for the edit commit sha, resolving its
// base patch set from the commit's first parent.
func (s *Service) editInfoFromSHA(project string, changeNumber int64, sha string) (*EditInfo, error) {
	dir := s.RepoDir(project)
	nc, err := s.readCommit(dir, sha)
	if err != nil {
		return nil, err
	}
	baseSHA, _ := s.runGit(dir, "rev-parse", "--verify", sha+"^")
	info := &EditInfo{
		SHA:         nc.SHA,
		BaseSHA:     baseSHA,
		AuthorName:  nc.AuthorName,
		AuthorEmail: nc.AuthorEmail,
		Message:     nc.Message,
	}
	// Map the base commit back to its patch-set number (0 when not found, e.g.
	// the edit was just created lazily and still points at the PS commit).
	if pss, err := s.db.ListPatchSets(changeNumber); err == nil {
		for _, ps := range pss {
			if ps.CommitSHA == baseSHA || ps.CommitSHA == sha {
				info.BasePS = ps.Number
			}
		}
	}
	return info, nil
}

// CreateEdit opens an edit on the change's current patch set. It is lazy: the
// edit ref simply points at the current patch-set commit, and the first file
// write performs the actual amend. Returns ErrRefExists when an edit is
// already open.
func (s *Service) CreateEdit(project string, changeNumber int64) (*EditInfo, error) {
	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	c, err := s.db.GetChange(changeNumber)
	if err != nil {
		return nil, err
	}
	if c.Status != "NEW" {
		return nil, ErrNotSubmittable
	}
	ps, err := s.db.GetPatchSet(changeNumber, c.CurrentPS)
	if err != nil {
		return nil, err
	}
	ref := s.editRef(changeNumber)
	if s.refExists(project, ref) {
		return nil, ErrRefExists
	}
	if _, err := s.runGit(s.RepoDir(project), "update-ref", ref, ps.CommitSHA); err != nil {
		return nil, err
	}
	return s.editInfoFromSHA(project, changeNumber, ps.CommitSHA)
}

// applyEditOp clones the repo, checks out the edit commit, applies op to the
// work tree, amends the edit commit (preserving its message and Change-Id),
// and force-pushes the result back to the edit ref. It returns the refreshed
// edit. When op produces no staged change the edit is returned unchanged.
func (s *Service) applyEditOp(project string, changeNumber int64, authorName, authorEmail string, op func(work string) error) (*EditInfo, error) {
	m := s.lockEdit(changeNumber)
	defer m.Unlock()

	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	ref := s.editRef(changeNumber)
	editSHA, err := s.runGit(s.RepoDir(project), "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return nil, ErrRefMissing
	}

	work, cleanup, err := s.cloneWork(project)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, err := s.runGit(work, "fetch", "--quiet", "origin", ref); err != nil {
		return nil, err
	}
	if _, err := s.runGit(work, "checkout", "--quiet", "--detach", editSHA); err != nil {
		return nil, err
	}
	if err := op(work); err != nil {
		return nil, err
	}
	if _, err := s.runGit(work, "add", "-A"); err != nil {
		return nil, err
	}
	// No staged change => nothing to amend; return the edit as-is.
	if _, err := s.runGit(work, "diff", "--cached", "--quiet"); err == nil {
		return s.editInfoFromSHA(project, changeNumber, editSHA)
	}
	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)
	if _, err := s.runGit(work, "commit", "--quiet", "--amend", "--no-edit", "--author="+author); err != nil {
		return nil, err
	}
	newSHA, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if _, err := s.runGit(work, "push", "--quiet", "--force", "origin", newSHA+":"+ref); err != nil {
		return nil, err
	}
	return s.editInfoFromSHA(project, changeNumber, newSHA)
}

// PutEditFile writes content to filePath within the open edit, amending the
// edit commit. The edit must already exist (see CreateEdit).
func (s *Service) PutEditFile(project string, changeNumber int64, filePath string, content []byte, authorName, authorEmail string) (*EditInfo, error) {
	cleanPath, ok := safeFilePath(filePath)
	if !ok {
		return nil, ErrInvalidRef
	}
	return s.applyEditOp(project, changeNumber, authorName, authorEmail, func(work string) error {
		abs := filepath.Join(work, filepath.FromSlash(cleanPath))
		if !strings.HasPrefix(abs, filepath.Clean(work)+string(os.PathSeparator)) {
			return ErrInvalidRef
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, content, 0o644)
	})
}

// DeleteEditFile removes filePath within the open edit, amending the edit
// commit. The edit must already exist.
func (s *Service) DeleteEditFile(project string, changeNumber int64, filePath string, authorName, authorEmail string) (*EditInfo, error) {
	cleanPath, ok := safeFilePath(filePath)
	if !ok {
		return nil, ErrInvalidRef
	}
	return s.applyEditOp(project, changeNumber, authorName, authorEmail, func(work string) error {
		abs := filepath.Join(work, filepath.FromSlash(cleanPath))
		if !strings.HasPrefix(abs, filepath.Clean(work)+string(os.PathSeparator)) {
			return ErrInvalidRef
		}
		if _, err := s.runGit(work, "rm", "--quiet", "--ignore-unmatch", "--", cleanPath); err != nil {
			return err
		}
		return nil
	})
}

// DeleteEdit discards the open edit without publishing it.
func (s *Service) DeleteEdit(project string, changeNumber int64) error {
	if !s.ProjectExists(project) {
		return ErrProjectMissing
	}
	ref := s.editRef(changeNumber)
	if !s.refExists(project, ref) {
		return ErrRefMissing
	}
	_, err := s.runGit(s.RepoDir(project), "update-ref", "-d", ref)
	return err
}

// PublishEdit turns the open edit into the change's next patch set: it pushes
// the edit commit to refs/changes/<NN>/<num>/<ps+1> and consumes the edit ref.
// The edit must be based on the current patch set (ErrEditStale otherwise) and
// must differ from it (ErrNoChanges when the edit was never modified).
func (s *Service) PublishEdit(project string, changeNumber int64) (NewCommit, error) {
	m := s.lockEdit(changeNumber)
	defer m.Unlock()

	if !s.ProjectExists(project) {
		return NewCommit{}, ErrProjectMissing
	}
	c, err := s.db.GetChange(changeNumber)
	if err != nil {
		return NewCommit{}, err
	}
	if c.Status != "NEW" {
		return NewCommit{}, ErrNotSubmittable
	}
	ps, err := s.db.GetPatchSet(changeNumber, c.CurrentPS)
	if err != nil {
		return NewCommit{}, err
	}
	ref := s.editRef(changeNumber)
	editSHA, err := s.runGit(s.RepoDir(project), "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return NewCommit{}, ErrRefMissing
	}
	if editSHA == ps.CommitSHA {
		return NewCommit{}, ErrNoChanges
	}
	// The edit commit's parent must be the current patch set; otherwise the
	// edit is stale (a newer patch set was uploaded after the edit started).
	// Edge case: when the current patch set is a root commit (an empty repo's
	// first change), amending it yields another root commit with no parent, so
	// the parent lookup fails; treat a root edit as based on a root patch set.
	dir := s.RepoDir(project)
	baseSHA, baseErr := s.runGit(dir, "rev-parse", "--verify", editSHA+"^")
	if baseErr != nil {
		// Edit is a root commit: only valid when the current patch set is too.
		if _, psErr := s.runGit(dir, "rev-parse", "--verify", ps.CommitSHA+"^"); psErr == nil {
			return NewCommit{}, ErrEditStale
		}
	} else if baseSHA != ps.CommitSHA {
		return NewCommit{}, ErrEditStale
	}
	nc, err := s.readCommit(dir, editSHA)
	if err != nil {
		return NewCommit{}, err
	}
	newPS := c.CurrentPS + 1
	if _, err := s.runGit(dir, "update-ref", s.changeRef(changeNumber, newPS), editSHA); err != nil {
		return NewCommit{}, err
	}
	if _, err := s.runGit(dir, "update-ref", "-d", ref); err != nil {
		return NewCommit{}, err
	}
	nc.NewPatchSet = newPS
	return nc, nil
}
