// Command migrate-db performs a one-time copy of all data from a SQLite
// database into a PostgreSQL database for the gerrit-go backend.
//
// Usage:
//
//	migrate-db -from /path/to/gerrit.db -to 'postgres://user:pass@host:5432/gerrit?sslmode=disable'
//
// The destination schema is created if missing, every table is replaced with
// the source rows, and identity sequences are advanced past the migrated ids.
package main

import (
	"flag"
	"log"

	"gerrit-go/internal/store"
)

func main() {
	from := flag.String("from", "", "source SQLite database file")
	to := flag.String("to", "", "destination PostgreSQL DSN (postgres://...)")
	flag.Parse()

	if *from == "" || *to == "" {
		log.Fatalf("both -from and -to are required")
	}
	if !store.IsPostgresDSN(*to) {
		log.Fatalf("-to must be a postgres:// or postgresql:// DSN")
	}

	src, err := store.Open(*from)
	if err != nil {
		log.Fatalf("open source: %v", err)
	}
	defer src.Close()

	dst, err := store.Open(*to)
	if err != nil {
		log.Fatalf("open destination: %v", err)
	}
	defer dst.Close()

	log.Printf("migrating %s -> PostgreSQL", *from)
	if err := store.CopyAll(src, dst); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("migration complete")
}
