package store

import (
	"database/sql"
	"time"
)

// Team is an organizational unit of accounts with an optional leader.
// Access is granted by binding roles to a team (role_bindings subject_type
// "team"); a team member inherits every binding of that team. Each team also
// owns a mirror group (GroupID) so it can be used as a subject in per-project
// access_rules; the group membership is kept in sync automatically.
type Team struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	LeaderID    int64     `json:"leader_id"`
	GroupID     int64     `json:"group_id"`
	Created     time.Time `json:"created"`
	MemberCount int       `json:"member_count"`
	// Enriched by the API layer, not by the store queries.
	LeaderName string   `json:"leader_name,omitempty"`
	Scopes     []string `json:"scopes,omitempty"`
	CanManage  bool     `json:"can_manage,omitempty"`
}

// TeamMirrorName is the reserved group-name prefix that maps a team into the
// group-based access_rules engine.
func TeamMirrorName(teamName string) string { return "team:" + teamName }

// backfillTeamMirrors creates mirror groups for teams that predate the
// team-ACL feature and syncs their membership. Runs once at startup.
func backfillTeamMirrors(db *sql.DB, drv string) error {
	rows, err := db.Query(`SELECT id, name FROM teams WHERE group_id=0`)
	if err != nil {
		return err
	}
	type tref struct {
		id   int64
		name string
	}
	var todo []tref
	for rows.Next() {
		var r tref
		if err := rows.Scan(&r.id, &r.name); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range todo {
		mirror := TeamMirrorName(r.name)
		var gid int64
		if err := db.QueryRow(`SELECT id FROM groups WHERE name=?`, mirror).Scan(&gid); err != nil {
			if drv == DriverPostgres {
				if err := db.QueryRow(
					`INSERT INTO groups(name, description, owner_group_id, system, created) VALUES(?,?,0,1,?) RETURNING id`,
					mirror, "Mirror group of team "+r.name, now()).Scan(&gid); err != nil {
					return err
				}
			} else {
				res, err := db.Exec(
					`INSERT INTO groups(name, description, owner_group_id, system, created) VALUES(?,?,0,1,?)`,
					mirror, "Mirror group of team "+r.name, now())
				if err != nil {
					return err
				}
				if gid, err = res.LastInsertId(); err != nil {
					return err
				}
			}
		}
		if _, err := db.Exec(`UPDATE teams SET group_id=? WHERE id=?`, gid, r.id); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO group_members(group_id, account_id, added)
			SELECT ?, account_id, ? FROM team_members WHERE team_id=?
			ON CONFLICT(group_id, account_id) DO NOTHING`, gid, now(), r.id); err != nil {
			return err
		}
	}
	return nil
}

// TeamMember links an account to a team.
type TeamMember struct {
	TeamID    int64     `json:"team_id"`
	AccountID int64     `json:"account_id"`
	Joined    time.Time `json:"joined"`
}

func (d *DB) CreateTeam(t *Team) error {
	id, err := d.insertID(
		`INSERT INTO teams(name, display_name, description, leader_id, created) VALUES(?,?,?,?,?)`,
		"id", t.Name, t.DisplayName, t.Description, t.LeaderID, now())
	if err != nil {
		return err
	}
	t.ID = id
	if err := d.ensureTeamMirror(t); err != nil {
		return err
	}
	if t.LeaderID > 0 {
		if err := d.AddTeamMember(id, t.LeaderID); err != nil {
			return err
		}
	}
	return nil
}

// ensureTeamMirror attaches (or creates) the mirror group for a team and
// renames it after a team rename.
func (d *DB) ensureTeamMirror(t *Team) error {
	want := TeamMirrorName(t.Name)
	if t.GroupID == 0 {
		if g, err := d.GetGroupByName(want); err == nil {
			t.GroupID = g.ID
		} else {
			g := &Group{Name: want, Description: "Mirror group of team " + t.Name, System: true}
			if err := d.CreateGroup(g); err != nil {
				return err
			}
			t.GroupID = g.ID
		}
		if _, err := d.db.Exec(`UPDATE teams SET group_id=? WHERE id=?`, t.GroupID, t.ID); err != nil {
			return err
		}
		return nil
	}
	g, err := d.GetGroup(t.GroupID)
	if err == nil && g.Name != want {
		if _, err := d.db.Exec(`UPDATE groups SET name=? WHERE id=?`, want, g.ID); err != nil {
			return err
		}
		d.perm.invalidateRules()
	}
	return nil
}

func (d *DB) UpdateTeam(t *Team) error {
	_, err := d.db.Exec(`UPDATE teams SET name=?, display_name=?, description=?, leader_id=? WHERE id=?`,
		t.Name, t.DisplayName, t.Description, t.LeaderID, t.ID)
	if err != nil {
		return err
	}
	if err := d.ensureTeamMirror(t); err != nil {
		return err
	}
	if t.LeaderID > 0 {
		return d.AddTeamMember(t.ID, t.LeaderID)
	}
	return nil
}

func (d *DB) DeleteTeam(id int64) error {
	var groupID int64
	_ = d.db.QueryRow(`SELECT group_id FROM teams WHERE id=?`, id).Scan(&groupID)
	if _, err := d.db.Exec(`DELETE FROM role_bindings WHERE subject_type='team' AND subject_id=?`, id); err != nil {
		return err
	}
	if _, err := d.db.Exec(`DELETE FROM team_members WHERE team_id=?`, id); err != nil {
		return err
	}
	if _, err := d.db.Exec(`DELETE FROM teams WHERE id=?`, id); err != nil {
		return err
	}
	if groupID > 0 {
		// access_rules rows referencing the mirror group cascade away.
		if _, err := d.db.Exec(`DELETE FROM groups WHERE id=?`, groupID); err != nil {
			return err
		}
		d.perm.invalidateAllGroups()
		d.perm.invalidateRules()
	}
	return nil
}

func (d *DB) GetTeam(id int64) (*Team, error) {
	return d.scanTeam(d.db.QueryRow(teamSelect+` WHERE t.id=? GROUP BY t.id`, id))
}

const teamSelect = `SELECT t.id, t.name, t.display_name, t.description, t.leader_id, t.group_id, t.created,
		COUNT(tm.account_id)
	FROM teams t LEFT JOIN team_members tm ON tm.team_id = t.id`

func (d *DB) scanTeam(row interface{ Scan(...any) error }) (*Team, error) {
	t := &Team{}
	var created string
	var groupID sql.NullInt64
	if err := row.Scan(&t.ID, &t.Name, &t.DisplayName, &t.Description, &t.LeaderID, &groupID, &created, &t.MemberCount); err != nil {
		return nil, err
	}
	t.GroupID = groupID.Int64
	t.Created = parseTime(created)
	return t, nil
}

func (d *DB) scanTeams(rows *sql.Rows) ([]*Team, error) {
	var out []*Team
	for rows.Next() {
		t, err := d.scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) ListTeams() ([]*Team, error) {
	rows, err := d.db.Query(teamSelect + ` GROUP BY t.id ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return d.scanTeams(rows)
}

// ListTeamsForAccount returns the teams the account belongs to.
func (d *DB) ListTeamsForAccount(accountID int64) ([]*Team, error) {
	rows, err := d.db.Query(teamSelect+` WHERE tm.account_id=? GROUP BY t.id ORDER BY t.name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return d.scanTeams(rows)
}

// TeamIDsForAccount returns the set of team IDs the account is a member of.
func (d *DB) TeamIDsForAccount(accountID int64) (map[int64]bool, error) {
	rows, err := d.db.Query(`SELECT team_id FROM team_members WHERE account_id=?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (d *DB) AddTeamMember(teamID, accountID int64) error {
	_, err := d.db.Exec(`INSERT INTO team_members(team_id, account_id, joined) VALUES(?,?,?)
		 ON CONFLICT(team_id, account_id) DO NOTHING`, teamID, accountID, now())
	if err != nil {
		return err
	}
	return d.syncMirrorMember(teamID, accountID, true)
}

// RemoveTeamMember drops the account from the team and clears the leader
// field if the removed member was the team leader.
func (d *DB) RemoveTeamMember(teamID, accountID int64) error {
	if _, err := d.db.Exec(`DELETE FROM team_members WHERE team_id=? AND account_id=?`, teamID, accountID); err != nil {
		return err
	}
	if _, err := d.db.Exec(`UPDATE teams SET leader_id=0 WHERE id=? AND leader_id=?`, teamID, accountID); err != nil {
		return err
	}
	return d.syncMirrorMember(teamID, accountID, false)
}

// syncMirrorMember keeps the team mirror group's membership in step with the
// team so access_rules evaluations see the same people.
func (d *DB) syncMirrorMember(teamID, accountID int64, add bool) error {
	var groupID int64
	if err := d.db.QueryRow(`SELECT group_id FROM teams WHERE id=?`, teamID).Scan(&groupID); err != nil {
		return nil // team vanished (delete path) — nothing to sync
	}
	if groupID == 0 {
		return nil
	}
	if add {
		return d.AddGroupMember(groupID, accountID)
	}
	return d.RemoveGroupMember(groupID, accountID)
}

// LeadsAnyTeam reports whether the account is the leader of at least one team.
func (d *DB) LeadsAnyTeam(accountID int64) bool {
	var n int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM teams WHERE leader_id=?`, accountID).Scan(&n)
	return n > 0
}

func (d *DB) ListTeamMembers(teamID int64) ([]*TeamMember, error) {
	rows, err := d.db.Query(`SELECT team_id, account_id, joined FROM team_members WHERE team_id=? ORDER BY joined, account_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TeamMember
	for rows.Next() {
		m := &TeamMember{}
		var joined string
		if err := rows.Scan(&m.TeamID, &m.AccountID, &joined); err != nil {
			return nil, err
		}
		m.Joined = parseTime(joined)
		out = append(out, m)
	}
	return out, rows.Err()
}
