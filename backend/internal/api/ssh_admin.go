package api

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	"gerrit-go/internal/store"
)

// sshRequireAuth writes an error and returns false when the caller is not
// authenticated.
func sshRequireAuth(ch ssh.Channel, acct *store.Account) bool {
	if acct == nil {
		fmt.Fprintln(ch.Stderr(), "authentication required")
		return false
	}
	return true
}

// sshLsProjects lists the projects the caller may read, one per line.
func (s *Server) sshLsProjects(ch ssh.Channel, acct *store.Account) int {
	list, err := s.db.ListProjects()
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "ls-projects failed: %v\n", err)
		return 1
	}
	for _, p := range list {
		if !s.canReadProject(acct, p.Name) {
			continue
		}
		fmt.Fprintln(ch, p.Name)
	}
	return 0
}

// sshLsGroups lists all groups (name and id). Requires authentication.
func (s *Server) sshLsGroups(ch ssh.Channel, acct *store.Account) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	groups, err := s.db.ListGroups()
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "ls-groups failed: %v\n", err)
		return 1
	}
	for _, g := range groups {
		fmt.Fprintf(ch, "%d\t%s\n", g.ID, g.Name)
	}
	return 0
}

// sshLsMembers lists the members of a group: `gerrit ls-members <group>`.
func (s *Server) sshLsMembers(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(ch.Stderr(), "ls-members: missing group name")
		return 1
	}
	g, err := s.db.GetGroupByName(strings.Join(args, " "))
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "group not found")
		return 1
	}
	members, err := s.db.ListGroupMembers(g.ID)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "ls-members failed: %v\n", err)
		return 1
	}
	for _, m := range members {
		fmt.Fprintf(ch, "%s\t%s\t%s\n", m.Username, m.FullName, m.Email)
	}
	return 0
}

// sshSetReviewers adds/removes reviewers on a change:
// `gerrit set-reviewers [--add user]... [--remove user]... <change>`.
func (s *Server) sshSetReviewers(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	var toAdd, toRemove []string
	changeArg := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case (a == "--add" || a == "-a") && i+1 < len(args):
			i++
			toAdd = append(toAdd, args[i])
		case (a == "--remove" || a == "-r") && i+1 < len(args):
			i++
			toRemove = append(toRemove, args[i])
		case strings.HasPrefix(a, "--add="):
			toAdd = append(toAdd, strings.TrimPrefix(a, "--add="))
		case strings.HasPrefix(a, "--remove="):
			toRemove = append(toRemove, strings.TrimPrefix(a, "--remove="))
		case !strings.HasPrefix(a, "-"):
			changeArg = a
		}
	}
	if changeArg == "" {
		fmt.Fprintln(ch.Stderr(), "set-reviewers: missing change number")
		return 1
	}
	num, ok := sshParseChangeNum(changeArg)
	if !ok {
		fmt.Fprintf(ch.Stderr(), "set-reviewers: invalid change %q\n", changeArg)
		return 1
	}
	c, err := s.db.GetChange(num)
	if err != nil || !s.canReadChange(acct, c) {
		fmt.Fprintln(ch.Stderr(), "change not found")
		return 1
	}
	if c.OwnerID != acct.ID && !s.can(acct, c.Project, branchRef(c.Branch), PermComment) {
		fmt.Fprintln(ch.Stderr(), "not permitted")
		return 1
	}
	for _, ident := range toAdd {
		target, err := s.resolveAccount(ident)
		if err != nil {
			fmt.Fprintf(ch.Stderr(), "account %q not found\n", ident)
			continue
		}
		if err := s.db.AddReviewer(c.Number, target.ID); err == nil {
			fmt.Fprintf(ch, "added reviewer %s\n", target.Username)
		}
	}
	for _, ident := range toRemove {
		target, err := s.resolveAccount(ident)
		if err != nil {
			fmt.Fprintf(ch.Stderr(), "account %q not found\n", ident)
			continue
		}
		if err := s.db.RemoveReviewer(c.Number, target.ID); err == nil {
			fmt.Fprintf(ch, "removed reviewer %s\n", target.Username)
		}
	}
	return 0
}

// sshCreateProject creates a project: `gerrit create-project <name> [--description d] [--parent p]`.
func (s *Server) sshCreateProject(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if !s.canCapability(acct, PermCreateProject) {
		fmt.Fprintln(ch.Stderr(), "createProject not permitted")
		return 1
	}
	var name, desc, parent string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--description" && i+1 < len(args):
			i++
			desc = args[i]
		case a == "--parent" && i+1 < len(args):
			i++
			parent = args[i]
		case strings.HasPrefix(a, "--description="):
			desc = strings.TrimPrefix(a, "--description=")
		case strings.HasPrefix(a, "--parent="):
			parent = strings.TrimPrefix(a, "--parent=")
		case !strings.HasPrefix(a, "-"):
			name = a
		}
	}
	if name == "" {
		fmt.Fprintln(ch.Stderr(), "create-project: missing name")
		return 1
	}
	name = strings.TrimSuffix(name, ".git")
	if err := s.validateParent(name, parent); err != nil {
		fmt.Fprintf(ch.Stderr(), "create-project: %v\n", err)
		return 1
	}
	if err := s.git.CreateProject(name, desc); err != nil {
		fmt.Fprintf(ch.Stderr(), "create-project failed: %v\n", err)
		return 1
	}
	if parent != "" {
		if err := s.db.SetProjectParent(name, parent); err != nil {
			fmt.Fprintf(ch.Stderr(), "create-project: set parent: %v\n", err)
			return 1
		}
	}
	s.seedProjectAccess(name, acct)
	fmt.Fprintf(ch, "created project %s\n", name)
	return 0
}

// sshCreateBranch creates a branch: `gerrit create-branch <project> <branch> [revision]`.
func (s *Server) sshCreateBranch(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if len(args) < 2 {
		fmt.Fprintln(ch.Stderr(), "create-branch: usage: create-branch <project> <branch> [revision]")
		return 1
	}
	project, branch := args[0], args[1]
	start := ""
	if len(args) > 2 {
		start = args[2]
	}
	if !s.can(acct, project, "refs/heads/"+branch, PermPush) {
		fmt.Fprintln(ch.Stderr(), "create-branch not permitted")
		return 1
	}
	sha, err := s.git.CreateBranch(project, branch, start)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "create-branch failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(ch, "created branch %s in %s (%s)\n", branch, project, shortSHA(sha))
	return 0
}

// sshCreateTag creates a tag: `gerrit create-tag <project> <tag> [revision]`.
func (s *Server) sshCreateTag(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if len(args) < 2 {
		fmt.Fprintln(ch.Stderr(), "create-tag: usage: create-tag <project> <tag> [revision]")
		return 1
	}
	project, tag := args[0], args[1]
	start := ""
	if len(args) > 2 {
		start = args[2]
	}
	if !s.can(acct, project, "refs/tags/"+tag, PermPush) {
		fmt.Fprintln(ch.Stderr(), "create-tag not permitted")
		return 1
	}
	sha, err := s.git.CreateTag(project, tag, start, "")
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "create-tag failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(ch, "created tag %s in %s (%s)\n", tag, project, shortSHA(sha))
	return 0
}

// sshDeleteProject deletes a project (admin only): `gerrit delete-project <name>`.
func (s *Server) sshDeleteProject(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if !acct.Admin {
		fmt.Fprintln(ch.Stderr(), "admin only")
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(ch.Stderr(), "delete-project: missing name")
		return 1
	}
	name := strings.TrimSuffix(args[0], ".git")
	if err := s.git.DeleteProject(name); err != nil {
		fmt.Fprintf(ch.Stderr(), "delete-project failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(ch, "deleted project %s\n", name)
	return 0
}

// sshDeleteBranch deletes a branch: `gerrit delete-branch <project> <branch>`.
func (s *Server) sshDeleteBranch(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if len(args) < 2 {
		fmt.Fprintln(ch.Stderr(), "delete-branch: usage: delete-branch <project> <branch>")
		return 1
	}
	project, branch := args[0], args[1]
	if !s.can(acct, project, "refs/heads/"+branch, PermPush) {
		fmt.Fprintln(ch.Stderr(), "delete-branch not permitted")
		return 1
	}
	if err := s.git.DeleteBranch(project, branch); err != nil {
		fmt.Fprintf(ch.Stderr(), "delete-branch failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(ch, "deleted branch %s in %s\n", branch, project)
	return 0
}

// sshDeleteTag deletes a tag: `gerrit delete-tag <project> <tag>`.
func (s *Server) sshDeleteTag(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	if len(args) < 2 {
		fmt.Fprintln(ch.Stderr(), "delete-tag: usage: delete-tag <project> <tag>")
		return 1
	}
	project, tag := args[0], args[1]
	if !s.can(acct, project, "refs/tags/"+tag, PermPush) {
		fmt.Fprintln(ch.Stderr(), "delete-tag not permitted")
		return 1
	}
	if err := s.git.DeleteTag(project, tag); err != nil {
		fmt.Fprintf(ch.Stderr(), "delete-tag failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(ch, "deleted tag %s in %s\n", tag, project)
	return 0
}

// sshSetProject updates project config: `gerrit set-project <name> [--description d] [--parent p] [--state ACTIVE|READ_ONLY|HIDDEN]`.
func (s *Server) sshSetProject(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	var name, desc, parent, state string
	hasDesc, hasParent, hasState := false, false, false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--description" && i+1 < len(args):
			i++
			desc, hasDesc = args[i], true
		case a == "--parent" && i+1 < len(args):
			i++
			parent, hasParent = args[i], true
		case a == "--state" && i+1 < len(args):
			i++
			state, hasState = args[i], true
		case strings.HasPrefix(a, "--description="):
			desc, hasDesc = strings.TrimPrefix(a, "--description="), true
		case strings.HasPrefix(a, "--parent="):
			parent, hasParent = strings.TrimPrefix(a, "--parent="), true
		case strings.HasPrefix(a, "--state="):
			state, hasState = strings.TrimPrefix(a, "--state="), true
		case !strings.HasPrefix(a, "-"):
			name = a
		}
	}
	if name == "" {
		fmt.Fprintln(ch.Stderr(), "set-project: missing name")
		return 1
	}
	if _, err := s.db.GetProject(name); err != nil {
		fmt.Fprintln(ch.Stderr(), "project not found")
		return 1
	}
	if !s.canEditAccess(acct, name) {
		fmt.Fprintln(ch.Stderr(), "editAccess not permitted")
		return 1
	}
	if hasDesc {
		if err := s.db.SetProjectDescription(name, desc); err != nil {
			fmt.Fprintf(ch.Stderr(), "set-project failed: %v\n", err)
			return 1
		}
	}
	if hasParent {
		if err := s.validateParent(name, parent); err != nil {
			fmt.Fprintf(ch.Stderr(), "set-project: %v\n", err)
			return 1
		}
		if err := s.db.SetProjectParent(name, parent); err != nil {
			fmt.Fprintf(ch.Stderr(), "set-project failed: %v\n", err)
			return 1
		}
	}
	if hasState {
		if err := s.db.SetProjectState(name, state); err != nil {
			fmt.Fprintf(ch.Stderr(), "set-project failed: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(ch, "updated project %s\n", name)
	return 0
}

// sshSetLabel sets or deletes labels on projects.
// Usage:
//   gerrit set-label <project> key=value [key2=value2...]
//   gerrit set-label <project> --delete key [key2...]
//   gerrit set-label --pattern <glob> key=value [key2=value2...]
func (s *Server) sshSetLabel(ch ssh.Channel, acct *store.Account, args []string) int {
	if !sshRequireAuth(ch, acct) {
		return 1
	}
	var pattern, project string
	var sets []string
	var deletes []string
	deleteMode := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--pattern" && i+1 < len(args):
			i++
			pattern = args[i]
		case strings.HasPrefix(a, "--pattern="):
			pattern = strings.TrimPrefix(a, "--pattern=")
		case a == "--delete":
			deleteMode = true
		case !strings.HasPrefix(a, "-") && project == "" && pattern == "":
			project = a
		case !strings.HasPrefix(a, "-"):
			if deleteMode {
				deletes = append(deletes, a)
			} else {
				sets = append(sets, a)
			}
		}
	}

	// Resolve target projects.
	var targets []string
	if pattern != "" {
		list, err := s.db.ListProjects()
		if err != nil {
			fmt.Fprintf(ch.Stderr(), "set-label failed: %v\n", err)
			return 1
		}
		for _, p := range list {
			if matchGlob(pattern, p.Name) {
				targets = append(targets, p.Name)
			}
		}
		if len(targets) == 0 {
			fmt.Fprintf(ch.Stderr(), "no projects match pattern %q\n", pattern)
			return 1
		}
	} else if project != "" {
		if _, err := s.db.GetProject(project); err != nil {
			fmt.Fprintln(ch.Stderr(), "project not found")
			return 1
		}
		targets = []string{project}
	} else {
		fmt.Fprintln(ch.Stderr(), "usage: gerrit set-label <project>|--pattern <glob> key=value [...] | --delete key [...]")
		return 1
	}

	// Check permission on first target (admin or project owner).
	if !s.canEditAccess(acct, targets[0]) {
		fmt.Fprintln(ch.Stderr(), "editAccess not permitted")
		return 1
	}

	updated := 0
	for _, name := range targets {
		for _, kv := range sets {
			key, value, _ := strings.Cut(kv, "=")
			if key == "" {
				continue
			}
			if err := s.db.SetProjectLabel(name, key, value); err != nil {
				fmt.Fprintf(ch.Stderr(), "set-label %s on %s failed: %v\n", key, name, err)
				return 1
			}
		}
		for _, key := range deletes {
			if err := s.db.DeleteProjectLabel(name, key); err != nil {
				fmt.Fprintf(ch.Stderr(), "delete-label %s on %s failed: %v\n", key, name, err)
				return 1
			}
		}
		updated++
	}
	fmt.Fprintf(ch, "updated %d project(s)\n", updated)
	return 0
}

// matchGlob does simple glob matching: * matches any sequence, ? matches one char.
func matchGlob(pattern, name string) bool {
	return globMatch(pattern, name)
}

func globMatch(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}
	if pattern == "*" {
		return true
	}
	// Find the first literal segment.
	i := strings.IndexAny(pattern, "*?")
	if i < 0 {
		return pattern == name
	}
	// Match prefix literally.
	if i > 0 {
		if len(name) < i || pattern[:i] != name[:i] {
			return false
		}
		pattern = pattern[i:]
		name = name[i:]
	}
	switch pattern[0] {
	case '*':
		// Try matching rest at every position.
		for j := 0; j <= len(name); j++ {
			if globMatch(pattern[1:], name[j:]) {
				return true
			}
		}
		return false
	case '?':
		if len(name) == 0 {
			return false
		}
		return globMatch(pattern[1:], name[1:])
	}
	return false
}
