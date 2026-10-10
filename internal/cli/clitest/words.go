package clitest

import "github.com/t8nax/gentry/internal/msg"

// The words of the catalog the tests of several groups build their outputs
// from: the tests of the output in package cli state them.

// None is the mark of a missing value.
var None = msg.Text(msg.ValueNone)

// Round names a stage of the shop by its title and identifier with the round
// of its pass, such as «Ветка (branch), круг 1».
func Round(title, stage string, n int) string {
	return msg.Text(msg.StageRound, msg.Text(msg.TaskNamed, title, stage), n)
}
