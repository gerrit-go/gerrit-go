package gitsvc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keepBackups is how many repository backup archives are retained; older ones
// are pruned after each backup.
const keepBackups = 5

// backupsDir returns the directory holding backup archives, creating it.
func (s *Service) backupsDir() (string, error) {
	dir := filepath.Join(filepath.Dir(s.basePath), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// BackupRepos archives all git repositories (the base path) into a timestamped
// .tar.gz under <data>/backups and prunes archives beyond the retention count.
// It returns the archive path.
func (s *Service) BackupRepos() (string, error) {
	dir, err := s.backupsDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("repos-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	dest := filepath.Join(dir, name)
	cmd := exec.Command("tar", "-czf", dest, "-C", filepath.Dir(s.basePath), filepath.Base(s.basePath))
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("tar: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if err := s.pruneBackups(dir); err != nil {
		return dest, err
	}
	return dest, nil
}

// pruneBackups removes the oldest repo archives beyond keepBackups.
func (s *Service) pruneBackups(dir string) error {
	archives, err := s.listBackupFiles(dir)
	if err != nil {
		return err
	}
	for len(archives) > keepBackups {
		if err := os.Remove(archives[0]); err != nil {
			return err
		}
		archives = archives[1:]
	}
	return nil
}

// listBackupFiles returns the repo-*.tar.gz archives in dir, oldest first.
func (s *Service) listBackupFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "repos-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // timestamped names sort chronologically
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(dir, n)
	}
	return out, nil
}

// BackupInfo describes one backup archive.
type BackupInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}

// ListBackups returns the existing backup archives, newest first.
func (s *Service) ListBackups() ([]*BackupInfo, error) {
	dir, err := s.backupsDir()
	if err != nil {
		return nil, err
	}
	archives, err := s.listBackupFiles(dir)
	if err != nil {
		return nil, err
	}
	var out []*BackupInfo
	for i := len(archives) - 1; i >= 0; i-- {
		fi, err := os.Stat(archives[i])
		if err != nil {
			continue
		}
		out = append(out, &BackupInfo{
			Name:    filepath.Base(archives[i]),
			Size:    fi.Size(),
			Created: fi.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}
