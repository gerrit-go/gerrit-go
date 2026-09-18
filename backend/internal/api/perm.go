package api

import (
	"regexp"
	"strings"
	"sync"

	"gerrit-go/internal/store"
)

// Permission names mirror Gerrit's access-control vocabulary.
const (
	PermRead          = "read"
	PermPush          = "push"
	PermSubmit        = "submit"
	PermAbandon       = "abandon"
	PermComment       = "comment"
	PermEditTopic     = "editTopicName"
	PermAddReviewer   = "addReviewer"
	PermCreateProject = "createProject"
	PermEditAccess    = "editAccess"
)

// access is the outcome of a permission evaluation. For label permissions the
// min/max hold the votable range granted to the caller.
type access struct {
	allowed bool
	min     int
	max     int
}

var globCache sync.Map // pattern string -> *regexp.Regexp

func globToRegex(pattern string) *regexp.Regexp {
	if re, ok := globCache.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	var sb strings.Builder
	sb.WriteByte('^')
	for _, r := range pattern {
		switch r {
		case '*':
			sb.WriteString(".*") // unlike path.Match, '*' spans '/'
		case '?':
			sb.WriteByte('.')
		default:
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	sb.WriteByte('$')
	re := regexp.MustCompile(sb.String())
	globCache.Store(pattern, re)
	return re
}

// refMatches reports whether ref is covered by a Gerrit-style ref pattern.
// An empty pattern or "*" matches everything.
func refMatches(pattern, ref string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if pattern == ref {
		return true
	}
	return globToRegex(pattern).MatchString(ref)
}

// specificity ranks a ref pattern: the length of its literal prefix before the
// first wildcard. More specific patterns control the outcome.
func specificity(pattern string) int {
	if i := strings.IndexAny(pattern, "*?"); i >= 0 {
		return i
	}
	return len(pattern)
}

// groupIDs returns the set of group ids the caller belongs to. A nil account
// (anonymous) belongs only to "Anonymous Users"; any signed-in account is also
// implicitly a member of "Registered Users".
func (s *Server) groupIDs(acct *store.Account) map[int64]bool {
	ids := map[int64]bool{}
	if acct == nil {
		if g, err := s.db.GetGroupByName("Anonymous Users"); err == nil {
			ids[g.ID] = true
		}
		return ids
	}
	if gs, err := s.db.GroupsForAccount(acct.ID); err == nil {
		for _, g := range gs {
			ids[g.ID] = true
		}
	}
	if g, err := s.db.GetGroupByName("Registered Users"); err == nil {
		ids[g.ID] = true
	}
	return ids
}

// checkAccess evaluates a permission for acct on a project at a given ref.
// Two grant sources are unioned: RBAC role bindings and the legacy
// access_rules inheritance chain. Either one allowing is enough; with no rule
// or binding covering the caller the outcome is denial (default-deny).
// Administrators bypass all checks.
func (s *Server) checkAccess(acct *store.Account, project, ref, permission string) access {
	if acct != nil && acct.Admin {
		return access{allowed: true, min: -2, max: 2}
	}

	// RBAC role bindings act as an additive grant.
	if acct != nil {
		groups := s.groupIDs(acct)
		rbacPerms, err := s.db.RBACPermissions(acct.ID, groups, project)
		if err == nil && rbacToAccess(rbacPerms, permission).allowed {
			return access{allowed: true, min: -2, max: 2}
		}
	}

	// Legacy access_rules evaluation.
	rules, err := s.db.ListAccessRulesInherited(project)
	if err != nil {
		return access{}
	}
	groups := s.groupIDs(acct)

	best := -1
	for _, r := range rules {
		if r.Permission != permission || !groups[r.GroupID] || !refMatches(r.RefPattern, ref) {
			continue
		}
		if sp := specificity(r.RefPattern); sp > best {
			best = sp
		}
	}
	if best < 0 {
		return access{}
	}

	res := access{}
	first := true
	for _, r := range rules {
		if r.Permission != permission || !groups[r.GroupID] || !refMatches(r.RefPattern, ref) {
			continue
		}
		if specificity(r.RefPattern) != best {
			continue
		}
		switch r.Action {
		case "BLOCK", "DENY":
			return access{}
		default: // ALLOW
			res.allowed = true
			if first {
				res.min, res.max, first = r.Min, r.Max, false
			} else {
				if r.Min < res.min {
					res.min = r.Min
				}
				if r.Max > res.max {
					res.max = r.Max
				}
			}
		}
	}
	return res
}

// rbacToAccess converts a set of RBAC permission strings to an access result.
// Permission strings can be plain ("read", "push", "submit") or ranged
// ("review:+2", "label:Code-Review:-2..+2").
func rbacToAccess(perms map[string]bool, permission string) access {
	// Direct match.
	if perms[permission] {
		return access{allowed: true, min: -2, max: 2}
	}
	// Check for wildcard or admin-level permissions.
	if perms["admin"] || perms["*"] {
		return access{allowed: true, min: -2, max: 2}
	}
	// Check for label/ranged permissions: "label:Code-Review" or "review:+2".
	for p := range perms {
		if strings.HasPrefix(p, permission+":") || strings.HasPrefix(p, "label:"+permission) {
			return access{allowed: true, min: -2, max: 2}
		}
	}
	return access{}
}

// can is a boolean convenience wrapper over checkAccess.
func (s *Server) can(acct *store.Account, project, ref, permission string) bool {
	return s.checkAccess(acct, project, ref, permission).allowed
}

// canCapability checks a global capability (project-independent, ref-agnostic).
// Both grant sources apply: a role binding at global scope ("*") or a legacy
// global access rule.
func (s *Server) canCapability(acct *store.Account, permission string) bool {
	if acct != nil && acct.Admin {
		return true
	}
	if acct != nil {
		groups := s.groupIDs(acct)
		if perms, err := s.db.RBACPermissions(acct.ID, groups, "*"); err == nil {
			if rbacToAccess(perms, permission).allowed {
				return true
			}
		}
	}
	rules, err := s.db.ListAccessRules("*")
	if err != nil {
		return false
	}
	groups := s.groupIDs(acct)
	for _, r := range rules {
		if r.Permission == permission && groups[r.GroupID] && r.Action == "ALLOW" {
			return true
		}
	}
	return false
}

// branchRef builds the ref a change's permissions are evaluated against.
func branchRef(branch string) string { return "refs/heads/" + branch }

// canReadChange reports whether acct may view a change, honouring read
// permission on the destination branch plus the private-change rule (only the
// owner, reviewers and admins see private changes).
func (s *Server) canReadChange(acct *store.Account, c *store.Change) bool {
	if acct != nil && acct.Admin {
		return true
	}
	if c.Private {
		if acct == nil {
			return false
		}
		if acct.ID == c.OwnerID {
			return true
		}
		for _, rv := range s.reviewersOf(c.Number) {
			if rv.AccountID == acct.ID {
				return true
			}
		}
		return false
	}
	return s.can(acct, c.Project, branchRef(c.Branch), PermRead)
}

// reviewersOf is a small helper returning reviewer rows for permission checks.
func (s *Server) reviewersOf(changeNum int64) []*store.Reviewer {
	rvs, err := s.db.ListReviewers(changeNum)
	if err != nil {
		return nil
	}
	return rvs
}
