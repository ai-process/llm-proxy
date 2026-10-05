// Package migrations carries this service's own DDL, embedded so the binary
// applies it at startup instead of waiting for an operator to run psql.
//
// Every file here must be idempotent (IF NOT EXISTS, or a DO block that
// swallows duplicate_object): a replica that crashed mid-run re-executes the
// file it was applying.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.sql
var files embed.FS

// FS exposes the embedded SQL. Files apply in filename order, which is why
// they are named with a sortable timestamp prefix.
func FS() fs.FS { return files }
