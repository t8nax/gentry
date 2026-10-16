// Command fakegentry stands for a program of a release in the mock release of
// the tests of installation: on version it prints the version the build
// stamped into it, on mcp it waits for the end of its input, as the tool
// server of a session of an agent does.
//
//	go build -ldflags "-X main.version=0.2.0" ./tests/acceptance/install/testdata/fakegentry
package main

import (
	"fmt"
	"io"
	"os"
)

var version = "0.0.0"

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Printf("Версия Gentry: %s\n", version)
	case "mcp":
		io.Copy(io.Discard, os.Stdin)
	default:
		os.Exit(2)
	}
}
