// Command sshm is a personal TUI for managing SSH.
package main

import (
	"os"

	"sshm/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
