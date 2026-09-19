package store

import "testing"

func TestTeamCRUDAndMembership(t *testing.T) {
	db := openTemp(t)
	lead := mustAccount(t, db, "lead", "Lead One")
	m1 := mustAccount(t, db, "m1", "Member One")

	team := &Team{Name: "bsp", DisplayName: "BSP Team", Description: "d", LeaderID: lead.ID}
	if err := db.CreateTeam(team); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if team.ID == 0 {
		t.Fatal("team id not set")
	}
	// Leader is auto-added as member.
	ids, err := db.TeamIDsForAccount(lead.ID)
	if err != nil || !ids[team.ID] {
		t.Fatalf("leader not auto-member: ids=%v err=%v", ids, err)
	}

	if err := db.AddTeamMember(team.ID, m1.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := db.AddTeamMember(team.ID, m1.ID); err != nil {
		t.Fatalf("duplicate add should be idempotent: %v", err)
	}

	got, err := db.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("get team: %v", err)
	}
	if got.Name != "bsp" || got.MemberCount != 2 {
		t.Fatalf("unexpected team: %+v", got)
	}

	own, err := db.ListTeamsForAccount(m1.ID)
	if err != nil || len(own) != 1 || own[0].ID != team.ID {
		t.Fatalf("ListTeamsForAccount(m1)=%v err=%v", own, err)
	}
	if out, err := db.ListTeamsForAccount(lead.ID); err != nil || len(out) != 1 {
		t.Fatalf("leader should see team via membership: %v err=%v", out, err)
	}
	stranger := mustAccount(t, db, "stranger", "S")
	if out, err := db.ListTeamsForAccount(stranger.ID); err != nil || len(out) != 0 {
		t.Fatalf("stranger should see no teams: %v err=%v", out, err)
	}

	// Removing the leader clears leader_id and membership.
	if err := db.RemoveTeamMember(team.ID, lead.ID); err != nil {
		t.Fatalf("remove leader: %v", err)
	}
	got, err = db.GetTeam(team.ID)
	if err != nil || got.LeaderID != 0 || got.MemberCount != 1 {
		t.Fatalf("after leader removal: %+v err=%v", got, err)
	}
}

func TestTeamRoleBindingsAndDelete(t *testing.T) {
	db := openTemp(t)
	m1 := mustAccount(t, db, "m1", "Member One")

	team := &Team{Name: "sdk"}
	if err := db.CreateTeam(team); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if err := db.AddTeamMember(team.ID, m1.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	role := &Role{Name: "sdk-dev", Permissions: []string{"read", "push"}}
	if err := db.CreateRole(role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	rb := &RoleBinding{RoleID: role.ID, SubjectType: "team", SubjectID: team.ID, Scope: "rk/*"}
	if err := db.CreateRoleBinding(rb); err != nil {
		t.Fatalf("create binding: %v", err)
	}

	bindings, err := db.RoleBindingsForAccount(m1.ID, nil)
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].ID != rb.ID {
		t.Fatalf("member should inherit team binding, got %+v", bindings)
	}
	if out, err := db.ListTeamsForAccount(m1.ID); err != nil || len(out) != 1 {
		t.Fatalf("own teams: %v err=%v", out, err)
	}
	// Non-member sees nothing.
	other := mustAccount(t, db, "other", "O")
	if bindings, err := db.RoleBindingsForAccount(other.ID, nil); err != nil || len(bindings) != 0 {
		t.Fatalf("non-member should have no bindings: %+v err=%v", bindings, err)
	}
	// Scope matching through RBACPermissions.
	perms, err := db.RBACPermissions(m1.ID, nil, "rk/Linux/kernel")
	if err != nil {
		t.Fatalf("rbac perms: %v", err)
	}
	if !perms["read"] || !perms["push"] {
		t.Fatalf("expected read+push via team binding, got %v", perms)
	}
	if perms, err := db.RBACPermissions(m1.ID, nil, "other/proj"); err != nil || len(perms) != 0 {
		t.Fatalf("out-of-scope project should grant nothing: %v err=%v", perms, err)
	}

	// DeleteTeam removes members and the team's bindings.
	if err := db.DeleteTeam(team.ID); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM team_members WHERE team_id=?`, team.ID); n != 0 {
		t.Fatalf("members left after delete: %d", n)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM role_bindings WHERE subject_type='team' AND subject_id=?`, team.ID); n != 0 {
		t.Fatalf("bindings left after delete: %d", n)
	}
}

func TestTeamMirrorGroupAndAssignable(t *testing.T) {
	db := openTemp(t)
	lead := mustAccount(t, db, "lead", "Lead")
	m1 := mustAccount(t, db, "m1", "M1")

	team := &Team{Name: "kernel", LeaderID: lead.ID}
	if err := db.CreateTeam(team); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if team.GroupID == 0 {
		t.Fatal("mirror group not attached")
	}
	g, err := db.GetGroupByName("team:kernel")
	if err != nil || g.ID != team.GroupID || !g.System {
		t.Fatalf("mirror group lookup: %+v err=%v", g, err)
	}
	if err := db.AddTeamMember(team.ID, m1.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	members, err := db.ListGroupMembers(team.GroupID)
	if err != nil || len(members) != 2 {
		t.Fatalf("mirror group members: %v err=%v", members, err)
	}
	if groups, err := db.GroupsForAccount(m1.ID); err != nil || len(groups) != 1 {
		t.Fatalf("GroupsForAccount should include mirror: %v err=%v", groups, err)
	}

	// Team rename follows into the mirror group.
	team.Name = "kernel-team"
	if err := db.UpdateTeam(team); err != nil {
		t.Fatalf("update team: %v", err)
	}
	if _, err := db.GetGroupByName("team:kernel-team"); err != nil {
		t.Fatalf("renamed mirror group missing: %v", err)
	}

	// RemoveTeamMember syncs the mirror too.
	if err := db.RemoveTeamMember(team.ID, m1.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if members, err := db.ListGroupMembers(team.GroupID); err != nil || len(members) != 1 {
		t.Fatalf("mirror after removal: %v err=%v", members, err)
	}

	// DeleteTeam drops the mirror group.
	if err := db.DeleteTeam(team.ID); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if _, err := db.GetGroup(team.GroupID); err == nil {
		t.Fatal("mirror group should be gone after team delete")
	}

	// team_assignable round-trip + GetRoleBinding.
	role := &Role{Name: "dev", Permissions: []string{"read"}, TeamAssignable: true}
	if err := db.CreateRole(role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	got, err := db.GetRole(role.ID)
	if err != nil || !got.TeamAssignable {
		t.Fatalf("role assignable lost: %+v err=%v", got, err)
	}
	got.TeamAssignable = false
	if err := db.UpdateRole(got); err != nil {
		t.Fatalf("update role: %v", err)
	}
	if got, err := db.GetRole(role.ID); err != nil || got.TeamAssignable {
		t.Fatalf("role assignable update failed: %+v err=%v", got, err)
	}
	team2 := &Team{Name: "sdk2"}
	if err := db.CreateTeam(team2); err != nil {
		t.Fatalf("create team2: %v", err)
	}
	rb := &RoleBinding{RoleID: role.ID, SubjectType: "team", SubjectID: team2.ID, Scope: "rk/*"}
	if err := db.CreateRoleBinding(rb); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	if one, err := db.GetRoleBinding(rb.ID); err != nil || one.SubjectType != "team" || one.RoleName != "dev" {
		t.Fatalf("GetRoleBinding: %+v err=%v", one, err)
	}
	if db.LeadsAnyTeam(m1.ID) {
		t.Fatal("member with no led teams should be false")
	}
	team2.LeaderID = lead.ID
	if err := db.UpdateTeam(team2); err != nil {
		t.Fatalf("set leader: %v", err)
	}
	if !db.LeadsAnyTeam(lead.ID) {
		t.Fatal("LeadsAnyTeam should be true after leader set")
	}
}
