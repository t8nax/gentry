// Command failclaudecleanup delegates to the regular fake Claude but also
// fails marketplace removal, independently of its primary failure setting.
package main

import (
	"errors"
	"os"
	"os/exec"
)

func main() {
	args := os.Args[1:]
	if len(args) >= 3 && args[0] == "plugin" && args[1] == "marketplace" && args[2] == "remove" {
		os.Setenv("FAKECLAUDE_FAIL", "marketplace-remove")
	}
	cmd := exec.Command(os.Getenv("GENTRY_TEST_CLAUDE_DELEGATE"), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		os.Exit(2)
	}
}
