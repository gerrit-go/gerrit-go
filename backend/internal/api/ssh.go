package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"

	"golang.org/x/crypto/ssh"
)

// sshVersion is reported by `gerrit version`.
const sshVersion = "gerrit-go SSH 0.1.0"

// StartSSH runs the git+ssh / Gerrit SSH daemon on addr (e.g. ":29418"). An
// empty addr disables the listener. It blocks serving connections and returns
// the terminal listener error, if any.
func (s *Server) StartSSH(addr string) error {
	if addr == "" {
		return nil
	}
	s.sshAddr = addr
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return s.sshAuth(conn, key)
		},
	}
	signer, err := sshHostSigner()
	if err != nil {
		return fmt.Errorf("ssh host key: %w", err)
	}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "gerrit-go ssh listening on %s\n", addr)
	for {
		nConn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.serveSSHConn(nConn, config)
	}
}

// sshAuth authenticates a connection by matching the presented public key
// against registered account keys and verifying the username matches.
func (s *Server) sshAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	normalized := key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
	acct, err := s.db.FindAccountBySSHPublicKey(normalized)
	if err != nil {
		return nil, fmt.Errorf("public key rejected")
	}
	if !strings.EqualFold(acct.Username, conn.User()) {
		return nil, fmt.Errorf("username does not match the registered key")
	}
	return &ssh.Permissions{
		Extensions: map[string]string{"account_id": strconv.FormatInt(acct.ID, 10)},
	}, nil
}

func sshHostSigner() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromSigner(priv)
}

func (s *Server) serveSSHConn(nConn net.Conn, config *ssh.ServerConfig) {
	defer nConn.Close()
	conn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)

	acct := s.sshAccount(conn)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			continue
		}
		go s.sshSession(ch, requests, acct)
	}
}

// sshAccount recovers the authenticated account from the connection permissions.
func (s *Server) sshAccount(conn *ssh.ServerConn) *store.Account {
	if conn.Permissions == nil {
		return nil
	}
	idStr := conn.Permissions.Extensions["account_id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil
	}
	acct, err := s.db.GetAccount(id)
	if err != nil {
		return nil
	}
	return acct
}

func (s *Server) sshSession(ch ssh.Channel, requests <-chan *ssh.Request, acct *store.Account) {
	defer ch.Close()
	for req := range requests {
		switch req.Type {
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				req.Reply(false, nil)
				continue
			}
			req.Reply(true, nil)
			status := s.dispatchSSH(ch, acct, payload.Command)
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
			return
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// dispatchSSH routes a single SSH command and returns its exit status.
func (s *Server) dispatchSSH(ch ssh.Channel, acct *store.Account, command string) int {
	args := tokenizeSSHCommand(command)
	if len(args) == 0 {
		return 0
	}
	switch args[0] {
	case "git-upload-pack", "git-receive-pack":
		return s.sshGit(ch, acct, args)
	case "gerrit":
		return s.sshGerrit(ch, acct, args[1:])
	case "version":
		fmt.Fprintln(ch, sshVersion)
		return 0
	default:
		fmt.Fprintf(ch.Stderr(), "%s: command not found\n", args[0])
		return 127
	}
}

// sshProjectFromArg extracts a project name from a git command argument such as
// "'/my/proj.git'" or "/my/proj.git".
func sshProjectFromArg(arg string) string {
	p := strings.Trim(arg, "'\"")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimSuffix(p, ".git")
	if p == "" || strings.Contains(p, "..") {
		return ""
	}
	return p
}

// sshGit runs git-upload-pack / git-receive-pack against a project repository,
// enforcing the same read/push gates as the smart-HTTP transport.
func (s *Server) sshGit(ch ssh.Channel, acct *store.Account, args []string) int {
	if acct == nil {
		fmt.Fprintln(ch.Stderr(), "authentication required")
		return 1
	}
	if len(args) < 2 {
		fmt.Fprintln(ch.Stderr(), "missing repository argument")
		return 1
	}
	service := args[0]
	project := sshProjectFromArg(args[1])
	if project == "" || !s.git.ProjectExists(project) {
		fmt.Fprintln(ch.Stderr(), "repository not found")
		return 1
	}

	isPush := service == "git-receive-pack"
	if isPush {
		if !s.can(acct, project, "refs/for/master", PermPush) {
			fmt.Fprintln(ch.Stderr(), "push not permitted")
			return 1
		}
	} else if !s.canReadProject(acct, project) {
		fmt.Fprintln(ch.Stderr(), "repository not found")
		return 1
	}

	binary := "upload-pack"
	if isPush {
		binary = "receive-pack"
	}
	cmd := exec.Command("git", binary, s.git.RepoDir(project))
	cmd.Stdin = ch
	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()
	runErr := cmd.Run()
	if runErr != nil {
		// upload/receive-pack exit non-zero on benign client disconnects; the
		// transport has already streamed any error to the client.
		fmt.Fprintf(os.Stderr, "ssh %s (%s): %v\n", binary, project, runErr)
	}
	if isPush && runErr == nil {
		if err := s.git.ProcessReceivePack(project, acct); err != nil {
			fmt.Fprintf(os.Stderr, "ssh post-receive processing for %s: %v\n", project, err)
		}
	}
	if runErr != nil {
		return 1
	}
	return 0
}

// sshGerrit implements the minimal Gerrit SSH command set.
func (s *Server) sshGerrit(ch ssh.Channel, acct *store.Account, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(ch.Stderr(), "gerrit: missing subcommand")
		return 1
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(ch, sshVersion)
		return 0
	case "query":
		return s.sshGerritQuery(ch, acct, args[1:])
	case "review":
		return s.sshGerritReview(ch, acct, args[1:])
	case "submit":
		return s.sshGerritSubmit(ch, acct, args[1:])
	case "stream-events":
		return s.sshGerritStreamEvents(ch, acct, args[1:])
	case "ls-projects":
		return s.sshLsProjects(ch, acct)
	case "ls-groups":
		return s.sshLsGroups(ch, acct)
	case "ls-members":
		return s.sshLsMembers(ch, acct, args[1:])
	case "set-reviewers":
		return s.sshSetReviewers(ch, acct, args[1:])
	case "create-project":
		return s.sshCreateProject(ch, acct, args[1:])
	case "create-branch":
		return s.sshCreateBranch(ch, acct, args[1:])
	case "create-tag":
		return s.sshCreateTag(ch, acct, args[1:])
	case "delete-project":
		return s.sshDeleteProject(ch, acct, args[1:])
	case "delete-branch":
		return s.sshDeleteBranch(ch, acct, args[1:])
	case "delete-tag":
		return s.sshDeleteTag(ch, acct, args[1:])
	case "set-project":
		return s.sshSetProject(ch, acct, args[1:])
	default:
		fmt.Fprintf(ch.Stderr(), "gerrit: unknown subcommand %q\n", args[0])
		return 1
	}
}

func (s *Server) sshGerritQuery(ch ssh.Channel, acct *store.Account, args []string) int {
	limit := 0
	var terms []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--limit" && i+1 < len(args):
			i++
			limit, _ = strconv.Atoi(args[i])
		case strings.HasPrefix(a, "--limit="):
			limit, _ = strconv.Atoi(strings.TrimPrefix(a, "--limit="))
		case strings.HasPrefix(a, "--"):
			// Ignore other presentation flags (format, current-patch-set, …).
		default:
			terms = append(terms, a)
		}
	}
	q := strings.Join(terms, " ")
	root := store.ParseQuery(q, acct)
	changes, total, err := s.db.SearchChangesParsed(root, limit, 0)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "query failed: %v\n", err)
		return 1
	}
	for _, c := range changes {
		if !s.canReadChange(acct, c) {
			continue
		}
		info := changeInfo(c)
		info["number"] = c.Number
		if ps, err := s.db.GetPatchSet(c.Number, c.CurrentPS); err == nil {
			info["currentPatchSet"] = map[string]any{"number": c.CurrentPS, "commit": ps.CommitSHA}
		}
		b, _ := json.Marshal(info)
		fmt.Fprintln(ch, string(b))
	}
	stats, _ := json.Marshal(map[string]any{"type": "stats", "rowcount": total})
	fmt.Fprintln(ch, string(stats))
	return 0
}

func (s *Server) sshGerritSubmit(ch ssh.Channel, acct *store.Account, args []string) int {
	if acct == nil {
		fmt.Fprintln(ch.Stderr(), "authentication required")
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(ch.Stderr(), "submit: missing change number")
		return 1
	}
	num, ok := sshParseChangeNum(args[0])
	if !ok {
		fmt.Fprintf(ch.Stderr(), "submit: invalid change %q\n", args[0])
		return 1
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "change not found")
		return 1
	}
	if !s.canReadChange(acct, c) {
		fmt.Fprintln(ch.Stderr(), "change not found")
		return 1
	}
	if c.Status != "NEW" {
		fmt.Fprintln(ch.Stderr(), "change is not open")
		return 1
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermSubmit) {
		fmt.Fprintln(ch.Stderr(), "submit not permitted")
		return 1
	}
	commit, submitted, err := s.submitChangeChain(acct, "en", c)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "submit failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(ch, "submitted [%s] (%s)\n", joinInt64s(submitted), shortSHA(commit))
	return 0
}

func (s *Server) sshGerritReview(ch ssh.Channel, acct *store.Account, args []string) int {
	if acct == nil {
		fmt.Fprintln(ch.Stderr(), "authentication required")
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(ch.Stderr(), "review: missing change number")
		return 1
	}
	num, ok := sshParseChangeNum(args[0])
	if !ok {
		fmt.Fprintf(ch.Stderr(), "review: invalid change %q\n", args[0])
		return 1
	}
	labels := map[string]int{}
	message := ""
	for i := 1; i < len(args); i++ {
		switch a := args[i]; a {
		case "--label":
			if i+1 < len(args) {
				i++
				if l, v, ok := parseLabelArg(args[i]); ok {
					labels[l] = v
				}
			}
		case "--message":
			if i+1 < len(args) {
				i++
				message = args[i]
			}
		default:
			if strings.HasPrefix(a, "--label=") {
				if l, v, ok := parseLabelArg(strings.TrimPrefix(a, "--label=")); ok {
					labels[l] = v
				}
			} else if strings.HasPrefix(a, "--message=") {
				message = strings.TrimPrefix(a, "--message=")
			}
		}
	}

	c, err := s.db.GetChange(num)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "change not found")
		return 1
	}
	if !s.canReadChange(acct, c) {
		fmt.Fprintln(ch.Stderr(), "change not found")
		return 1
	}
	ref := branchRef(c.Branch)
	if strings.TrimSpace(message) != "" && !s.can(acct, c.Project, ref, PermComment) {
		fmt.Fprintln(ch.Stderr(), "comment not permitted")
		return 1
	}

	s.db.AddReviewer(c.Number, acct.ID)
	for label, value := range labels {
		acc := s.checkAccess(acct, c.Project, ref, "label-"+label)
		if !acc.allowed {
			fmt.Fprintf(ch.Stderr(), "label %s not permitted\n", label)
			return 1
		}
		if value < acc.min || value > acc.max {
			fmt.Fprintf(ch.Stderr(), "%s value %d out of range [%d, %d]\n", label, value, acc.min, acc.max)
			return 1
		}
		v := &store.Vote{ChangeNumber: c.Number, PatchSet: c.CurrentPS, AccountID: acct.ID, Label: label, Value: value}
		if value == 0 {
			if err := s.db.DeleteVote(v.ChangeNumber, v.PatchSet, v.AccountID, v.Label); err != nil {
				fmt.Fprintf(ch.Stderr(), "%v\n", err)
				return 1
			}
			continue
		}
		if err := s.db.SetVote(v); err != nil {
			fmt.Fprintf(ch.Stderr(), "%v\n", err)
			return 1
		}
	}
	if strings.TrimSpace(message) != "" {
		s.db.AddChangeMessage(&store.ChangeMessage{
			ChangeNum: c.Number, PatchSet: c.CurrentPS, Type: "comment",
			AuthorID: acct.ID, Message: strings.TrimSpace(message),
		})
	}
	s.db.TouchChange(c.Number)
	evType := "review"
	if len(labels) == 0 {
		evType = "comment"
	}
	s.notifyChange(c, acct.ID, notify.Event{
		Type:             evType,
		Message:          strings.TrimSpace(message),
		NotifyOwner:      true,
		IncludeReviewers: true,
	})
	fmt.Fprintf(ch, "reviewed change %d\n", c.Number)
	return 0
}

// joinInt64s formats a slice of change numbers as a comma-separated string.
func joinInt64s(ns []int64) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.FormatInt(n, 10)
	}
	return strings.Join(parts, ", ")
}

// parseLabelArg parses "Code-Review=+2" into label and value.
func parseLabelArg(s string) (string, int, bool) {
	i := strings.LastIndex(s, "=")
	if i <= 0 {
		return "", 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(s[i+1:]))
	if err != nil {
		return "", 0, false
	}
	return s[:i], v, true
}

// sshParseChangeNum accepts "123" or "123,2" (change,patch-set), returning the
// change number; the patch set is ignored because review targets the current one.
func sshParseChangeNum(s string) (int64, bool) {
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// tokenizeSSHCommand splits an SSH exec command into arguments, honouring single
// and double quotes the way a POSIX shell would (without expansion).
func tokenizeSSHCommand(cmd string) []string {
	var args []string
	var cur strings.Builder
	inSingle, inDouble, hasToken := false, false, false
	for _, r := range cmd {
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			} else {
				cur.WriteRune(r)
			}
		case inDouble:
			if r == '"' {
				inDouble = false
			} else {
				cur.WriteRune(r)
			}
		case r == '\'':
			inSingle = true
			hasToken = true
		case r == '"':
			inDouble = true
			hasToken = true
		case r == ' ' || r == '\t':
			if hasToken {
				args = append(args, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if hasToken {
		args = append(args, cur.String())
	}
	return args
}
