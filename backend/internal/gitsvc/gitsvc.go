package gitsvc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gerrit-go/internal/store"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	fdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var (
	ErrProjectExists  = errors.New("project already exists")
	ErrProjectMissing = errors.New("project not found")
	ErrNotFastForward = errors.New("change is not a fast-forward of the destination branch; rebase required")
	ErrNotSubmittable = errors.New("change cannot be submitted in its current state")
)

var changeIDRe = regexp.MustCompile(`(?m)^Change-Id:\s*(I[0-9a-f]{8,40})\s*$`)

type Service struct {
	basePath string
	db       *store.DB
	// editLocks serializes change-edit operations per change number so
	// concurrent amends/publishes on the same edit ref cannot interleave.
	editLocks sync.Map
}

func New(basePath string, db *store.DB) *Service {
	return &Service{basePath: basePath, db: db}
}

func (s *Service) RepoDir(project string) string {
	return filepath.Join(s.basePath, project+".git")
}

func (s *Service) CreateProject(name string, description string) error {
	if _, err := s.db.GetProject(name); err == nil {
		return ErrProjectExists
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return errors.New("invalid project name")
	}
	dir := s.RepoDir(name)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if _, err := git.PlainInit(dir, true); err != nil {
		return err
	}
	if err := EnableReceivePack(dir); err != nil {
		return err
	}
	if err := writeHead(dir, "master"); err != nil {
		return err
	}
	if err := s.db.CreateProject(&store.Project{Name: name, Description: description, Head: "master"}); err != nil {
		return err
	}
	return nil
}

func writeHead(dir, branch string) error {
	return os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644)
}

func (s *Service) OpenRepo(project string) (*git.Repository, error) {
	if _, err := s.db.GetProject(project); err != nil {
		return nil, ErrProjectMissing
	}
	return git.PlainOpen(s.RepoDir(project))
}

// Backend returns the smart-HTTP handler backed by the system git binary.
func (s *Service) Backend() *GitHTTPBackend {
	return NewGitHTTPBackend(s.basePath)
}

func (s *Service) ProjectExists(project string) bool {
	_, err := s.db.GetProject(project)
	return err == nil
}

// ---------- refs/for processing ----------

// ProcessReceivePack runs after a successful git-receive-pack push: it converts
// any refs/for/<branch> advertisements into changes + patch sets, mirroring
// Gerrit's magic-branch behaviour, then removes the magic refs.
func (s *Service) ProcessReceivePack(project string, pusher *store.Account) error {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return err
	}
	refs, err := repo.References()
	if err != nil {
		return err
	}
	var magic []*plumbing.Reference
	err = refs.ForEach(func(r *plumbing.Reference) error {
		if strings.HasPrefix(r.Name().String(), "refs/for/") {
			magic = append(magic, r)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, m := range magic {
		target := strings.TrimPrefix(m.Name().String(), "refs/for/")
		if err := s.createChangesFromPush(repo, project, target, m.Hash(), pusher); err != nil {
			fmt.Fprintf(os.Stderr, "refs/for/%s: %v\n", target, err)
		}
		if err := repo.Storer.RemoveReference(m.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) createChangesFromPush(repo *git.Repository, project, branch string, tip plumbing.Hash, pusher *store.Account) error {
	branchRef := plumbing.NewBranchReferenceName(branch)
	var base plumbing.Hash
	if r, err := repo.Reference(branchRef, true); err == nil {
		base = r.Hash()
	}

	// Collect commits reachable from tip but not from the branch tip (oldest first).
	var commits []*object.Commit
	if base == tip {
		return nil
	}
	it, err := repo.Log(&git.LogOptions{From: tip})
	if err != nil {
		return err
	}
	err = it.ForEach(func(c *object.Commit) error {
		if c.Hash == base {
			return io.EOF
		}
		commits = append(commits, c)
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].Author.When.Before(commits[j].Author.When)
	})

	for _, c := range commits {
		if err := s.upsertChange(repo, project, branch, c, pusher); err != nil {
			return fmt.Errorf("commit %s: %w", c.Hash, err)
		}
	}
	return nil
}

func (s *Service) upsertChange(repo *git.Repository, project, branch string, c *object.Commit, pusher *store.Account) error {
	subject := strings.SplitN(strings.TrimSpace(c.Message), "\n", 2)[0]

	changeID := ""
	if m := changeIDRe.FindStringSubmatch(c.Message); len(m) == 2 {
		changeID = m[1]
	}

	var change *store.Change
	if changeID != "" {
		existing, err := s.db.GetChangeByChangeID(project, branch, changeID)
		if err == nil {
			if existing.Status == "NEW" {
				change = existing
			} else {
				// Change-Id of a closed change cannot be reused; start fresh.
				changeID = generateChangeID()
			}
		}
	}
	if change == nil {
		if changeID == "" {
			changeID = generateChangeID()
		}
		change = &store.Change{
			Project: project, Branch: branch, ChangeID: changeID,
			Subject: subject, OwnerID: pusher.ID,
		}
		if err := s.db.CreateChange(change); err != nil {
			return err
		}
		if err := s.db.AddReviewer(change.Number, pusher.ID); err != nil {
			return err
		}
	}

	// Duplicate patch set (same commit already uploaded) is a no-op.
	pss, err := s.db.ListPatchSets(change.Number)
	if err != nil {
		return err
	}
	for _, ps := range pss {
		if ps.CommitSHA == c.Hash.String() {
			return nil
		}
	}

	num := change.CurrentPS + 1
	ps := &store.PatchSet{
		ChangeNumber: change.Number, Number: num, CommitSHA: c.Hash.String(),
		AuthorName: c.Author.Name, AuthorEmail: c.Author.Email, Message: c.Message,
	}
	if err := s.db.CreatePatchSet(ps); err != nil {
		return err
	}
	if err := s.db.SetCurrentPatchSet(change.Number, num); err != nil {
		return err
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: change.Number, PatchSet: num, Type: "patchset-uploaded",
		AuthorID: pusher.ID, Message: fmt.Sprintf("Uploaded patch set %d.", num),
	})

	refName := fmt.Sprintf("refs/changes/%02d/%d/%d", change.Number%100, change.Number, num)
	return repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(refName), c.Hash))
}

func generateChangeID() string {
	buf := make([]byte, 20)
	rand.Read(buf)
	return "I" + hex.EncodeToString(buf)
}

// ---------- submit ----------

func (s *Service) markSubmitted(changeNumber int64) error {
	t := time.Now()
	return s.db.UpdateChangeStatus(changeNumber, "MERGED", &t)
}

// isAncestor reports whether `from` is reachable from `to`.
func isAncestor(repo *git.Repository, from, to plumbing.Hash) (bool, error) {
	if from == to {
		return true, nil
	}
	if from.IsZero() {
		return true, nil
	}
	it, err := repo.Log(&git.LogOptions{From: to})
	if err != nil {
		return false, err
	}
	found := false
	err = it.ForEach(func(c *object.Commit) error {
		for _, p := range c.ParentHashes {
			if p == from {
				found = true
				return io.EOF
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return found, nil
}

// ---------- diff ----------

type DiffLine struct {
	Type  string `json:"type"` // context | add | del
	Text  string `json:"text"`
	OldNo int    `json:"old_no,omitempty"`
	NewNo int    `json:"new_no,omitempty"`
}

type DiffHunk struct {
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

type FileDiff struct {
	Path     string     `json:"path"`
	OldPath  string     `json:"old_path,omitempty"`
	Status   string     `json:"status"` // A | M | D | R
	Binary   bool       `json:"binary,omitempty"`
	Hunks    []DiffHunk `json:"hunks"`
	AddCount int        `json:"add_count"`
	DelCount int        `json:"del_count"`
}

// PatchSetDiff computes the diff of a patch set against its parent commit.
func (s *Service) PatchSetDiff(changeNumber int64, psNumber int) ([]*FileDiff, error) {
	ps, err := s.db.GetPatchSet(changeNumber, psNumber)
	if err != nil {
		return nil, err
	}
	change, err := s.db.GetChange(changeNumber)
	if err != nil {
		return nil, err
	}
	repo, err := s.OpenRepo(change.Project)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(plumbing.NewHash(ps.CommitSHA))
	if err != nil {
		return nil, err
	}
	patch, err := patchAgainstParent(commit)
	if err != nil {
		return nil, err
	}
	return patchToFileDiffs(patch), nil
}

// patchAgainstParent returns the unified diff introduced by the commit
// (first parent → commit); initial commits diff against the empty tree.
func patchAgainstParent(commit *object.Commit) (*object.Patch, error) {
	commitTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	if commit.NumParents() == 0 {
		empty := &object.Tree{}
		return empty.Patch(commitTree)
	}
	parent, err := commit.Parent(0)
	if err != nil {
		return nil, err
	}
	parentTree, err := parent.Tree()
	if err != nil {
		return nil, err
	}
	return parentTree.Patch(commitTree)
}

func patchToFileDiffs(patch *object.Patch) []*FileDiff {
	var out []*FileDiff
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		fd := &FileDiff{Binary: fp.IsBinary()}
		switch {
		case from == nil:
			fd.Status = "A"
			fd.Path = to.Path()
		case to == nil:
			fd.Status = "D"
			fd.Path = from.Path()
		case from.Path() != to.Path():
			fd.Status = "R"
			fd.OldPath = from.Path()
			fd.Path = to.Path()
		default:
			fd.Status = "M"
			fd.Path = from.Path()
		}
		if !fd.Binary {
			oldNo, newNo := 1, 1
			var hunk *DiffHunk
			for _, ch := range fp.Chunks() {
				content := ch.Content()
				var typ string
				switch ch.Type() {
				case fdiff.Add:
					typ = "add"
				case fdiff.Delete:
					typ = "del"
				default:
					typ = "context"
				}
				lines := splitLines(content)
				if len(lines) == 0 {
					continue
				}
				if hunk == nil {
					hunk = &DiffHunk{Header: fmt.Sprintf("@@ -%d +%d @@", oldNo, newNo)}
				}
				for _, ln := range lines {
					dl := DiffLine{Type: typ, Text: ln}
					switch typ {
					case "add":
						dl.NewNo = newNo
						newNo++
						fd.AddCount++
					case "del":
						dl.OldNo = oldNo
						oldNo++
						fd.DelCount++
					default:
						dl.OldNo = oldNo
						dl.NewNo = newNo
						oldNo++
						newNo++
					}
					hunk.Lines = append(hunk.Lines, dl)
				}
			}
			if hunk != nil {
				fd.Hunks = append(fd.Hunks, *hunk)
			}
		}
		out = append(out, fd)
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// ---------- repository browsing ----------

type FileEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // tree | blob
	Size int64  `json:"size,omitempty"`
}

type CommitInfo struct {
	SHA     string    `json:"sha"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
	Message string    `json:"message,omitempty"`
}

type BranchInfo struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

func (s *Service) ListBranches(project string) ([]*BranchInfo, error) {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return nil, err
	}
	branches, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	var out []*BranchInfo
	branches.ForEach(func(b *plumbing.Reference) error {
		out = append(out, &BranchInfo{Name: strings.TrimPrefix(b.Name().String(), "refs/heads/"), SHA: b.Hash().String()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// headOrFirstBranch resolves the repository HEAD, falling back to the first
// available branch when HEAD is unborn or points at a missing ref. Repositories
// imported from Gerrit commonly have a HEAD referencing a branch (e.g. master)
// that was never created locally, which would otherwise make tree/commit views
// fail even though the repository has valid branches.
func headOrFirstBranch(repo *git.Repository) (plumbing.Hash, error) {
	if head, err := repo.Head(); err == nil {
		return head.Hash(), nil
	}
	branches, err := repo.Branches()
	if err != nil {
		return plumbing.ZeroHash, ErrProjectMissing
	}
	var first plumbing.Hash
	var found bool
	_ = branches.ForEach(func(ref *plumbing.Reference) error {
		first = ref.Hash()
		found = true
		return io.EOF
	})
	if !found {
		return plumbing.ZeroHash, ErrProjectMissing
	}
	return first, nil
}

func (s *Service) resolveCommit(repo *git.Repository, rev string) (*object.Commit, error) {
	if rev == "" {
		h, err := headOrFirstBranch(repo)
		if err != nil {
			return nil, err
		}
		return repo.CommitObject(h)
	}
	h := plumbing.NewHash(rev)
	if h.String() == rev {
		return repo.CommitObject(h)
	}
	resolved, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, err
	}
	return repo.CommitObject(*resolved)
}

func (s *Service) ListTree(project, rev, path string) ([]*FileEntry, error) {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return nil, err
	}
	c, err := s.resolveCommit(repo, rev)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	if path != "" {
		tree, err = tree.Tree(path)
		if err != nil {
			return nil, err
		}
	}
	var out []*FileEntry
	for _, e := range tree.Entries {
		fe := &FileEntry{Name: e.Name}
		if e.Mode == 0o40000 {
			fe.Type = "tree"
		} else {
			fe.Type = "blob"
			if f, err := tree.File(e.Name); err == nil {
				fe.Size = f.Size
			}
		}
		out = append(out, fe)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == "tree"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Service) FileContent(project, rev, path string) ([]byte, error) {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return nil, err
	}
	c, err := s.resolveCommit(repo, rev)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	f, err := tree.File(path)
	if err != nil {
		return nil, err
	}
	text, err := f.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (s *Service) ListCommits(project, rev string, limit int) ([]*CommitInfo, error) {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return nil, err
	}
	opts := &git.LogOptions{Order: git.LogOrderCommitterTime}
	if rev != "" {
		resolved, err := repo.ResolveRevision(plumbing.Revision(rev))
		if err != nil {
			return nil, err
		}
		opts.From = *resolved
	} else {
		h, err := headOrFirstBranch(repo)
		if err != nil {
			return nil, err
		}
		opts.From = h
	}
	it, err := repo.Log(opts)
	if err != nil {
		return nil, err
	}
	var out []*CommitInfo
	it.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) >= limit {
			return io.EOF
		}
		out = append(out, &CommitInfo{
			SHA:     c.Hash.String(),
			Author:  c.Author.Name,
			Email:   c.Author.Email,
			Date:    c.Author.When,
			Subject: strings.SplitN(c.Message, "\n", 2)[0],
		})
		return nil
	})
	return out, nil
}

// PatchText renders the raw unified diff of a patch set (Gerrit /patch style).
func (s *Service) PatchText(changeNumber int64, psNumber int) (string, error) {
	ps, err := s.db.GetPatchSet(changeNumber, psNumber)
	if err != nil {
		return "", err
	}
	change, err := s.db.GetChange(changeNumber)
	if err != nil {
		return "", err
	}
	repo, err := s.OpenRepo(change.Project)
	if err != nil {
		return "", err
	}
	commit, err := repo.CommitObject(plumbing.NewHash(ps.CommitSHA))
	if err != nil {
		return "", err
	}
	patch, err := patchAgainstParent(commit)
	if err != nil {
		return "", err
	}
	return patch.String(), nil
}

// FileAtPatchSet returns file content at a specific patch set (for the diff viewer).
func (s *Service) FileAtPatchSet(changeNumber int64, psNumber int, path string, side string) ([]byte, error) {
	ps, err := s.db.GetPatchSet(changeNumber, psNumber)
	if err != nil {
		return nil, err
	}
	change, err := s.db.GetChange(changeNumber)
	if err != nil {
		return nil, err
	}
	repo, err := s.OpenRepo(change.Project)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(plumbing.NewHash(ps.CommitSHA))
	if err != nil {
		return nil, err
	}
	if side == "parent" {
		if commit.NumParents() == 0 {
			return nil, nil
		}
		commit, err = commit.Parent(0)
		if err != nil {
			return nil, err
		}
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	f, err := tree.File(path)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, nil
		}
		return nil, err
	}
	text, err := f.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}
