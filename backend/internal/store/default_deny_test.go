package store

import "testing"

func globalRuleCount(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM access_rules WHERE project='*'`).Scan(&n); err != nil {
		t.Fatalf("count global rules: %v", err)
	}
	return n
}

func TestFreshDBIsDefaultDeny(t *testing.T) {
	db := openTemp(t)
	if got := db.PermissionModel(); got != "default-deny-v1" {
		t.Fatalf("PermissionModel = %q, want default-deny-v1", got)
	}
	// Only the Administrators read rule is seeded globally.
	if n := globalRuleCount(t, db); n != 1 {
		t.Errorf("global rule count = %d, want 1", n)
	}
	reg, err := db.GetGroupByName("Registered Users")
	if err != nil {
		t.Fatalf("registered group: %v", err)
	}
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM access_rules WHERE project='*' AND group_id=?`, reg.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("Registered Users should hold no global rules under default-deny, got %d", n)
	}
}

func TestMigrateDefaultDenyRemovesLegacySeeds(t *testing.T) {
	db := openTemp(t)
	reg, err := db.GetGroupByName("Registered Users")
	if err != nil {
		t.Fatalf("registered group: %v", err)
	}
	anon, err := db.GetGroupByName("Anonymous Users")
	if err != nil {
		t.Fatalf("anonymous group: %v", err)
	}

	// Reset the gate and re-create the old permissive seeds, plus one
	// hand-written rule that must survive the migration.
	if _, err := db.db.Exec(`UPDATE app_meta SET value='' WHERE key=?`, permissionModelMetaKey); err != nil {
		t.Fatalf("reset gate: %v", err)
	}
	insert := func(ref, perm string, gid int64, action string) {
		t.Helper()
		if _, err := db.db.Exec(
			`INSERT INTO access_rules(project, ref_pattern, permission, group_id, action, exclusive, min_val, max_val)
			 VALUES('*',?,?,?,?,0,0,0)`, ref, perm, gid, action); err != nil {
			t.Fatalf("insert seed: %v", err)
		}
	}
	insert("refs/*", "read", reg.ID, "ALLOW")
	insert("refs/*", "read", anon.ID, "ALLOW")
	insert("refs/for/*", "push", reg.ID, "ALLOW")
	// hand-written: same group but a permission that is not in the seed list
	insert("refs/*", "submit", reg.ID, "ALLOW")
	// hand-written: same tuple but DENY action must also survive untouched logic
	// (seeds only match ALLOW)

	if err := migrateDefaultDeny(db.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if got := db.PermissionModel(); got != "default-deny-v1" {
		t.Errorf("PermissionModel = %q after migration", got)
	}
	var n int
	if err := db.db.QueryRow(
		`SELECT COUNT(*) FROM access_rules WHERE project='*' AND ref_pattern='refs/*' AND permission='read' AND group_id=?`,
		reg.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Error("permissive Registered Users read seed should be removed")
	}
	if err := db.db.QueryRow(
		`SELECT COUNT(*) FROM access_rules WHERE project='*' AND permission='submit'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("hand-written submit rule should survive, got %d rows", n)
	}
	// The migration records itself in the audit log.
	if err := db.db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE action='permission-model-default-deny'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Errorf("audit entries = %d, want 1", n)
	}

	// Idempotent: a second run changes nothing.
	before := globalRuleCount(t, db)
	if err := migrateDefaultDeny(db.db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if after := globalRuleCount(t, db); after != before {
		t.Errorf("rule count changed on re-run: %d -> %d", before, after)
	}
}
