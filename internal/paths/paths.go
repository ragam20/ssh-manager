// Package paths defines file locations used by sshm.
package paths

import (
	"os"
	"path/filepath"

	"sshm/internal/store"
)

type Paths struct {
	SSHDir     string // ~/.ssh
	Config     string // SSHM_CONFIG or ~/.ssh/config
	KnownHosts string
	State      string // state.json milik sshm
	BackupDir  string // config/known_hosts backups before modification
}

func Default() Paths {
	home, _ := os.UserHomeDir()
	ssh := filepath.Join(home, ".ssh")
	cfg := os.Getenv("SSHM_CONFIG")
	if cfg == "" {
		cfg = filepath.Join(ssh, "config")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return Paths{
		SSHDir:     ssh,
		Config:     cfg,
		KnownHosts: filepath.Join(ssh, "known_hosts"),
		State:      store.DefaultPath(),
		BackupDir:  filepath.Join(state, "sshm", "backups"),
	}
}
