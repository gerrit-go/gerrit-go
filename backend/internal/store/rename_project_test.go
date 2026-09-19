package store

import (
	"testing"
)

func countWhere(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func TestRenameProjectRewritesAllReferences(t *testing.T) {
	db := openTemp(t)
	acct := mustAccount(t, db, "u1", "U One")
	reg, err := db.GetGroupByName("Registered Users")
	if err != nil {
		t.Fatalf("registered group: %v", err)
	}

	const oldName, newName = "old/child", "rk/Linux/child"
	if err := db.CreateProject(&Project{Name: "old"}); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := db.CreateProject(&Project{Name: oldName, Parent: "old"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.CreateProject(&Project{Name: "keepme", Parent: oldName}); err != nil {
		t.Fatalf("create dependent: %v", err)
	}
	owners := &Group{Name: oldName + " Owners"}
	if err := db.CreateGroup(owners); err != nil {
		t.Fatalf("create owners group: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO changes(project, branch, change_id, subject, owner_id, created, updated)
		 VALUES(?,?,?,?,?,?,?)`, oldName, "refs/heads/master", "Iabc123", "subj", acct.ID, now(), now()); err != nil {
		t.Fatalf("seed change: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO access_rules(project, ref_pattern, permission, group_id) VALUES(?,?,?,?)`,
		oldName, "refs/*", "read", reg.ID); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	if err := db.SetProjectLabel(oldName, "os", "linux"); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO submit_requirements(project, label, min_value, block_value) VALUES(?,?,2,-2)`,
		oldName, "Code-Review"); err != nil {
		t.Fatalf("seed submit req: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO watched_projects(account_id, project, added) VALUES(?,?,?)`,
		acct.ID, oldName, now()); err != nil {
		t.Fatalf("seed watch: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO webhooks(project, url, created) VALUES(?,?,?)`,
		oldName, "http://example/hook", now()); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	role := &Role{Name: "tester", DisplayName: "Tester", Permissions: []string{"read"}}
	if err := db.CreateRole(role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	exactScope := &RoleBinding{RoleID: role.ID, SubjectType: "account", SubjectID: acct.ID, Scope: oldName}
	prefixScope := &RoleBinding{RoleID: role.ID, SubjectType: "group", SubjectID: reg.ID, Scope: oldName + "/*"}
	otherScope := &RoleBinding{RoleID: role.ID, SubjectType: "group", SubjectID: reg.ID, Scope: "rk/*"}
	for _, rb := range []*RoleBinding{exactScope, prefixScope, otherScope} {
		if err := db.CreateRoleBinding(rb); err != nil {
			t.Fatalf("create binding: %v", err)
		}
	}

	if err := db.RenameProject(oldName, newName); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if _, err := db.GetProject(oldName); err == nil {
		t.Error("old project name still present")
	}
	p, err := db.GetProject(newName)
	if err != nil {
		t.Fatalf("new project missing: %v", err)
	}
	if p.Parent != "old" {
		t.Errorf("renamed project parent = %q, want old", p.Parent)
	}
	dep, err := db.GetProject("keepme")
	if err != nil {
		t.Fatalf("dependent project missing: %v", err)
	}
	if dep.Parent != newName {
		t.Errorf("dependent parent = %q, want %q", dep.Parent, newName)
	}

	for _, tbl := range projectRefTables {
		if n := countWhere(t, db, `SELECT COUNT(*) FROM `+tbl+` WHERE project=?`, newName); n == 0 {
			t.Errorf("%s: no rows referencing new name", tbl)
		}
		if n := countWhere(t, db, `SELECT COUNT(*) FROM `+tbl+` WHERE project=?`, oldName); n != 0 {
			t.Errorf("%s: %d rows still reference old name", tbl, n)
		}
	}

	if n := countWhere(t, db, `SELECT COUNT(*) FROM role_bindings WHERE scope=?`, newName); n != 1 {
		t.Errorf("exact-scope binding not rewritten (count=%d)", n)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM role_bindings WHERE scope=?`, newName+"/*"); n != 1 {
		t.Errorf("prefix-scope binding not rewritten (count=%d)", n)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM role_bindings WHERE scope='rk/*'`); n != 1 {
		t.Error("unrelated binding should be untouched")
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM groups WHERE name=?`, newName+" Owners"); n != 1 {
		t.Error("owners group not renamed")
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM groups WHERE name=?`, oldName+" Owners"); n != 0 {
		t.Error("old owners group still present")
	}
}

func TestRenameProjectNoOps(t *testing.T) {
	db := openTemp(t)
	if err := db.CreateProject(&Project{Name: "solo"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.RenameProject("solo", "solo"); err == nil {
		t.Error("same-name rename should fail")
	}
	if err := db.RenameProject("", "x"); err == nil {
		t.Error("empty old name should fail")
	}
	if _, err := db.GetProject("solo"); err != nil {
		t.Errorf("project should be untouched: %v", err)
	}
}
