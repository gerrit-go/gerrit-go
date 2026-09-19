package api

import (
	"net/http"
	"strconv"
	"strings"

	"gerrit-go/internal/store"
)

// ---------- teams ----------
//
// A team is a named group of accounts with an optional leader. Teams receive
// access through role bindings with subject_type "team". Admins see and
// manage all teams; any signed-in user sees the teams they belong to.

func (s *Server) teamByIDOr404(w http.ResponseWriter, idRaw string) (*store.Team, bool) {
	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid team id")
		return nil, false
	}
	team, err := s.db.GetTeam(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "team not found")
		return nil, false
	}
	return team, true
}

// canManageTeam is the leader-autonomy gate: admins manage every team, a
// leader manages members and role bindings of the teams they lead.
func (s *Server) canManageTeam(acct *store.Account, team *store.Team) bool {
	return acct.Admin || (team.LeaderID != 0 && acct.ID == team.LeaderID)
}

// enrichTeams fills in leader_name and binding scopes for list responses.
func (s *Server) enrichTeams(teams []*store.Team) {
	bindings, _ := s.db.ListRoleBindings(0, "team", 0)
	byTeam := map[int64][]string{}
	for _, rb := range bindings {
		byTeam[rb.SubjectID] = append(byTeam[rb.SubjectID], rb.Scope)
	}
	cache := map[int64]string{}
	for _, t := range teams {
		t.Scopes = byTeam[t.ID]
		if t.LeaderID == 0 {
			continue
		}
		if name, ok := cache[t.LeaderID]; ok {
			t.LeaderName = name
			continue
		}
		if a, err := s.db.GetAccount(t.LeaderID); err == nil {
			cache[t.LeaderID] = a.Username
			t.LeaderName = a.Username
		}
	}
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	var (
		teams []*store.Team
		err   error
	)
	if acct.Admin {
		teams, err = s.db.ListTeams()
	} else {
		teams, err = s.db.ListTeamsForAccount(acct.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if teams == nil {
		teams = []*store.Team{}
	}
	s.enrichTeams(teams)
	for _, t := range teams {
		t.CanManage = s.canManageTeam(acct, t)
	}
	writeJSON(w, http.StatusOK, teams)
}

func (s *Server) teamMemberView(teamID int64) ([]map[string]any, error) {
	members, err := s.db.ListTeamMembers(teamID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		view := map[string]any{"account_id": m.AccountID, "joined": m.Joined}
		if a, err := s.db.GetAccount(m.AccountID); err == nil {
			view["username"] = a.Username
			view["name"] = a.FullName
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	team, ok := s.teamByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	teamIDs, _ := s.db.TeamIDsForAccount(acct.ID)
	if !acct.Admin && !teamIDs[team.ID] {
		s.forbid(w, r, "team membership")
		return
	}
	s.enrichTeams([]*store.Team{team})
	team.CanManage = s.canManageTeam(acct, team)
	members, err := s.teamMemberView(team.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	bindings, _ := s.db.ListRoleBindings(0, "team", team.ID)
	if bindings == nil {
		bindings = []*store.RoleBinding{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"team":     team,
		"members":  members,
		"bindings": bindings,
	})
}

// validTeamName keeps names URL/label friendly: letters, digits, dash, underscore, dot.
func validTeamName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, ru := range name {
		ok := ru == '-' || ru == '_' || ru == '.' ||
			(ru >= 'a' && ru <= 'z') || (ru >= 'A' && ru <= 'Z') || (ru >= '0' && ru <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Leader      string `json:"leader"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validTeamName(req.Name) {
		writeErr(w, http.StatusBadRequest, "name must use letters, digits, '-', '_' or '.'")
		return
	}
	team := &store.Team{Name: req.Name, DisplayName: req.DisplayName, Description: req.Description}
	if req.Leader != "" {
		lead, err := s.resolveAccount(req.Leader)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "leader account not found")
			return
		}
		team.LeaderID = lead.ID
	}
	if err := s.db.CreateTeam(team); err != nil {
		writeErr(w, http.StatusConflict, "team name already exists")
		return
	}
	s.audit(acct, "team-create", "team", team.Name, "leader="+req.Leader)
	s.enrichTeams([]*store.Team{team})
	writeJSON(w, http.StatusCreated, team)
}

func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	team, ok := s.teamByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManageTeam(acct, team) {
		s.forbid(w, r, "team leadership")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		DisplayName *string `json:"display_name"`
		Description *string `json:"description"`
		Leader      *string `json:"leader"` // "" clears the leader
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !acct.Admin {
		// Leaders manage their team's profile, not its identity or leader.
		if req.Name != nil && strings.TrimSpace(*req.Name) != team.Name {
			writeErr(w, http.StatusForbidden, "only admins can rename a team")
			return
		}
		req.Name = nil
		if req.Leader != nil {
			l := strings.TrimSpace(*req.Leader)
			if l == "" {
				if team.LeaderID != 0 {
					writeErr(w, http.StatusForbidden, "only admins can change the team leader")
					return
				}
			} else {
				lead, err := s.resolveAccount(l)
				if err != nil || lead.ID != team.LeaderID {
					writeErr(w, http.StatusForbidden, "only admins can change the team leader")
					return
				}
			}
			req.Leader = nil
		}
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !validTeamName(name) {
			writeErr(w, http.StatusBadRequest, "name must use letters, digits, '-', '_' or '.'")
			return
		}
		team.Name = name
	}
	if req.DisplayName != nil {
		team.DisplayName = *req.DisplayName
	}
	if req.Description != nil {
		team.Description = *req.Description
	}
	if req.Leader != nil {
		if *req.Leader == "" {
			team.LeaderID = 0
		} else {
			lead, err := s.resolveAccount(*req.Leader)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "leader account not found")
				return
			}
			team.LeaderID = lead.ID
		}
	}
	if err := s.db.UpdateTeam(team); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(acct, "team-update", "team", team.Name, "")
	s.enrichTeams([]*store.Team{team})
	team.CanManage = s.canManageTeam(acct, team)
	writeJSON(w, http.StatusOK, team)
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	team, ok := s.teamByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := s.db.DeleteTeam(team.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "team-delete", "team", team.Name, "")
	w.WriteHeader(http.StatusNoContent)
}

// handleTeamAccountCandidates gives team leaders a minimal account picker
// (username + display name only, search-gated) without exposing the admin
// directory endpoint.
func (s *Server) handleTeamAccountCandidates(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	team, ok := s.teamByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManageTeam(acct, team) {
		s.forbid(w, r, "team leadership")
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	accounts, err := s.db.ListAccounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, 25)
	for _, a := range accounts {
		if len(out) >= 25 {
			break
		}
		if strings.Contains(strings.ToLower(a.Username), q) || strings.Contains(strings.ToLower(a.FullName), q) {
			out = append(out, map[string]any{"_account_id": a.ID, "username": a.Username, "name": a.FullName})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	team, ok := s.teamByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManageTeam(acct, team) {
		s.forbid(w, r, "team leadership")
		return
	}
	target, err := s.resolveAccount(r.PathValue("account"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.db.AddTeamMember(team.ID, target.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "team-member-add", "team", team.Name, target.Username)
	writeJSON(w, http.StatusOK, map[string]any{"account_id": target.ID, "username": target.Username})
}

func (s *Server) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	team, ok := s.teamByIDOr404(w, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canManageTeam(acct, team) {
		s.forbid(w, r, "team leadership")
		return
	}
	target, err := s.resolveAccount(r.PathValue("account"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if !acct.Admin && target.ID == team.LeaderID {
		writeErr(w, http.StatusBadRequest, "leaders cannot remove themselves; an admin must transfer leadership first")
		return
	}
	if err := s.db.RemoveTeamMember(team.ID, target.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "team-member-remove", "team", team.Name, target.Username)
	w.WriteHeader(http.StatusNoContent)
}
