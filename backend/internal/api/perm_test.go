package api

import (
	"path/filepath"
	"testing"

	"gerrit-go/internal/store"
)

// openPermTestDB creates a temp SQLite DB for permission tests.
func openPermTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "perm-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// permTestServer returns a minimal Server with only the db field set,
// sufficient for checkAccess/can/canCapability tests.
func permTestServer(db *store.DB) *Server {
	return &Server{db: db}
}

func mustPermAccount(t *testing.T, db *store.DB, username string, admin bool) *store.Account {
	t.Helper()
	a := &store.Account{Username: username, FullName: username, Email: username + "@test.com", Admin: admin}
	if err := db.CreateAccount(a); err != nil {
		t.Fatalf("create account %s: %v", username, err)
	}
	return a
}

func mustPermProject(t *testing.T, db *store.DB, name string) {
	t.Helper()
	if err := db.CreateProject(&store.Project{Name: name, Head: "main"}); err != nil {
		t.Fatalf("create project %s: %v", name, err)
	}
}

func mustPermGroup(t *testing.T, db *store.DB, name string) *store.Group {
	t.Helper()
	g := &store.Group{Name: name}
	if err := db.CreateGroup(g); err != nil {
		t.Fatalf("create group %s: %v", name, err)
	}
	return g
}

func mustGroupMember(t *testing.T, db *store.DB, groupID, accountID int64) {
	t.Helper()
	if err := db.AddGroupMember(groupID, accountID); err != nil {
		t.Fatalf("add group member: %v", err)
	}
}

func mustAccessRule(t *testing.T, db *store.DB, project, ref, perm string, groupID int64, action string, min, max int) {
	t.Helper()
	r := &store.AccessRule{
		Project: project, RefPattern: ref, Permission: perm,
		GroupID: groupID, Action: action, Min: min, Max: max,
	}
	if err := db.AddAccessRule(r); err != nil {
		t.Fatalf("create access rule: %v", err)
	}
}

// ---------- legacy access_rules tests ----------

func TestCheckAccessAdminBypass(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	admin := mustPermAccount(t, db, "admin", true)
	mustPermProject(t, db, "foo")

	acc := s.checkAccess(admin, "foo", "refs/heads/main", PermRead)
	if !acc.allowed {
		t.Error("admin should bypass all checks")
	}
	if acc.min != -2 || acc.max != 2 {
		t.Errorf("admin range = [%d,%d], want [-2,2]", acc.min, acc.max)
	}
}

func TestCheckAccessAnonymousDenied(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	mustPermProject(t, db, "foo")

	// Anonymous has global default read. Test a permission not covered by defaults.
	acc := s.checkAccess(nil, "foo", "refs/heads/main", PermSubmit)
	if acc.allowed {
		t.Error("anonymous should not have submit access")
	}
	// But read is granted by the default Anonymous Users rule.
	acc = s.checkAccess(nil, "foo", "refs/heads/main", PermRead)
	if !acc.allowed {
		t.Error("anonymous should have read via default rule")
	}
}

func TestCheckAccessBasicAllow(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)
	mustAccessRule(t, db, "foo", "refs/heads/*", PermRead, devs.ID, "ALLOW", 0, 0)

	acc := s.checkAccess(alice, "foo", "refs/heads/main", PermRead)
	if !acc.allowed {
		t.Error("alice should have read access via group")
	}
}

func TestCheckAccessDenyOverrides(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)
	mustAccessRule(t, db, "foo", "refs/heads/*", PermPush, devs.ID, "ALLOW", 0, 0)
	mustAccessRule(t, db, "foo", "refs/heads/*", PermPush, devs.ID, "DENY", 0, 0)

	acc := s.checkAccess(alice, "foo", "refs/heads/main", PermPush)
	if acc.allowed {
		t.Error("DENY should override ALLOW at same specificity")
	}
}

func TestCheckAccessBlockOverrides(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)
	mustAccessRule(t, db, "foo", "refs/heads/*", PermPush, devs.ID, "ALLOW", 0, 0)
	mustAccessRule(t, db, "foo", "refs/heads/*", PermPush, devs.ID, "BLOCK", 0, 0)

	acc := s.checkAccess(alice, "foo", "refs/heads/main", PermPush)
	if acc.allowed {
		t.Error("BLOCK should override ALLOW")
	}
}

func TestCheckAccessSpecificity(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)
	// Broad allow.
	mustAccessRule(t, db, "foo", "refs/heads/*", PermPush, devs.ID, "ALLOW", 0, 0)
	// More specific deny on release branch.
	mustAccessRule(t, db, "foo", "refs/heads/release", PermPush, devs.ID, "DENY", 0, 0)

	// main: allowed by broad rule.
	acc := s.checkAccess(alice, "foo", "refs/heads/main", PermPush)
	if !acc.allowed {
		t.Error("main should be allowed by broad rule")
	}
	// release: denied by more specific rule.
	acc = s.checkAccess(alice, "foo", "refs/heads/release", PermPush)
	if acc.allowed {
		t.Error("release should be denied by specific rule")
	}
}

func TestCheckAccessLabelRange(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)
	mustAccessRule(t, db, "foo", "refs/heads/*", "label-Code-Review", devs.ID, "ALLOW", -1, 2)

	acc := s.checkAccess(alice, "foo", "refs/heads/main", "label-Code-Review")
	if !acc.allowed {
		t.Fatal("should have label access")
	}
	if acc.min != -1 || acc.max != 2 {
		t.Errorf("label range = [%d,%d], want [-1,2]", acc.min, acc.max)
	}
}

func TestCheckAccessNotMember(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	bob := mustPermAccount(t, db, "bob", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)
	// Grant submit only to developers.
	mustAccessRule(t, db, "foo", "refs/heads/*", PermSubmit, devs.ID, "ALLOW", 0, 0)

	// alice is member: allowed.
	if !s.can(alice, "foo", "refs/heads/main", PermSubmit) {
		t.Error("alice should have submit access")
	}
	// bob is not member: denied (no default submit rule).
	if s.can(bob, "foo", "refs/heads/main", PermSubmit) {
		t.Error("bob should not have submit access")
	}
}

func TestCheckAccessGlobalDefault(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")

	// The schema seeds a global default: Registered Users get refs/* read ALLOW.
	// Alice is a signed-in user, so she's implicitly in Registered Users.
	acc := s.checkAccess(alice, "foo", "refs/heads/main", PermRead)
	if !acc.allowed {
		t.Error("should inherit global default read access")
	}
}

// ---------- RBAC tests ----------

func mustRole(t *testing.T, db *store.DB, name string, perms []string) *store.Role {
	t.Helper()
	r := &store.Role{Name: name, Permissions: perms}
	if err := db.CreateRole(r); err != nil {
		t.Fatalf("create role %s: %v", name, err)
	}
	return r
}

func mustRoleBinding(t *testing.T, db *store.DB, roleID int64, subjectType string, subjectID int64, scope string) {
	t.Helper()
	rb := &store.RoleBinding{RoleID: roleID, SubjectType: subjectType, SubjectID: subjectID, Scope: scope}
	if err := db.CreateRoleBinding(rb); err != nil {
		t.Fatalf("create role binding: %v", err)
	}
}

func TestRBACBasicGrant(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")

	role := mustRole(t, db, "dev", []string{"read", "push"})
	mustRoleBinding(t, db, role.ID, "account", alice.ID, "*")

	if !s.can(alice, "foo", "refs/heads/main", PermRead) {
		t.Error("RBAC should grant read")
	}
	if !s.can(alice, "foo", "refs/heads/main", PermPush) {
		t.Error("RBAC should grant push")
	}
	// Not in role permissions.
	if s.can(alice, "foo", "refs/heads/main", PermSubmit) {
		t.Error("RBAC should not grant submit")
	}
}

func TestRBACScopeProject(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	mustPermProject(t, db, "bar")

	role := mustRole(t, db, "dev", []string{"read", "push", "submit"})
	mustRoleBinding(t, db, role.ID, "account", alice.ID, "foo")

	if !s.can(alice, "foo", "refs/heads/main", PermSubmit) {
		t.Error("should have submit on foo")
	}
	// bar is not in scope — RBAC doesn't match, and no legacy submit rule.
	if s.can(alice, "bar", "refs/heads/main", PermSubmit) {
		t.Error("should not have submit on bar")
	}
}

func TestRBACScopeNamespace(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "rk/kernel")
	mustPermProject(t, db, "rk/device/rk3576")
	mustPermProject(t, db, "docs/android")

	role := mustRole(t, db, "bsp", []string{"read", "push", "submit"})
	mustRoleBinding(t, db, role.ID, "account", alice.ID, "rk/*")

	if !s.can(alice, "rk/kernel", "refs/heads/main", PermSubmit) {
		t.Error("should have submit on rk/kernel")
	}
	if !s.can(alice, "rk/device/rk3576", "refs/heads/main", PermSubmit) {
		t.Error("should have submit on rk/device/rk3576")
	}
	if s.can(alice, "docs/android", "refs/heads/main", PermSubmit) {
		t.Error("should not have submit on docs/android")
	}
}

func TestRBACScopeLabelSelector(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	mustPermProject(t, db, "bar")

	if err := db.SetProjectLabel("foo", "product", "android"); err != nil {
		t.Fatalf("set label: %v", err)
	}

	role := mustRole(t, db, "android-dev", []string{"read", "push", "submit"})
	mustRoleBinding(t, db, role.ID, "account", alice.ID, "label:product=android")

	if !s.can(alice, "foo", "refs/heads/main", PermSubmit) {
		t.Error("should have submit on foo (has product:android label)")
	}
	if s.can(alice, "bar", "refs/heads/main", PermSubmit) {
		t.Error("should not have submit on bar (no label)")
	}
}

func TestRBACGroupBinding(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	bob := mustPermAccount(t, db, "bob", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)

	role := mustRole(t, db, "dev", []string{"read", "push", "submit"})
	mustRoleBinding(t, db, role.ID, "group", devs.ID, "*")

	if !s.can(alice, "foo", "refs/heads/main", PermSubmit) {
		t.Error("alice should have submit via group binding")
	}
	if s.can(bob, "foo", "refs/heads/main", PermSubmit) {
		t.Error("bob should not have submit (not in group)")
	}
}

func TestRBACAdminPermission(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")

	role := mustRole(t, db, "super", []string{"admin"})
	mustRoleBinding(t, db, role.ID, "account", alice.ID, "*")

	// "admin" permission in RBAC grants everything.
	if !s.can(alice, "foo", "refs/heads/main", PermRead) {
		t.Error("admin perm should grant read")
	}
	if !s.can(alice, "foo", "refs/heads/main", PermSubmit) {
		t.Error("admin perm should grant submit")
	}
	if !s.can(alice, "foo", "refs/heads/main", PermEditAccess) {
		t.Error("admin perm should grant editAccess")
	}
}

func TestRBACFallbackToLegacy(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")
	devs := mustPermGroup(t, db, "developers")
	mustGroupMember(t, db, devs.ID, alice.ID)

	// Legacy rule grants read.
	mustAccessRule(t, db, "foo", "refs/heads/*", PermRead, devs.ID, "ALLOW", 0, 0)

	// RBAC role with different scope (doesn't match foo).
	role := mustRole(t, db, "other", []string{"admin"})
	mustRoleBinding(t, db, role.ID, "account", alice.ID, "bar")

	// Should fall back to legacy rules.
	if !s.can(alice, "foo", "refs/heads/main", PermRead) {
		t.Error("should fall back to legacy access_rules")
	}
}

func TestRBACMultipleRolesMerge(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	mustPermProject(t, db, "foo")

	reader := mustRole(t, db, "reader", []string{"read"})
	mustRoleBinding(t, db, reader.ID, "account", alice.ID, "*")
	reviewer := mustRole(t, db, "reviewer", []string{"comment", "push"})
	mustRoleBinding(t, db, reviewer.ID, "account", alice.ID, "*")

	// Permissions from both roles should merge.
	if !s.can(alice, "foo", "refs/heads/main", PermRead) {
		t.Error("should have read from reader role")
	}
	if !s.can(alice, "foo", "refs/heads/main", PermPush) {
		t.Error("should have push from reviewer role")
	}
	if !s.can(alice, "foo", "refs/heads/main", PermComment) {
		t.Error("should have comment from reviewer role")
	}
}

// ---------- ScopeMatches unit tests ----------

func TestScopeMatches(t *testing.T) {
	cases := []struct {
		scope   string
		project string
		want    bool
	}{
		{"*", "anything", true},
		{"", "anything", true},
		{"foo", "foo", true},
		{"foo", "foo/bar", true},
		{"foo", "foobar", false},
		{"rk/*", "rk/kernel", true},
		{"rk/*", "rk/device/rk3576", true},
		{"rk/*", "rk", true},
		{"rk/*", "docs/android", false},
		{"rk/kernel", "rk/kernel", true},
		{"rk/kernel", "rk/device", false},
	}
	for _, c := range cases {
		if got := store.ScopeMatches(c.scope, c.project); got != c.want {
			t.Errorf("ScopeMatches(%q, %q) = %v, want %v", c.scope, c.project, got, c.want)
		}
	}
}

// ---------- canCapability tests ----------

func TestCanCapability(t *testing.T) {
	db := openPermTestDB(t)
	s := permTestServer(db)
	alice := mustPermAccount(t, db, "alice", false)
	leads := mustPermGroup(t, db, "leads")
	mustGroupMember(t, db, leads.ID, alice.ID)
	// Grant editAccess capability to leads group.
	mustAccessRule(t, db, "*", "refs/*", PermEditAccess, leads.ID, "ALLOW", 0, 0)

	// alice is in leads: has editAccess.
	if !s.canCapability(alice, PermEditAccess) {
		t.Error("alice should have editAccess capability")
	}

	// bob is not in leads and editAccess is not a default permission.
	bob := mustPermAccount(t, db, "bob", false)
	if s.canCapability(bob, PermEditAccess) {
		t.Error("bob should not have editAccess capability")
	}

	// createProject is granted to Registered Users by default schema seed.
	if !s.canCapability(bob, PermCreateProject) {
		t.Error("bob should have createProject via default Registered Users rule")
	}
}
