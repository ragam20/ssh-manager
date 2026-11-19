// Package keys inspects and creates SSH key files.
package keys

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// Info contains the result of inspecting a key.
type Info struct {
	Path    string
	FP      string // SHA256:...
	Type    string // ED25519, RSA, ...
	Bits    int
	Comment string
	HasPub  bool
}

// Inspect runs `ssh-keygen -lf`. Setsid prevents it from prompting for a
// passphrase in the terminal. FP is empty on failure (for example, an encrypted key without .pub).
func Inspect(path string) Info {
	info := Info{Path: path}
	cand := path
	if _, err := os.Stat(path + ".pub"); err == nil {
		info.HasPub = true
		cand = path + ".pub"
	}
	cmd := exec.Command("ssh-keygen", "-lf", cand)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.Output()
	if err != nil {
		return info
	}
	info.Bits, info.FP, info.Comment, info.Type = parseKeygenLine(string(out))
	return info
}

func parseKeygenLine(line string) (bits int, fp, comment, typ string) {
	f := strings.Fields(line)
	if len(f) < 3 || !strings.HasPrefix(f[1], "SHA256:") {
		return
	}
	bits, _ = strconv.Atoi(f[0])
	fp = f[1]
	typ = strings.Trim(f[len(f)-1], "()")
	comment = strings.Join(f[2:len(f)-1], " ")
	return
}

// Weak returns why a key is considered weak, or "".
func Weak(i Info) string {
	switch strings.ToUpper(i.Type) {
	case "DSA":
		return "DSA is deprecated"
	case "RSA":
		if i.Bits > 0 && i.Bits < 3072 {
			return fmt.Sprintf("RSA %d bit (disarankan ≥ 3072)", i.Bits)
		}
	}
	return ""
}

var skipNames = map[string]bool{
	"config": true, "known_hosts": true, "known_hosts.old": true, "authorized_keys": true,
	"environment": true, "rc": true,
}

// ListPaths returns private key file paths in dir (non-recursively).
func ListPaths(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || skipNames[n] || strings.HasSuffix(n, ".pub") || strings.HasPrefix(n, ".") || strings.HasSuffix(n, ".bak") {
			continue
		}
		if isPrivateKey(filepath.Join(dir, n)) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

func isPrivateKey(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	s := string(buf[:n])
	return strings.HasPrefix(s, "-----BEGIN") && strings.Contains(s, "PRIVATE KEY")
}

// PublicKey reads the contents of a .pub file (one line).
func PublicKey(path string) (string, error) {
	b, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", errors.New(".pub file does not exist")
	}
	return strings.TrimSpace(string(b)), nil
}

// PermProblem returns a private key file permission issue, or "".
func PermProblem(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if m := st.Mode().Perm(); m&0o077 != 0 {
		return fmt.Sprintf("permissions %04o are too open (should be 0600)", m)
	}
	return ""
}

// ValidName validates a new key filename (relative to ~/.ssh).
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("name is required")
	case strings.ContainsAny(name, "/\\ \t\n\r\x00"):
		return errors.New("name must not contain / or spaces")
	case strings.HasPrefix(name, "-") || strings.HasPrefix(name, "."):
		return errors.New("name must not start with - or .")
	case strings.HasSuffix(name, ".pub"):
		return errors.New("name must not end with .pub")
	}
	return nil
}

// GenerateCmd builds the ed25519 key creation command. When withPassphrase is true,
// ssh-keygen prompts for the passphrase in the terminal (it never passes through sshm).
func GenerateCmd(path, comment string, withPassphrase bool) *exec.Cmd {
	args := []string{"-t", "ed25519", "-f", path}
	if comment != "" {
		args = append(args, "-C", comment)
	}
	if !withPassphrase {
		args = append(args, "-N", "")
	}
	return exec.Command("ssh-keygen", args...)
}
