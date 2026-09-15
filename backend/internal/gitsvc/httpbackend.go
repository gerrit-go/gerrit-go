package gitsvc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// GitHTTPBackend serves the git smart HTTP protocol by invoking the system
// `git http-backend` CGI program. Using the official git implementation keeps
// full protocol compatibility with any git client (v0/v2, chunked encoding,
// shallow clones, etc.), which a pure-Go reimplementation would struggle to
// match.
type GitHTTPBackend struct {
	root string // GIT_PROJECT_ROOT: directory containing <project>.git repos
}

func NewGitHTTPBackend(root string) *GitHTTPBackend {
	return &GitHTTPBackend{root: root}
}

// ServeHTTP handles one CGI request. The caller must have validated the
// project exists; projectPath is the repo directory name (e.g. "my/proj.git").
func (b *GitHTTPBackend) ServeHTTP(w http.ResponseWriter, r *http.Request, projectPath, pathInfo, remoteUser string) error {
	// Buffer the request body into a temp file so CONTENT_LENGTH is always
	// known (git clients may use chunked encoding for pushes).
	var bodyFile *os.File
	contentLength := 0
	if r.Body != nil && (r.Method == http.MethodPost) {
		f, err := os.CreateTemp("", "gerrit-go-body-*")
		if err != nil {
			return err
		}
		defer func() {
			f.Close()
			os.Remove(f.Name())
		}()
		n, err := io.Copy(f, io.LimitReader(r.Body, 2<<30))
		if err != nil {
			return err
		}
		contentLength = int(n)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		bodyFile = f
	}

	env := []string{
		"GIT_PROJECT_ROOT=" + b.root,
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO=" + pathInfo,
		"QUERY_STRING=" + r.URL.RawQuery,
		"REQUEST_METHOD=" + r.Method,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"CONTENT_LENGTH=" + strconv.Itoa(contentLength),
		"REMOTE_ADDR=" + clientAddr(r),
		"GIT_PROTOCOL=" + r.Header.Get("Git-Protocol"),
	}
	if remoteUser != "" {
		env = append(env, "REMOTE_USER="+remoteUser)
		env = append(env, "REMOTE_AUTHORITY=Basic")
	}
	_ = projectPath

	cmd := exec.Command("git", "http-backend")
	cmd.Dir = b.root
	cmd.Env = append(os.Environ(), env...)
	if bodyFile != nil {
		cmd.Stdin = bodyFile
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start git http-backend: %w", err)
	}

	if err := writeCGIResponse(w, stdout); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		if errBuf.Len() > 0 {
			return fmt.Errorf("git http-backend: %w (%s)", err, strings.TrimSpace(errBuf.String()))
		}
		// A non-zero exit after a successful response (e.g. client disconnect)
		// is not fatal.
	}
	return nil
}

// writeCGIResponse parses a CGI header block from r and streams the body.
func writeCGIResponse(w http.ResponseWriter, r io.Reader) error {
	br := bufio.NewReader(r)
	tp := textproto.NewReader(br)
	headers, err := tp.ReadMIMEHeader()
	if err != nil && len(headers) == 0 {
		return err
	}

	status := http.StatusOK
	if s := headers.Get("Status"); s != "" {
		if code, convErr := strconv.Atoi(strings.Fields(s)[0]); convErr == nil {
			status = code
		}
	}
	for name, values := range headers {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if canonical == "Status" {
			continue
		}
		for _, v := range values {
			w.Header().Add(canonical, v)
		}
	}
	w.WriteHeader(status)
	_, err = io.Copy(w, br)
	return err
}

func clientAddr(r *http.Request) string {
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// EnableReceivePack writes the repo config flag that makes git http-backend
// accept pushes over HTTP.
func EnableReceivePack(repoDir string) error {
	cfgPath := filepath.Join(repoDir, "config")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	if bytes.Contains(data, []byte("receivepack = true")) {
		return nil
	}
	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("[http]\n\treceivepack = true\n")
	return err
}
