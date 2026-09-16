package gitsvc

import (
	"errors"
	"path"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
)

// maxOwnersFileSize caps how much of an OWNERS file is parsed, guarding
// against pathological blobs.
const maxOwnersFileSize = 64 << 10

// ParseOwners parses an OWNERS file body into a list of usernames/emails.
// Blank lines and '#' comments are skipped; 'per-file' directives are not
// evaluated in this increment and are skipped entirely.
func ParseOwners(data []byte) []string {
	if len(data) > maxOwnersFileSize {
		data = data[:maxOwnersFileSize]
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "per-file") {
			continue
		}
		// Strip an inline comment.
		if i := strings.Index(line, " #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// OwnersForPaths returns the ordered, deduped OWNERS entries governing the
// given file paths, reading OWNERS files from each path's directory up to the
// repo root (deepest first). rev resolves via resolveCommit (empty means
// HEAD/first-branch). Returns an empty slice when no OWNERS files exist.
func (s *Service) OwnersForPaths(project, rev string, paths []string) ([]string, error) {
	repo, err := s.OpenRepo(project)
	if err != nil {
		return nil, err
	}
	commit, err := s.resolveCommit(repo, rev)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	// Collect the directories to probe, deepest first, deduped across paths.
	var dirs []string
	seenDir := map[string]bool{}
	for _, p := range paths {
		for d := path.Dir(p); ; d = path.Dir(d) {
			if !seenDir[d] {
				seenDir[d] = true
				dirs = append(dirs, d)
			}
			if d == "." || d == "/" {
				break
			}
		}
	}
	// Sort so deeper (more specific) directories come before their ancestors.
	// A simple length-descending sort approximates depth ordering.
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			if len(dirs[j]) > len(dirs[i]) {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
		}
	}

	var out []string
	seenEntry := map[string]bool{}
	for _, d := range dirs {
		ownersPath := "OWNERS"
		if d != "." && d != "/" {
			ownersPath = d + "/OWNERS"
		}
		f, err := tree.File(ownersPath)
		if err != nil {
			if !errors.Is(err, object.ErrFileNotFound) {
				continue
			}
			continue
		}
		if f.Size > maxOwnersFileSize {
			continue
		}
		contents, err := f.Contents()
		if err != nil {
			continue
		}
		for _, e := range ParseOwners([]byte(contents)) {
			key := strings.ToLower(e)
			if !seenEntry[key] {
				seenEntry[key] = true
				out = append(out, e)
			}
		}
	}
	return out, nil
}
