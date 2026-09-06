// skillc -- entry point for the skill compiler.
//
// The engine lives in internal/, one thesis per package. This file only
// forwards to the CLI so the compiled binary has a main.
package main

import (
	"os"

	"github.com/williamfzc/skill-compiler/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
