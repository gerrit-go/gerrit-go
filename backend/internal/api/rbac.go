package api

import (
	"net/http"
	"strconv"
	"strings"

	"gerrit-go/internal/store"
)

// ---------- roles ----------

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.db.ListRoles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) handleGetRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	role, err := s.db.GetRole(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "role not found")
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	var req struct {
		Name           string   `json:"name"`
		DisplayName    string   `json:"display_name"`
		Description    string   `json:"description"`
		Permissions    []string `json:"permissions"`
		TeamAssignable bool     `json:"team_assignable"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	role := &store.Role{
		Name:           req.Name,
		DisplayName:    req.DisplayName,
		Description:    req.Description,
		Permissions:    req.Permissions,
		TeamAssignable: req.TeamAssignable,
	}
	if err := s.db.CreateRole(role); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(acct, "role-create", "role", strconv.FormatInt(role.ID, 10),
		role.Name+" perms="+strings.Join(role.Permissions, ","))
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	role, err := s.db.GetRole(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "role not found")
		return
	}
	var req struct {
		DisplayName string   `json:"display_name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role.DisplayName = req.DisplayName
	role.Description = req.Description
	role.Permissions = req.Permissions
	if err := s.db.UpdateRole(role); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "role-update", "role", strconv.FormatInt(role.ID, 10),
		role.Name+" perms="+strings.Join(role.Permissions, ","))
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	if err := s.db.DeleteRole(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "role-delete", "role", strconv.FormatInt(id, 10), "")
	writeJSON(w, http.StatusOK, nil)
}

// ---------- role bindings ----------

// handleListAllRoleBindings returns every binding (with role names) so the
// organization page can show which roles apply to which scopes in one call.
func (s *Server) handleListAllRoleBindings(w http.ResponseWriter, r *http.Request) {
	if acct := s.account(r); acct == nil || !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	bindings, err := s.db.ListRoleBindings(0, "", 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bindings)
}

func (s *Server) handleListRoleBindings(w http.ResponseWriter, r *http.Request) {
	roleID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	bindings, err := s.db.ListRoleBindings(roleID, "", 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bindings)
}

func (s *Server) handleCreateRoleBinding(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	roleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	role, err := s.db.GetRole(roleID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "role not found")
		return
	}
	var req struct {
		SubjectType string `json:"subject_type"` // "account" | "group" | "team"
		SubjectID   int64  `json:"subject_id"`
		Scope       string `json:"scope"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SubjectType != "account" && req.SubjectType != "group" && req.SubjectType != "team" {
		writeErr(w, http.StatusBadRequest, "subject_type must be account, group or team")
		return
	}
	if req.Scope == "" {
		req.Scope = "*"
	}
	if req.SubjectType == "team" {
		if _, err := s.db.GetTeam(req.SubjectID); err != nil {
			writeErr(w, http.StatusNotFound, "team not found")
			return
		}
	}
	if !acct.Admin {
		// Leader autonomy: a team leader may bind an admin-approved
		// (team_assignable) role to their own team within a concrete scope.
		if req.SubjectType != "team" {
			s.forbid(w, r, "admin")
			return
		}
		team, err := s.db.GetTeam(req.SubjectID)
		if err != nil || team.LeaderID != acct.ID {
			s.forbid(w, r, "team leadership")
			return
		}
		if !role.TeamAssignable {
			writeErr(w, http.StatusForbidden, "role is not team-assignable")
			return
		}
		if req.Scope == "*" {
			writeErr(w, http.StatusForbidden, "team leaders must bind to a concrete scope")
			return
		}
	}
	rb := &store.RoleBinding{
		RoleID:      roleID,
		SubjectType: req.SubjectType,
		SubjectID:   req.SubjectID,
		Scope:       req.Scope,
	}
	if err := s.db.CreateRoleBinding(rb); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "binding-create", "role-binding", strconv.FormatInt(rb.ID, 10),
		"role="+strconv.FormatInt(roleID, 10)+" subject="+req.SubjectType+":"+strconv.FormatInt(req.SubjectID, 10)+" scope="+rb.Scope)
	writeJSON(w, http.StatusCreated, rb)
}

func (s *Server) handleDeleteRoleBinding(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid binding id")
		return
	}
	if !acct.Admin {
		rb, err := s.db.GetRoleBinding(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "binding not found")
			return
		}
		if rb.SubjectType != "team" {
			s.forbid(w, r, "admin")
			return
		}
		team, err := s.db.GetTeam(rb.SubjectID)
		if err != nil || team.LeaderID != acct.ID {
			s.forbid(w, r, "team leadership")
			return
		}
	}
	if err := s.db.DeleteRoleBinding(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "binding-delete", "role-binding", strconv.FormatInt(id, 10), "")
	writeJSON(w, http.StatusOK, nil)
}

// ---------- effective permissions ----------

// handleEffectivePermissions returns the current user's effective permissions,
// optionally filtered by project. Shows both RBAC and legacy access_rules sources.
func (s *Server) handleEffectivePermissions(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.URL.Query().Get("project")

	groups := s.groupIDs(acct)
	out := map[string]any{
		"account_id": acct.ID,
		"username":   acct.Username,
		"admin":      acct.Admin,
	}

	// RBAC permissions.
	rbacPerms, _ := s.db.RBACPermissions(acct.ID, groups, project)
	rbacList := make([]string, 0, len(rbacPerms))
	for p := range rbacPerms {
		rbacList = append(rbacList, p)
	}
	out["rbac_permissions"] = rbacList

	// Role bindings.
	bindings, _ := s.db.RoleBindingsForAccount(acct.ID, groups)
	out["role_bindings"] = bindings

	// Legacy access_rules permissions (for the given project).
	if project != "" {
		legacyPerms := map[string]bool{}
		for _, perm := range []string{PermRead, PermPush, PermSubmit, PermAbandon, PermComment, PermEditAccess} {
			if s.can(acct, project, "refs/heads/*", perm) {
				legacyPerms[perm] = true
			}
		}
		legacyList := make([]string, 0, len(legacyPerms))
		for p := range legacyPerms {
			legacyList = append(legacyList, p)
		}
		out["legacy_permissions"] = legacyList
	}

	writeJSON(w, http.StatusOK, out)
}
