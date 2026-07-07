// Command Rexion is a config- and plugin-driven coding agent CLI.
package main

import (
	"os"

	"rexion/internal/cli"

	// Blank imports wire compile-time built-ins into their registries.
	_ "rexion/internal/provider/anthropic"
	_ "rexion/internal/provider/openai"
	_ "rexion/internal/tool/builtin"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
