package gitsvc

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrRefExists  = errors.New("ref already exists")
	ErrRefMissing = errors.New("ref does not exist")
	ErrInvalidRef = errors.New("invalid ref name")
	ErrNoChanges  = errors.New("no changes to commit")
)

// TagInfo describes a git tag (lightweight or annotated).
type TagInfo struct {
	Name    string `json:"name"`
	SHA     string `json:"sha"`
	Message string `json:"message,omitempty"`
}

// validRefName applies a conservative subset of git check-ref-format rules.
func validRefName(name string) bool {
	if name == "" || len(name) > 200 || strings.HasPrefix(name, "refs/") {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return false
	}
	for _, c := range name {
		if c <= 32 || c == 127 {
			return false
		}
		switch c {
		case '~', '^', ':', '?', '*', '[', '\\', ' ', '\t':
			return false
		}
	}
	for _, comp := range strings.Split(name, "/") {
		if comp == "" || comp == "." || strings.HasPrefix(comp, "-") || strings.HasSuffix(comp, ".lock") {
			return false
		}
	}
	return true
}

func (s *Service) refExists(project, ref string) bool {
	_, err := s.runGit(s.RepoDir(project), "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

func (s *Service) resolveRef(project, rev string) (string, error) {
	if rev == "" {
		rev = "HEAD"
	}
	sha, err := s.runGit(s.RepoDir(project), "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return sha, nil
}

// CreateBranch creates refs/heads/branch at the commit resolved from startRef
// (HEAD when empty).
func (s *Service) CreateBranch(project, branch, startRef string) (string, error) {
	if !s.ProjectExists(project) {
		return "", ErrProjectMissing
	}
	if !validRefName(branch) {
		return "", ErrInvalidRef
	}
	ref := "refs/heads/" + branch
	if s.refExists(project, ref) {
		return "", ErrRefExists
	}
	sha, err := s.resolveRef(project, startRef)
	if err != nil {
		return "", err
	}
	if _, err := s.runGit(s.RepoDir(project), "update-ref", ref, sha); err != nil {
		return "", err
	}
	return sha, nil
}

// DeleteBranch removes refs/heads/branch.
func (s *Service) DeleteBranch(project, branch string) error {
	if !s.ProjectExists(project) {
		return ErrProjectMissing
	}
	ref := "refs/heads/" + branch
	if !s.refExists(project, ref) {
		return ErrRefMissing
	}
	_, err := s.runGit(s.RepoDir(project), "update-ref", "-d", ref)
	return err
}

// ListTags returns all tags, sorted by name.
func (s *Service) ListTags(project string) ([]*TagInfo, error) {
	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	out, err := s.runGit(s.RepoDir(project), "for-each-ref",
		"--format=%(refname:short)\t%(objectname)\t%(*objectname)\t%(contents:subject)", "refs/tags")
	if err != nil {
		return nil, err
	}
	var tags []*TagInfo
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		sha := f[1]
		if len(f) > 2 && f[2] != "" {
			sha = f[2] // peeled commit of an annotated tag
		}
		msg := ""
		if len(f) > 3 {
			msg = f[3]
		}
		tags = append(tags, &TagInfo{Name: f[0], SHA: sha, Message: msg})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	return tags, nil
}

// CreateTag creates a tag at the commit resolved from startRef. When message is
// non-empty an annotated tag is created, otherwise a lightweight one.
func (s *Service) CreateTag(project, tag, startRef, message string) (string, error) {
	if !s.ProjectExists(project) {
		return "", ErrProjectMissing
	}
	if !validRefName(tag) {
		return "", ErrInvalidRef
	}
	ref := "refs/tags/" + tag
	if s.refExists(project, ref) {
		return "", ErrRefExists
	}
	sha, err := s.resolveRef(project, startRef)
	if err != nil {
		return "", err
	}
	dir := s.RepoDir(project)
	if strings.TrimSpace(message) != "" {
		if _, err := s.runGit(dir, "tag", "-a", tag, "-m", message, sha); err != nil {
			return "", err
		}
	} else {
		if _, err := s.runGit(dir, "tag", tag, sha); err != nil {
			return "", err
		}
	}
	return sha, nil
}

// DeleteTag removes refs/tags/tag.
func (s *Service) DeleteTag(project, tag string) error {
	if !s.ProjectExists(project) {
		return ErrProjectMissing
	}
	ref := "refs/tags/" + tag
	if !s.refExists(project, ref) {
		return ErrRefMissing
	}
	_, err := s.runGit(s.RepoDir(project), "update-ref", "-d", ref)
	return err
}

// safeFilePath normalizes a repository-relative file path, rejecting traversal.
func safeFilePath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	p = strings.TrimPrefix(p, "/")
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false
	}
	return clean, true
}

// CommitFileEdit writes content to path on branch and commits it directly to
// that branch (bypassing review), returning the new commit SHA. The author is
// recorded as the editing user.
func (s *Service) CommitFileEdit(project, branch, filePath string, content []byte, message, authorName, authorEmail string) (string, error) {
	if !s.ProjectExists(project) {
		return "", ErrProjectMissing
	}
	cleanPath, ok := safeFilePath(filePath)
	if !ok {
		return "", ErrInvalidRef
	}
	if !s.refExists(project, "refs/heads/"+branch) {
		return "", ErrRefMissing
	}
	if strings.TrimSpace(message) == "" {
		message = "Update " + cleanPath
	}
	bareDir := s.RepoDir(project)
	tmp, err := os.MkdirTemp("", "gerritgo-edit-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	work := filepath.Join(tmp, "repo")
	if _, err := s.runGit(tmp, "clone", "--quiet", "--branch", branch, bareDir, work); err != nil {
		return "", err
	}

	abs := filepath.Join(work, filepath.FromSlash(cleanPath))
	// Re-verify the resolved path stays inside the work tree.
	if !strings.HasPrefix(abs, filepath.Clean(work)+string(os.PathSeparator)) {
		return "", ErrInvalidRef
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return "", err
	}

	if _, err := s.runGit(work, "add", "--", cleanPath); err != nil {
		return "", err
	}
	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)
	if _, err := s.runGit(work, "commit", "--quiet", "--author="+author, "-m", message); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") || strings.Contains(err.Error(), "no changes added") {
			return "", ErrNoChanges
		}
		return "", err
	}
	sha, err := s.runGit(work, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if _, err := s.runGit(work, "push", "--quiet", "origin", sha+":refs/heads/"+branch); err != nil {
		return "", err
	}
	return sha, nil
}

// DeleteProject removes the bare repository directory from disk.
func (s *Service) DeleteProject(project string) error {
	dir := s.RepoDir(project)
	if !s.ProjectExists(project) {
		return ErrProjectMissing
	}
	return os.RemoveAll(dir)
}
