// Package fsutil provides safe (atomic) file writes and backups.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AtomicWrite writes data to path through a temporary file and then renames it.
// Symlinks are followed (so dotfile managers keep working) and the original file mode is preserved.
func AtomicWrite(path string, data []byte, defaultMode os.FileMode) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := defaultMode
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sshm-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Backup copies path to dir/<name>-<time>.bak and keeps only
// the `keep` most recent backups for the same file. Missing files are skipped.
func Backup(path, dir string, keep int) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := filepath.Base(path)
	stamp := time.Now().Format("20060102-150405.000000000")
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s.bak", base, stamp))
	// O_EXCL: never overwrite an existing backup.
	for n := 1; ; n++ {
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.Write(b)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return "", werr
			}
			break
		}
		if !os.IsExist(err) {
			return "", err
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.bak", base, stamp, n))
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, base+"-*.bak")); len(matches) > keep {
		sort.Strings(matches)
		for _, m := range matches[:len(matches)-keep] {
			os.Remove(m)
		}
	}
	return dst, nil
}

// ReadLines reads a file into lines (without newlines). A missing file returns an empty list.
func ReadLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// JoinLines kebalikan ReadLines; selalu diakhiri newline.
func JoinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}
