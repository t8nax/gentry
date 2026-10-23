// Command gatedclaude fails the install of one setup at a file barrier,
// before the ordinary fake reads its state. Other commands use fakeclaude.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	args := os.Args[1:]
	gate := os.Getenv("FAKECLAUDE_INSTALL_GATE")
	if gate != "" && len(args) > 1 && args[0] == "plugin" && args[1] == "install" {
		if err := os.WriteFile(filepath.Join(gate, "entered"), nil, 0o644); err != nil {
			panic(err)
		}
		deadline := time.Now().Add(2 * time.Minute)
		for {
			if _, err := os.Stat(filepath.Join(gate, "release")); err == nil {
				fmt.Println(`{"message":"simulated gated install failure"}`)
				os.Exit(1)
			}
			if time.Now().After(deadline) {
				panic("install barrier was not released")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	cmd := exec.Command(os.Getenv("FAKECLAUDE_DELEGATE"), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			os.Exit(e.ExitCode())
		}
		panic(err)
	}
}
