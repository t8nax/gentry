// Package contract is the interface between gentry and its clients, such as
// Gentry Desk: JSON schemas of command input and output and the Go types
// generated from them.
//
// The output of every `--json` command is exactly one JSON object followed by
// a newline on stdout, in UTF-8, with snake_case field names. A successful
// result is printed as is, without an envelope; the exit code tells success (0)
// from failure. Clients must ignore unknown fields: adding fields and commands
// is a compatible change.
package contract

import "embed"

//go:generate go tool go-jsonschema -p contract -t --tags json -o version.go schemas/version.json

// Version is the contract version. It grows only on an incompatible change:
// a field removed, renamed or retyped. It stays 0 until the first release of
// Gentry, while the contract may still change freely.
const Version = 0

// Schemas holds the JSON schemas of the contract, one file per document.
//
//go:embed schemas/*.json
var Schemas embed.FS
