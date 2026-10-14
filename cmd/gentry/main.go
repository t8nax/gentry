// Command gentry is the Gentry core.
package main

import (
	"os"

	"github.com/t8nax/gentry/internal/cli"
)

// run runs a command line. The build for the acceptance tests sets it to
// runTest, which adds the entries of the tests: see acceptance.go.
var run = cli.Run

func main() {
	os.Exit(run(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
