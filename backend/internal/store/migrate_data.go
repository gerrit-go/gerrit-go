package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// CopyAll copies every table from src (SQLite) into dst (PostgreSQL), replacing
// any existing rows, and resets the destination identity sequences so new
// inserts continue after the migrated ids. It is intended for the one-time
// SQLite→PostgreSQL migration and expects dst to already have the schema
// created (store.Open does this).
//
// All destination work runs on a single connection with FK enforcement
// disabled (session_replication_role=replica) so tables can be loaded in any
// order; the connection is required because that setting is per-session.
func CopyAll(src, dst *DB) error {
	if dst.driver != DriverPostgres {
		return errors.New("destination must be a PostgreSQL database")
	}
	if src.driver == DriverPostgres {
		return errors.New("source must be a SQLite database")
	}

	tables, err := sqliteTables(src.db)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return errors.New("no tables found in source")
	}

	ctx := context.Background()
	conn, err := dst.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SET session_replication_role = replica`); err != nil {
		return fmt.Errorf("disable triggers: %w", err)
	}
	defer conn.ExecContext(ctx, `SET session_replication_role = DEFAULT`)

	// Truncate every table in one statement; listing all of them together
	// satisfies the FK "referenced table" restriction.
	quoted := make([]string, len(tables))
	for i, t := range tables {
		quoted[i] = `"` + t + `"`
	}
	if _, err := conn.ExecContext(ctx, `TRUNCATE TABLE `+strings.Join(quoted, ",")); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	for _, t := range tables {
		n, err := copyTable(ctx, src.db, conn, t)
		if err != nil {
			return fmt.Errorf("copy %s: %w", t, err)
		}
		fmt.Printf("  %-22s %d rows\n", t, n)
	}

	if err := resetSequences(ctx, conn); err != nil {
		return err
	}
	return nil
}

// sqliteTables lists user tables in a SQLite database.
func sqliteTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// copyTable copies every row from src's table into the destination connection.
func copyTable(ctx context.Context, src *sql.DB, conn *sql.Conn, table string) (int, error) {
	rows, err := src.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	if len(cols) == 0 {
		return 0, nil
	}

	quoted := make([]string, len(cols))
	placeholders := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = `"` + c + `"`
		placeholders[i] = "?"
	}
	insert := `INSERT INTO "` + table + `" (` +
		strings.Join(quoted, ",") + `) VALUES (` +
		strings.Join(placeholders, ",") + `)`

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(insert)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()

	count := 0
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			tx.Rollback()
			return 0, err
		}
		args := make([]any, len(cols))
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				args[i] = string(b)
			} else {
				args[i] = v
			}
		}
		if _, err := stmt.Exec(args...); err != nil {
			tx.Rollback()
			return 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

// resetSequences advances every PostgreSQL identity sequence past the largest
// migrated id so subsequent inserts do not collide.
func resetSequences(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE is_identity = 'YES'`)
	if err != nil {
		return fmt.Errorf("list identities: %w", err)
	}
	type ident struct{ table, column string }
	var ids []ident
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, ident{t, c})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range ids {
		q := fmt.Sprintf(
			`SELECT setval(pg_get_serial_sequence('%s','%s'), COALESCE((SELECT MAX("%s") FROM "%s"), 0) + 1, false)`,
			id.table, id.column, id.column, id.table)
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("reset sequence %s.%s: %w", id.table, id.column, err)
		}
	}
	return nil
}
