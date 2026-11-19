// Package knownhosts reads, audits, and deletes entries in ~/.ssh/known_hosts.
package knownhosts

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"sshm/internal/fsutil"
)

type Entry struct {
	Line   int      // 1-based
	Raw    string   // Original line, used to verify it before deletion.
	Marker string   // @revoked / @cert-authority / ""
	Hosts  []string // Empty when hashed.
	Hashed bool
	Type   string // ssh-ed25519, ssh-rsa, ...
	Bits   int
	FP     string // SHA256:...
}

// Label returns a short label for lists.
func (e Entry) Label() string {
	if e.Hashed {
		return "(hashed)"
	}
	return strings.Join(e.Hosts, ",")
}

// Parse reads a known_hosts file. A missing file produces an empty list.
func Parse(path string) ([]Entry, error) {
	lines, err := fsutil.ReadLines(path)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Fields(t)
		e := Entry{Line: i + 1, Raw: ln}
		if strings.HasPrefix(f[0], "@") {
			e.Marker, f = f[0], f[1:]
		}
		if len(f) < 3 {
			continue
		}
		if strings.HasPrefix(f[0], "|1|") {
			e.Hashed = true
		} else {
			e.Hosts = strings.Split(f[0], ",")
		}
		e.Type = f[1]
		if blob, err := base64.StdEncoding.DecodeString(f[2]); err == nil {
			sum := sha256.Sum256(blob)
			e.FP = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
			e.Bits = bits(e.Type, blob)
		}
		out = append(out, e)
	}
	return out, nil
}

func bits(typ string, blob []byte) int {
	switch typ {
	case "ssh-ed25519", "sk-ssh-ed25519@openssh.com":
		return 256
	case "ecdsa-sha2-nistp256":
		return 256
	case "ecdsa-sha2-nistp384":
		return 384
	case "ecdsa-sha2-nistp521":
		return 521
	case "ssh-dss":
		return 1024
	case "ssh-rsa":
		// blob: string "ssh-rsa", mpint e, mpint n
		rest := blob
		var parts [][]byte
		for i := 0; i < 3; i++ {
			if len(rest) < 4 {
				return 0
			}
			n := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
			if n < 0 || len(rest) < 4+n {
				return 0
			}
			parts = append(parts, rest[4:4+n])
			rest = rest[4+n:]
		}
		return new(big.Int).SetBytes(parts[2]).BitLen()
	}
	return 0
}

// Warnings returns audit warnings keyed by line number.
func Warnings(entries []Entry) map[int][]string {
	w := map[int][]string{}
	byHost := map[string]map[string]bool{} // host+tipe -> set fingerprint
	for _, e := range entries {
		switch {
		case e.Type == "ssh-dss":
			w[e.Line] = append(w[e.Line], "DSA key is deprecated")
		case e.Type == "ssh-rsa" && e.Bits > 0 && e.Bits < 2048:
			w[e.Line] = append(w[e.Line], fmt.Sprintf("RSA %d-bit key is too weak", e.Bits))
		}
		if e.Marker == "@revoked" {
			w[e.Line] = append(w[e.Line], "key is marked revoked")
		}
		for _, h := range e.Hosts {
			k := h + "|" + e.Type
			if byHost[k] == nil {
				byHost[k] = map[string]bool{}
			}
			byHost[k][e.FP] = true
		}
	}
	for _, e := range entries {
		for _, h := range e.Hosts {
			if len(byHost[h+"|"+e.Type]) > 1 {
				w[e.Line] = append(w[e.Line], "host has multiple different "+e.Type+" keys")
				break
			}
		}
	}
	return w
}

// Delete removes one line after verifying it matches raw, and creates a backup.
func Delete(path string, line int, raw, backupDir string) error {
	lines, err := fsutil.ReadLines(path)
	if err != nil {
		return err
	}
	if line < 1 || line > len(lines) || lines[line-1] != raw {
		return errors.New("known_hosts changed since it was read; reload it")
	}
	if backupDir != "" {
		if _, err := fsutil.Backup(path, backupDir, 20); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}
	lines = append(lines[:line-1], lines[line:]...)
	return fsutil.AtomicWrite(path, fsutil.JoinLines(lines), 0o600)
}

// Summary returns an audit summary for `sshm doctor`.
func Summary(entries []Entry) (total, hashed, warned int) {
	w := Warnings(entries)
	lines := make([]int, 0, len(w))
	for l := range w {
		lines = append(lines, l)
	}
	sort.Ints(lines)
	for _, e := range entries {
		total++
		if e.Hashed {
			hashed++
		}
	}
	return total, hashed, len(lines)
}
