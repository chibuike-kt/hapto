// Package migrations embeds hapto-api's SQL migration files so the
// compiled binary carries its own schema history regardless of the
// process's working directory.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
