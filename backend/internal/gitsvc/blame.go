package gitsvc

import (
	"strconv"
	"strings"
)

// BlameLine describes one line of a file's blame annotation: which commit last
// touched it and who authored that commit.
type BlameLine struct {
	Line    int    `json:"line"`
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	When    int64  `json:"when"` // unix seconds
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

// Blame runs `git blame --porcelain` for path at rev (HEAD when empty) and
// returns one entry per line. Porcelain output carries full commit metadata in
// a header block the first time a commit appears, then compact per-line refs.
func (s *Service) Blame(project, rev, path string) ([]*BlameLine, error) {
	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	cleanPath, ok := safeFilePath(path)
	if !ok {
		return nil, ErrInvalidRef
	}
	commit, err := s.resolveCommitForRead(project, rev)
	if err != nil {
		return nil, err
	}
	out, err := s.runGitRaw(s.RepoDir(project), "blame", "--porcelain", commit, "--", cleanPath)
	if err != nil {
		return nil, err
	}
	return parseBlamePorcelain(out), nil
}

// parseBlamePorcelain parses `git blame --porcelain` output. Each line group
// starts with "<sha> <orig> <final> [<count>]"; the first group for a sha is
// followed by author/summary/etc. header lines; a "\t<text>" line terminates
// each group.
func parseBlamePorcelain(out string) []*BlameLine {
	type meta struct{ author, email, summary string; when int64 }
	commits := map[string]meta{}
	var lines []*BlameLine
	var cur *BlameLine
	for _, raw := range strings.Split(out, "\n") {
		if strings.HasPrefix(raw, "\t") {
			if cur != nil {
				cur.Text = raw[1:]
				lines = append(lines, cur)
				cur = nil
			}
			continue
		}
		fields := strings.Fields(raw)
		// A group header has 3-4 numeric-ish fields and a 40-hex sha.
		if len(fields) >= 3 && len(fields[0]) == 40 && isHex(fields[0]) {
			sha := fields[0]
			finalLine, _ := strconv.Atoi(fields[2])
			m := commits[sha]
			cur = &BlameLine{Line: finalLine, SHA: sha, Author: m.author, Email: m.email, When: m.when, Summary: m.summary}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "author "):
			cur.Author = strings.TrimPrefix(raw, "author ")
		case strings.HasPrefix(raw, "author-mail "):
			cur.Email = strings.Trim(strings.TrimPrefix(raw, "author-mail "), "<>")
		case strings.HasPrefix(raw, "author-time "):
			cur.When, _ = strconv.ParseInt(strings.TrimPrefix(raw, "author-time "), 10, 64)
		case strings.HasPrefix(raw, "summary "):
			cur.Summary = strings.TrimPrefix(raw, "summary ")
		}
		commits[cur.SHA] = meta{cur.Author, cur.Email, cur.Summary, cur.When}
	}
	return lines
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// FileLogEntry is one commit that touched a file.
type FileLogEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	When    int64  `json:"when"`
	Subject string `json:"subject"`
}

// FileLog lists commits that touched path, newest first, at rev (HEAD when
// empty), capped at limit.
func (s *Service) FileLog(project, rev, path string, limit int) ([]*FileLogEntry, error) {
	if !s.ProjectExists(project) {
		return nil, ErrProjectMissing
	}
	cleanPath, ok := safeFilePath(path)
	if !ok {
		return nil, ErrInvalidRef
	}
	commit, err := s.resolveCommitForRead(project, rev)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out, err := s.runGitRaw(s.RepoDir(project), "log", "--format=%H%x1f%an%x1f%ae%x1f%at%x1f%s", "-n", strconv.Itoa(limit), commit, "--", cleanPath)
	if err != nil {
		return nil, err
	}
	var entries []*FileLogEntry
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 5)
		if len(parts) < 5 {
			continue
		}
		when, _ := strconv.ParseInt(parts[3], 10, 64)
		entries = append(entries, &FileLogEntry{SHA: parts[0], Author: parts[1], Email: parts[2], When: when, Subject: parts[4]})
	}
	return entries, nil
}

// resolveCommitForRead resolves rev (or HEAD/first-branch when empty) to a
// commit sha for read-only operations.
func (s *Service) resolveCommitForRead(project, rev string) (string, error) {
	if rev != "" {
		return s.resolveRef(project, rev)
	}
	repo, err := s.OpenRepo(project)
	if err != nil {
		return "", err
	}
	h, err := headOrFirstBranch(repo)
	if err != nil {
		return "", err
	}
	return h.String(), nil
}
