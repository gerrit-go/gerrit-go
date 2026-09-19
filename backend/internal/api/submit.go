package api

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gerrit-go/internal/store"
)

func shortSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// currentCommit returns the commit SHA of a change's current patch set.
func (s *Server) currentCommit(c *store.Change) string {
	ps, err := s.db.GetPatchSet(c.Number, c.CurrentPS)
	if err != nil {
		return ""
	}
	return ps.CommitSHA
}

// submitRequirementsMet evaluates the project's submit requirements against the
// change's votes. It reports whether the change may be submitted and, if not, a
// human-readable reason.
func (s *Server) submitRequirementsMet(c *store.Change) (bool, string) {
	reqs, err := s.db.ListSubmitRequirements(c.Project)
	if err != nil {
		return false, err.Error()
	}
	votes, _ := s.db.ListVotes(c.Number)
	byLabel := map[string][]int{}
	for _, v := range votes {
		byLabel[v.Label] = append(byLabel[v.Label], v.Value)
	}
	for _, req := range reqs {
		vals := byLabel[req.Label]
		maxV, minV := 0, 0
		for i, x := range vals {
			if i == 0 {
				maxV, minV = x, x
				continue
			}
			if x > maxV {
				maxV = x
			}
			if x < minV {
				minV = x
			}
		}
		if len(vals) > 0 && minV <= req.BlockValue {
			return false, fmt.Sprintf("%s vetoed (%+d)", req.Label, minV)
		}
		if len(vals) == 0 || maxV < req.MinValue {
			return false, fmt.Sprintf("%s %+d required", req.Label, req.MinValue)
		}
	}
	if msg := s.requiredPipelinesBlocked(c); msg != "" {
		return false, msg
	}
	return true, ""
}

// requiredPipelinesBlocked reports a reason when a pipeline marked "required"
// has no successful run on the change's current patch set. Empty means allowed.
func (s *Server) requiredPipelinesBlocked(c *store.Change) string {
	var needed []string
	for _, scope := range []string{c.Project, "*"} {
		configs, err := s.db.ListPipelineConfigs(scope)
		if err != nil {
			return ""
		}
		for _, cfg := range configs {
			if cfg.Enabled && cfg.Required {
				needed = append(needed, cfg.Name)
			}
		}
	}
	if len(needed) == 0 {
		return ""
	}
	runs, err := s.db.ListPipelineRunsForPatchSet(c.Number, c.CurrentPS)
	if err != nil {
		return ""
	}
	passed := map[string]bool{}
	for _, r := range runs {
		if r.Status == store.RunSuccess {
			passed[r.ConfigName] = true
		}
	}
	var missing []string
	for _, name := range needed {
		if !passed[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "pipeline required: " + strings.Join(missing, ", ")
}

// openAncestors returns open changes in the same project and branch whose commit
// is a strict ancestor of this change's commit — the relation chain that must be
// submitted first.
func (s *Server) openAncestors(c *store.Change) []*store.Change {
	commit := s.currentCommit(c)
	if commit == "" {
		return nil
	}
	siblings, _, err := s.db.SearchChanges(store.ChangeQuery{
		Status: "NEW", Project: c.Project, Branch: c.Branch, Limit: 500,
	})
	if err != nil {
		return nil
	}
	var out []*store.Change
	for _, oc := range siblings {
		if oc.Number == c.Number {
			continue
		}
		ocCommit := s.currentCommit(oc)
		if ocCommit == "" || ocCommit == commit {
			continue
		}
		if ok, err := s.git.IsAncestor(c.Project, ocCommit, commit); err == nil && ok {
			out = append(out, oc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// topicSiblings returns the other open changes sharing this change's topic in
// the same project (used by submit-whole-topic).
func (s *Server) topicSiblings(c *store.Change) []*store.Change {
	if c.Topic == "" {
		return nil
	}
	siblings, _, err := s.db.SearchChanges(store.ChangeQuery{
		Status: "NEW", Project: c.Project, Topic: c.Topic, Limit: 500,
	})
	if err != nil {
		return nil
	}
	var out []*store.Change
	for _, oc := range siblings {
		if oc.Number != c.Number {
			out = append(out, oc)
		}
	}
	return out
}

func changeNums(cs []*store.Change) string {
	nums := make([]string, len(cs))
	for i, c := range cs {
		nums[i] = strconv.FormatInt(c.Number, 10)
	}
	return strings.Join(nums, ", ")
}

// relationChain returns the changes in the same project/branch that are
// ancestors (parents) or descendants (children) of this change's commit,
// ordered oldest-parent first, then this change, then children. It mirrors
// Gerrit's relation chain shown on the change page.
func (s *Server) relationChain(c *store.Change) []map[string]any {
	commit := s.currentCommit(c)
	if commit == "" {
		return nil
	}
	all, _, err := s.db.SearchChanges(store.ChangeQuery{
		Project: c.Project, Branch: c.Branch, Limit: 500,
	})
	if err != nil {
		return nil
	}
	var ancestors, descendants []*store.Change
	for _, oc := range all {
		if oc.Number == c.Number || oc.Status != "NEW" {
			continue
		}
		ocCommit := s.currentCommit(oc)
		if ocCommit == "" || ocCommit == commit {
			continue
		}
		if ok, _ := s.git.IsAncestor(c.Project, ocCommit, commit); ok {
			ancestors = append(ancestors, oc)
			continue
		}
		if ok, _ := s.git.IsAncestor(c.Project, commit, ocCommit); ok {
			descendants = append(descendants, oc)
		}
	}
	sort.Slice(ancestors, func(i, j int) bool { return ancestors[i].Number < ancestors[j].Number })
	sort.Slice(descendants, func(i, j int) bool { return descendants[i].Number < descendants[j].Number })

	entry := func(ch *store.Change, relation string, self bool) map[string]any {
		m := map[string]any{
			"_number":  ch.Number,
			"subject":  ch.Subject,
			"status":   ch.Status,
			"relation": relation,
		}
		if self {
			m["self"] = true
		}
		return m
	}
	var out []map[string]any
	for _, a := range ancestors {
		out = append(out, entry(a, "ancestor", false))
	}
	out = append(out, entry(c, "self", true))
	for _, d := range descendants {
		out = append(out, entry(d, "descendant", false))
	}
	if len(out) == 1 {
		return nil
	}
	return out
}
