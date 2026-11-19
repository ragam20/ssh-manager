// Package tunnel manages SSH port forwarding through ControlMaster sockets.
package tunnel

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"sshm/internal/sshcmd"
)

// Spec defines a saved tunnel.
type Spec struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Kind       string `json:"kind"`           // L (local), R (remote), D (dynamic/SOCKS)
	Bind       string `json:"bind,omitempty"` // alamat bind opsional
	ListenPort int    `json:"listen_port"`
	DestHost   string `json:"dest_host,omitempty"`
	DestPort   int    `json:"dest_port,omitempty"`
}

var (
	reBind = regexp.MustCompile(`^[A-Za-z0-9.:\[\]*_-]+$`)
	reDest = regexp.MustCompile(`^[A-Za-z0-9._:\[\]-]+$`)
	reID   = regexp.MustCompile(`^[A-Za-z0-9]{4,16}$`)
)

func okPort(p int) bool { return p >= 1 && p <= 65535 }

// Validate checks a Spec.
func (s Spec) Validate() error {
	if s.Host == "" || s.Host[0] == '-' {
		return errors.New("host is required and must not start with '-'")
	}
	if !reID.MatchString(s.ID) {
		return errors.New("invalid tunnel ID")
	}
	if !okPort(s.ListenPort) {
		return errors.New("port must be 1-65535")
	}
	if s.Bind != "" && (!reBind.MatchString(s.Bind) || s.Bind[0] == '-') {
		return errors.New("invalid bind address")
	}
	switch s.Kind {
	case "D":
		return nil
	case "L", "R":
		if !reDest.MatchString(s.DestHost) || s.DestHost[0] == '-' {
			return errors.New("invalid destination host")
		}
		if !okPort(s.DestPort) {
			return errors.New("destination port must be 1-65535")
		}
		return nil
	}
	return errors.New("type must be L, R, or D")
}

// Forward returns the flag and its value, e.g. ("-L", "8080:localhost:80").
func (s Spec) Forward() (flag, val string) {
	bind := ""
	if s.Bind != "" {
		bind = s.Bind + ":"
	}
	switch s.Kind {
	case "R":
		return "-R", fmt.Sprintf("%s%d:%s:%d", bind, s.ListenPort, s.DestHost, s.DestPort)
	case "D":
		return "-D", fmt.Sprintf("%s%d", bind, s.ListenPort)
	}
	return "-L", fmt.Sprintf("%s%d:%s:%d", bind, s.ListenPort, s.DestHost, s.DestPort)
}

// Describe adalah ringkasan satu baris.
func (s Spec) Describe() string {
	switch s.Kind {
	case "R":
		return fmt.Sprintf("remote :%d → %s:%d", s.ListenPort, s.DestHost, s.DestPort)
	case "D":
		return fmt.Sprintf("SOCKS :%d", s.ListenPort)
	}
	return fmt.Sprintf("local :%d → %s:%d", s.ListenPort, s.DestHost, s.DestPort)
}

// SocketPath returns the control socket path (kept short due to the ~104-character limit).
func SocketPath(id string) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("sshm-%d", os.Getuid()))
	}
	dir = filepath.Join(dir, "sshm")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, id+".sock")
}

// StartCmd builds the command that opens a tunnel in the background (ssh -f).
// It runs with a terminal attached so passphrase/password prompts remain visible.
func StartCmd(s Spec) *exec.Cmd {
	flag, val := s.Forward()
	args := append(sshcmd.ConfigArgs(), "-f", "-N", "-M", "-S", SocketPath(s.ID),
		"-o", "ExitOnForwardFailure=yes", flag, val, "--", s.Host)
	return exec.Command("ssh", args...)
}

func ctl(s Spec, op string) *exec.Cmd {
	args := append(sshcmd.ConfigArgs(), "-S", SocketPath(s.ID), "-O", op, "--", s.Host)
	return exec.Command("ssh", args...)
}

// Running checks whether the tunnel master is still active.
func Running(s Spec) bool { return ctl(s, "check").Run() == nil }

// Stop stops the tunnel.
func Stop(s Spec) error {
	if out, err := ctl(s, "exit").CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

// NewID creates a random 6-character hexadecimal ID.
func NewID() string {
	b := make([]byte, 3)
	f, err := os.Open("/dev/urandom")
	if err == nil {
		defer f.Close()
		_, _ = f.Read(b)
	}
	return fmt.Sprintf("%02x%02x%02x", b[0], b[1], b[2])
}

// ParsePort converts text to a valid port.
func ParsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || !okPort(n) {
		return 0, errors.New("port must be 1-65535")
	}
	return n, nil
}
