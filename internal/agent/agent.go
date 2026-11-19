package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

type Info struct {
	Running bool
	Count   int
	FPs     map[string]bool // SHA256 fingerprints of loaded keys.
}

// Read runs `ssh-add -l`.
// exit 0 = keys are present, 1 = agent is running with no keys, otherwise the agent is unreachable.
func Read() Info {
	info := Info{FPs: map[string]bool{}}
	out, err := exec.Command("ssh-add", "-l").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			info.Running = true
		}
		return info
	}
	info.Running = true
	info.Count, info.FPs = parseAddList(string(out))
	return info
}

func parseAddList(out string) (int, map[string]bool) {
	fps := map[string]bool{}
	n := 0
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 && strings.HasPrefix(f[1], "SHA256:") {
			fps[f[1]] = true
			n++
		}
	}
	return n, fps
}

func Unload(path string) error {
	cmd := exec.Command("ssh-add", "-d", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	return nil
}

var (
	reSock = regexp.MustCompile(`SSH_AUTH_SOCK=([^;\s]+)`)
	rePID  = regexp.MustCompile(`SSH_AGENT_PID=([0-9]+)`)
)

// ParseAgentOutput parses the output of `ssh-agent -s`.
func ParseAgentOutput(s string) (sock, pid string) {
	if m := reSock.FindStringSubmatch(s); m != nil {
		sock = m[1]
	}
	if m := rePID.FindStringSubmatch(s); m != nil {
		pid = m[1]
	}
	return
}

func cacheFile() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		h, _ := os.UserHomeDir()
		dir = filepath.Join(h, ".cache")
	}
	return filepath.Join(dir, "sshm", "agent.env")
}

// Ensure makes sure an agent is available and sets
// SSH_AUTH_SOCK in this process (inherited by ssh-add and ssh).
// Order: agent from environment -> cached agent -> new ssh-agent.
func Ensure() error {
	if Read().Running {
		return nil
	}
	if b, err := os.ReadFile(cacheFile()); err == nil {
		sock, pid := ParseAgentOutput(string(b))
		if sock != "" {
			os.Setenv("SSH_AUTH_SOCK", sock)
			if pid != "" {
				os.Setenv("SSH_AGENT_PID", pid)
			}
			if Read().Running {
				return nil
			}
		}
	}
	out, err := exec.Command("ssh-agent", "-s").Output()
	if err != nil {
		return fmt.Errorf("failed to run ssh-agent: %w", err)
	}
	sock, pid := ParseAgentOutput(string(out))
	if sock == "" {
		return errors.New("unrecognized ssh-agent output")
	}
	os.Setenv("SSH_AUTH_SOCK", sock)
	if pid != "" {
		os.Setenv("SSH_AGENT_PID", pid)
	}
	_ = os.MkdirAll(filepath.Dir(cacheFile()), 0o700)
	_ = os.WriteFile(cacheFile(), []byte(fmt.Sprintf("SSH_AUTH_SOCK=%s\nSSH_AGENT_PID=%s\n", sock, pid)), 0o600)
	return nil
}

// LoadCmd builds `ssh-add [-t seconds] key...`. A lifetime of 0 means no timeout.
func LoadCmd(lifetime int, keys []string) *exec.Cmd {
	var args []string
	if lifetime > 0 {
		args = append(args, "-t", strconv.Itoa(lifetime))
	}
	return exec.Command("ssh-add", append(args, keys...)...)
}
