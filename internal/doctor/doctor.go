// Package doctor checks the SSH environment and reports common issues.
package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"sshm/internal/agent"
	"sshm/internal/keys"
	"sshm/internal/knownhosts"
	"sshm/internal/paths"
	"sshm/internal/sshconfig"
)

type report struct {
	w    io.Writer
	fail int
	warn int
}

func (r *report) ok(f string, a ...any)    { fmt.Fprintf(r.w, "  ✓ "+f+"\n", a...) }
func (r *report) warnf(f string, a ...any) { r.warn++; fmt.Fprintf(r.w, "  ! "+f+"\n", a...) }
func (r *report) bad(f string, a ...any)   { r.fail++; fmt.Fprintf(r.w, "  ✗ "+f+"\n", a...) }
func (r *report) head(s string)            { fmt.Fprintf(r.w, "\n%s\n", s) }

// Run writes a report to w and returns the exit code (1 if there are errors).
func Run(w io.Writer, p paths.Paths) int {
	r := &report{w: w}

	r.head("Perintah")
	for _, b := range []string{"ssh", "ssh-add", "ssh-agent", "ssh-keygen"} {
		if path, err := exec.LookPath(b); err == nil {
			r.ok("%s (%s)", b, path)
		} else {
			r.bad("%s not found di PATH", b)
		}
	}
	if _, err := exec.LookPath("ssh-copy-id"); err != nil {
		r.warnf("ssh-copy-id does not exist (required to deploy public keys)")
	} else {
		r.ok("ssh-copy-id")
	}

	r.head("File permissions")
	if st, err := os.Stat(p.SSHDir); err != nil {
		r.warnf("%s does not exist", p.SSHDir)
	} else if m := st.Mode().Perm(); m&0o077 != 0 {
		r.bad("%s izin %04o (should be 0700)", p.SSHDir, m)
	} else {
		r.ok("%s izin %04o", p.SSHDir, m)
	}
	if st, err := os.Stat(p.Config); err == nil {
		if m := st.Mode().Perm(); m&0o022 != 0 {
			r.bad("%s is writable by group/others (izin %04o)", p.Config, m)
		} else {
			r.ok("%s izin %04o", p.Config, m)
		}
	}

	r.head("Config")
	hosts, err := sshconfig.ParseConfig(p.Config)
	switch {
	case err != nil:
		r.bad("failed to read %s: %v", p.Config, err)
	default:
		r.ok("%d host di %s", len(hosts), p.Config)
		for _, h := range hosts {
			for _, k := range h.IdentityFiles {
				if _, err := os.Stat(k); err != nil {
					r.warnf("host %s: IdentityFile %s does not exist", h.Alias, k)
				}
			}
		}
	}

	r.head("Agent")
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		r.warnf("SSH_AUTH_SOCK is not set in this shell (sshm will use a fallback agent)")
	}
	if a := agent.Read(); a.Running {
		r.ok("agent running, %d keys loaded", a.Count)
	} else {
		r.warnf("agent is unreachable (sshm will start one when connecting)")
	}

	r.head("Key")
	paths := keys.ListPaths(p.SSHDir)
	if len(paths) == 0 {
		r.warnf("no private keys in %s", p.SSHDir)
	}
	for _, k := range paths {
		name := filepath.Base(k)
		i := keys.Inspect(k)
		if pp := keys.PermProblem(k); pp != "" {
			r.bad("%s: %s", name, pp)
		}
		if w := keys.Weak(i); w != "" {
			r.warnf("%s: %s", name, w)
		}
		if !i.HasPub {
			r.warnf("%s: has no .pub file", name)
		}
		if keys.PermProblem(k) == "" && keys.Weak(i) == "" && i.HasPub {
			r.ok("%s (%s %d bit)", name, i.Type, i.Bits)
		}
	}

	r.head("known_hosts")
	if es, err := knownhosts.Parse(p.KnownHosts); err != nil {
		r.warnf("failed to read %s: %v", p.KnownHosts, err)
	} else {
		total, hashed, warned := knownhosts.Summary(es)
		r.ok("%d entries (%d hashed)", total, hashed)
		if warned > 0 {
			r.warnf("%d entries need attention (open the Known tab in sshm)", warned)
		}
	}

	fmt.Fprintf(w, "\n%d errors, %d warnings\n", r.fail, r.warn)
	if r.fail > 0 {
		return 1
	}
	return 0
}
