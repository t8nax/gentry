// Package contract is the interface between gentry and its clients, such as
// Gentry Desk: JSON schemas of command input and output and the Go types
// generated from them.
//
// The output of every `--json` command is exactly one JSON object followed by
// a newline on stdout, in UTF-8, with snake_case field names. The one exception
// is `gentry events`: one JSON line per event, as a stream (Event), with or
// without --json, as it has no text output; its failure is still a single
// ErrorOutput when --json is given. A successful
// result is printed as is, without an envelope; the exit code tells success (0)
// from failure. Clients must ignore unknown fields: adding fields and commands
// is a compatible change.
package contract

import "embed"

//go:generate go tool go-jsonschema -p contract -t --tags json -o version.go schemas/version.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o error.go schemas/error.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event.go schemas/event.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o setup.go schemas/setup.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o project_add.go schemas/project-add.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o project_list.go schemas/project-list.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o worktree_add.go schemas/worktree-add.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o worktree_list.go schemas/worktree-list.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o flow_show.go schemas/flow-show.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o flow_invalid.go schemas/flow-invalid.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_project_added.go schemas/events/project.added.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_worktree_added.go schemas/events/worktree.added.json

// Version is the contract version. It grows only on an incompatible change:
// a field removed, renamed or retyped. It stays 0 until the first release of
// Gentry, while the contract may still change freely.
const Version = 0

// Schemas holds the JSON schemas of the contract, one file per document. The
// data of each event type has its own schema in schemas/events/<type>.json.
//
//go:embed schemas/*.json schemas/events/*.json
var Schemas embed.FS
