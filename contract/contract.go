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
// The schemas that refer to each other are generated together, each into a
// file of its own; a schema shared by two groups is written the same by both.
//go:generate go tool go-jsonschema -p contract -t --tags json --schema-output https://github.com/t8nax/gentry/contract/schemas/applied.json=applied.go --schema-output https://github.com/t8nax/gentry/contract/schemas/process-item.json=process_item.go --schema-output https://github.com/t8nax/gentry/contract/schemas/sync.json=sync.go --schema-output https://github.com/t8nax/gentry/contract/schemas/flow-problem.json=flow_problem.go --schema-output https://github.com/t8nax/gentry/contract/schemas/flow-invalid.json=flow_invalid.go --schema-output https://github.com/t8nax/gentry/contract/schemas/flow-draft-invalid.json=flow_draft_invalid.go --schema-output https://github.com/t8nax/gentry/contract/schemas/library-draft-invalid.json=library_draft_invalid.go --schema-output https://github.com/t8nax/gentry/contract/schemas/flow-show.json=flow_show.go --schema-output https://github.com/t8nax/gentry/contract/schemas/flow-diff.json=flow_diff.go --schema-output https://github.com/t8nax/gentry/contract/schemas/flow-apply.json=flow_apply.go --schema-output https://github.com/t8nax/gentry/contract/schemas/library-diff.json=library_diff.go --schema-output https://github.com/t8nax/gentry/contract/schemas/library-apply.json=library_apply.go --schema-output https://github.com/t8nax/gentry/contract/schemas/process-remote.json=process_remote.go --schema-output https://github.com/t8nax/gentry/contract/schemas/process-sync.json=process_sync.go --schema-output https://github.com/t8nax/gentry/contract/schemas/process-status.json=process_status.go --schema-output https://github.com/t8nax/gentry/contract/schemas/events/process.synced.json=event_process_synced.go schemas/applied.json schemas/process-item.json schemas/sync.json schemas/flow-problem.json schemas/flow-invalid.json schemas/flow-draft-invalid.json schemas/library-draft-invalid.json schemas/flow-show.json schemas/flow-diff.json schemas/flow-apply.json schemas/library-diff.json schemas/library-apply.json schemas/process-remote.json schemas/process-sync.json schemas/process-status.json schemas/events/process.synced.json
//go:generate go tool go-jsonschema -p contract -t --tags json --schema-output https://github.com/t8nax/gentry/contract/schemas/applied.json=applied.go --schema-output https://github.com/t8nax/gentry/contract/schemas/process-item.json=process_item.go --schema-output https://github.com/t8nax/gentry/contract/schemas/sync.json=sync.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task.json=task.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-take.json=task_take.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-show.json=task_show.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-list.json=task_list.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-pass.json=task_pass.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-step.json=task_step.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-note.json=task_note.go --schema-output https://github.com/t8nax/gentry/contract/schemas/task-artifact.json=task_artifact.go --schema-output https://github.com/t8nax/gentry/contract/schemas/stage-close.json=stage_close.go --schema-output https://github.com/t8nax/gentry/contract/schemas/step-list.json=step_list.go --schema-output https://github.com/t8nax/gentry/contract/schemas/note-add.json=note_add.go --schema-output https://github.com/t8nax/gentry/contract/schemas/note-list.json=note_list.go --schema-output https://github.com/t8nax/gentry/contract/schemas/artifact-save.json=artifact_save.go --schema-output https://github.com/t8nax/gentry/contract/schemas/question-option.json=question_option.go --schema-output https://github.com/t8nax/gentry/contract/schemas/operator-decision.json=operator_decision.go --schema-output https://github.com/t8nax/gentry/contract/schemas/operator-record.json=operator_record.go --schema-output https://github.com/t8nax/gentry/contract/schemas/operator-record-input.json=operator_record_input.go --schema-output https://github.com/t8nax/gentry/contract/schemas/events/operator_decision.recorded.json=event_operator_decision_recorded.go schemas/applied.json schemas/process-item.json schemas/sync.json schemas/task.json schemas/task-take.json schemas/task-show.json schemas/task-list.json schemas/task-pass.json schemas/task-step.json schemas/task-note.json schemas/task-artifact.json schemas/stage-close.json schemas/step-list.json schemas/note-add.json schemas/note-list.json schemas/artifact-save.json schemas/question-option.json schemas/operator-decision.json schemas/operator-record.json schemas/operator-record-input.json schemas/events/operator_decision.recorded.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o flow_discard.go schemas/flow-discard.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o library_discard.go schemas/library-discard.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_library_applied.go schemas/events/library.applied.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_library_draft_discarded.go schemas/events/library.draft_discarded.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_process_remote_set.go schemas/events/process.remote_set.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_project_added.go schemas/events/project.added.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_worktree_added.go schemas/events/worktree.added.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_flow_applied.go schemas/events/flow.applied.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_flow_draft_discarded.go schemas/events/flow.draft_discarded.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o task_take_input.go schemas/task-take-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_task_taken.go schemas/events/task.taken.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o stage_show.go schemas/stage-show.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o stage_exit_input.go schemas/stage-exit-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o stage_skip_input.go schemas/stage-skip-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o step_add_input.go schemas/step-add-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o step_done_input.go schemas/step-done-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o step_drop_input.go schemas/step-drop-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o note_add_input.go schemas/note-add-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o artifact_save_input.go schemas/artifact-save-input.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_stage_exited.go schemas/events/stage.exited.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_stage_skipped.go schemas/events/stage.skipped.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_step_added.go schemas/events/step.added.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_step_done.go schemas/events/step.done.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_step_dropped.go schemas/events/step.dropped.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_note_added.go schemas/events/note.added.json
//go:generate go tool go-jsonschema -p contract -t --tags json -o event_artifact_saved.go schemas/events/artifact.saved.json

// Version is the contract version. It grows only on an incompatible change:
// a field removed, renamed or retyped. It stays 0 until the first release of
// Gentry, while the contract may still change freely.
const Version = 0

// Schemas holds the JSON schemas of the contract, one file per document. The
// data of each event type has its own schema in schemas/events/<type>.json.
//
//go:embed schemas/*.json schemas/events/*.json
var Schemas embed.FS
