package db

import "embed"

// Migrations embeds the SQL migration files for use with goose.
// This lives in the db/ package (next to the migrations/ directory)
// because //go:embed paths must be relative to the source file.
//
//go:embed migrations/*.sql
var Migrations embed.FS
