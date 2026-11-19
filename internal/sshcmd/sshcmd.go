// Package sshcmd builds ssh command arguments and replaces the current process with it.
package sshcmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// ConfigArgs returns ["-F", path] when SSHM_CONFIG is set.
func ConfigArgs() []string {
	if c := os.Getenv("SSHM_CONFIG"); c != "" {
		return []string{"-F", c}
	}
	return nil
}

// Args returns ssh arguments to connect to an alias.
func Args(alias string) []string {
	args := append([]string{"ssh"}, ConfigArgs()...)
	return append(args, "--", alias)
}

// Exec replaces the process with argv (argv[0] is looked up in PATH).
func Exec(argv []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	if err := syscall.Exec(path, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", argv[0], err)
	}
	return nil
}
