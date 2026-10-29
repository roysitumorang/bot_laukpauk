// Package migrations embeds the SQL schema.
package migrations

import _ "embed"

// Init is the idempotent schema applied at startup.
//
//go:embed 001_init.sql
var Init string
