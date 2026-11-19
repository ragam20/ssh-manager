package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"sshm/internal/fsutil"
)

// Entry is the content of one Host block that can be edited in the app.
// IdentityFiles are written as-is (may use ~).
type Entry struct {
	Alias         string
	HostName      string
	User          string
	Port          string
	ProxyJump     string
	IdentityFiles []string
}

const backupsKept = 20

func noCtl(name, v string) error {
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains control characters", name)
		}
	}
	return nil
}

func token(name, v string, allowEmpty bool) error {
	if v == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%s wajib diisi", name)
	}
	if err := noCtl(name, v); err != nil {
		return err
	}
	if strings.ContainsAny(v, " \t\"#") {
		return fmt.Errorf("%s must not contain spaces, \" or #", name)
	}
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("%s must not start with '-'", name)
	}
	return nil
}

// ValidateEntry checks the fields before writing them to the config.
func ValidateEntry(e Entry) error {
	if err := token("alias", e.Alias, false); err != nil {
		return err
	}
	if strings.ContainsAny(e.Alias, "*?!,=") {
		return errors.New("alias must not contain * ? ! , =")
	}
	for name, v := range map[string]string{"HostName": e.HostName, "User": e.User, "ProxyJump": e.ProxyJump} {
		if err := token(name, v, true); err != nil {
			return err
		}
	}
	if e.Port != "" {
		n, err := strconv.Atoi(e.Port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("Port must 1-65535")
		}
	}
	for _, f := range e.IdentityFiles {
		if strings.TrimSpace(f) == "" {
			return errors.New("IdentityFile is empty")
		}
		if err := noCtl("IdentityFile", f); err != nil {
			return err
		}
		if strings.Contains(f, `"`) {
			return errors.New(`IdentityFile must not contain "`)
		}
	}
	return nil
}

func quote(v string) string {
	if strings.ContainsAny(v, " \t#") {
		return `"` + v + `"`
	}
	return v
}

func leadingWS(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

func firstKey(line string) string {
	f := splitFields(line)
	if len(f) == 0 {
		return ""
	}
	return strings.ToLower(f[0])
}

// blockRange finds the block range beginning at `line` (1-based).
func blockRange(lines []string, line int, alias string) (start, end int, err error) {
	start = line - 1
	if start < 0 || start >= len(lines) || firstKey(lines[start]) != "host" {
		return 0, 0, errors.New("config changed since it was read; reload it")
	}
	if f := splitFields(lines[start]); !contains(f[1:], alias) {
		return 0, 0, errors.New("config changed since it was read; reload it")
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if k := firstKey(lines[i]); k == "host" || k == "match" {
			end = i
			break
		}
	}
	return start, end, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func commit(path, backupDir string, lines []string) error {
	if backupDir != "" {
		if _, err := fsutil.Backup(path, backupDir, backupsKept); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}
	return fsutil.AtomicWrite(path, fsutil.JoinLines(lines), 0o600)
}

// AddHost appends a new Host block to the file.
func AddHost(path, backupDir string, e Entry) error {
	if err := ValidateEntry(e); err != nil {
		return err
	}
	lines, err := fsutil.ReadLines(path)
	if err != nil {
		return err
	}
	if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, "Host "+e.Alias)
	add := func(k, v string) {
		if v != "" {
			lines = append(lines, "    "+k+" "+quote(v))
		}
	}
	add("HostName", e.HostName)
	add("User", e.User)
	add("Port", e.Port)
	add("ProxyJump", e.ProxyJump)
	for _, f := range e.IdentityFiles {
		add("IdentityFile", f)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return commit(path, backupDir, lines)
}

// UpdateHost modifies the Host block beginning at `line` in src.
// Only managed options (HostName, User, Port, ProxyJump, IdentityFile) and
// the `oldAlias` alias are changed; comments and other options are left untouched.
func UpdateHost(src string, line int, oldAlias string, e Entry, backupDir string) error {
	if err := ValidateEntry(e); err != nil {
		return err
	}
	lines, err := fsutil.ReadLines(src)
	if err != nil {
		return err
	}
	start, end, err := blockRange(lines, line, oldAlias)
	if err != nil {
		return err
	}

	// alias
	if e.Alias != oldAlias {
		re := regexp.MustCompile(`(^|[ \t=])` + regexp.QuoteMeta(oldAlias) + `([ \t]|$)`)
		lines[start] = re.ReplaceAllString(lines[start], "${1}"+e.Alias+"${2}")
	}

	body := append([]string{}, lines[start+1:end]...)
	indent := "    "
	for _, l := range body {
		if firstKey(l) != "" {
			indent = leadingWS(l)
			break
		}
	}
	insertAt := func() int { // after the last option line
		at := 0
		for i, l := range body {
			if firstKey(l) != "" {
				at = i + 1
			}
		}
		return at
	}
	sets := []struct {
		key  string
		vals []string
	}{
		{"hostname", one(e.HostName)}, {"user", one(e.User)}, {"port", one(e.Port)},
		{"proxyjump", one(e.ProxyJump)}, {"identityfile", e.IdentityFiles},
	}
	names := map[string]string{"hostname": "HostName", "user": "User", "port": "Port", "proxyjump": "ProxyJump", "identityfile": "IdentityFile"}
	for _, s := range sets {
		var idx []int
		var cur []string
		for i, l := range body {
			if firstKey(l) == s.key {
				idx = append(idx, i)
				if f := splitFields(l); len(f) > 1 {
					cur = append(cur, f[1])
				}
			}
		}
		if s.key != "identityfile" {
			cur = firstN(cur, 1)
			if len(s.vals) > 0 { // only the first line applies in ssh
				idx = firstN(idx, 1)
			}
		}
		if equal(cur, s.vals) {
			continue
		}
		mk := func(v string) string { return indent + names[s.key] + " " + quote(v) }
		pos := insertAt()
		if len(idx) > 0 {
			pos = idx[0]
		}
		body = removeAt(body, idx)
		var add []string
		for _, v := range s.vals {
			add = append(add, mk(v))
		}
		body = append(body[:pos], append(add, body[pos:]...)...)
	}

	out := append(append(append([]string{}, lines[:start+1]...), body...), lines[end:]...)
	return commit(src, backupDir, out)
}

func one(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}

func firstN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func removeAt(s []string, idx []int) []string {
	drop := map[int]bool{}
	for _, i := range idx {
		drop[i] = true
	}
	var out []string
	for i, l := range s {
		if !drop[i] {
			out = append(out, l)
		}
	}
	return out
}

// DeleteHost removes an alias from a Host block. If the block has only one alias,
// the entire block (including the comment immediately above it) is removed.
func DeleteHost(src string, line int, alias, backupDir string) error {
	lines, err := fsutil.ReadLines(src)
	if err != nil {
		return err
	}
	start, end, err := blockRange(lines, line, alias)
	if err != nil {
		return err
	}
	if f := splitFields(lines[start]); len(f) > 2 { // multiple aliases: remove only the token
		re := regexp.MustCompile(`(^|[ \t=])` + regexp.QuoteMeta(alias) + `([ \t]|$)`)
		lines[start] = strings.TrimRight(re.ReplaceAllString(lines[start], "${1}${2}"), " \t")
		lines[start] = regexp.MustCompile(`[ \t]{2,}`).ReplaceAllString(lines[start], " ")
		return commit(src, backupDir, lines)
	}
	from := start
	for from > 0 && strings.HasPrefix(strings.TrimSpace(lines[from-1]), "#") {
		from--
	}
	out := append(append([]string{}, lines[:from]...), lines[end:]...)
	// collapse repeated blank lines at the join point
	if from > 0 && from < len(out) && strings.TrimSpace(out[from-1]) == "" && strings.TrimSpace(out[from]) == "" {
		out = append(out[:from], out[from+1:]...)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return commit(src, backupDir, out)
}
