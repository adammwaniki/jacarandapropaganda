// Package migrations embeds the goose SQL migrations so the binary can run
// them against the database. Migrations run manually on production deploys
// (see spec.md § Deployment); the binary does not apply them on startup.
package migrations

import "embed"

//go:embed *.sql
var Embed embed.FS
