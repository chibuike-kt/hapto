// Command migrate applies or reverses hapto-api's database migrations
// independent of the server process.
//
// Usage:
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate down
//	go run ./cmd/migrate up -database postgres://user:pass@host:5432/db?sslmode=disable
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/chibuike-kt/hapto-api/internal/migrate"
)

const defaultDatabaseURL = "postgres://hapto:hapto@localhost:5432/hapto?sslmode=disable" //nolint:gosec // local-only dev default, matches docker-compose.yml

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbURL := fs.String("database", "", "Postgres connection string (defaults to $DATABASE_URL)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	url := *dbURL
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		url = defaultDatabaseURL
	}

	var err error
	switch cmd {
	case "up":
		err = migrate.Up(url)
	case "down":
		err = migrate.Down(url)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("migrate %s: %v", cmd, err) //nolint:gosec // cmd is a CLI arg from the operator, not untrusted input
	}

	fmt.Printf("migrate %s: ok\n", cmd)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate <up|down> [-database <url>]")
}
