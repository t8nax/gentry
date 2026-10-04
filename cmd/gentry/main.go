// Command gentry is the Gentry core.
package main

import (
	"os"

	"github.com/t8nax/gentry/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Env{Stdout: os.Stdout, Stderr: os.Stderr}))
}
