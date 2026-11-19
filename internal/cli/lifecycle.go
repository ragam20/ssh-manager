package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const latestRelease = "https://github.com/ragam20/ssh-manager/releases/latest/download/"

func lifecycle(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	action := args[0]
	yes := action == "uninstall" && len(args) == 2 && args[1] == "--yes"
	if len(args) != 1 && !yes {
		fmt.Fprintf(stderr, "usage: sshm %s", action)
		if action == "uninstall" {
			fmt.Fprint(stderr, " [--yes]")
		}
		fmt.Fprintln(stderr)
		return 2
	}
	path, err := os.Executable()
	if err != nil {
		return fail(stderr, err)
	}
	target, err := findInstallation(path)
	if err != nil {
		return fail(stderr, err)
	}
	if action == "uninstall" {
		return fail(stderr, target.uninstall(stdin, stdout, yes))
	}
	client := &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("unexpected release download redirect")
			}
			return nil
		},
	}
	asset := "sshm-" + runtime.GOOS + "-" + runtime.GOARCH
	if !((runtime.GOOS == "linux" || runtime.GOOS == "darwin") &&
		(runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")) {
		return fail(stderr, fmt.Errorf("unsupported update platform: %s/%s", runtime.GOOS, runtime.GOARCH))
	}
	if err := target.update(client, latestRelease, asset); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintln(stdout, "updated sshm at", target.path)
	return 0
}

type installation struct {
	path      string
	file, dir os.FileInfo
}

func findInstallation(path string) (installation, error) {
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return installation{}, err
	}
	dir := filepath.Dir(path)
	if filepath.Base(path) != "sshm" || filepath.Base(dir) != "bin" {
		return installation{}, fmt.Errorf("expected an installed executable at <prefix>/bin/sshm, found %s", path)
	}
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return installation{}, err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return installation{}, err
	}
	if !fileInfo.Mode().IsRegular() || !dirInfo.IsDir() ||
		int(fileInfo.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() ||
		int(dirInfo.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return installation{}, fmt.Errorf("use the original installation method for %s; the executable and its bin directory must be owned by the current user", path)
	}
	return installation{path: path, file: fileInfo, dir: dirInfo}, nil
}

func (i installation) checkIdentity() error {
	for _, entry := range []struct {
		path string
		info os.FileInfo
	}{{filepath.Dir(i.path), i.dir}, {i.path, i.file}} {
		current, err := os.Lstat(entry.path)
		if err != nil {
			return err
		}
		if !os.SameFile(entry.info, current) {
			return fmt.Errorf("%s changed during the operation", entry.path)
		}
	}
	return nil
}

func (i installation) uninstall(stdin io.Reader, stdout io.Writer, yes bool) error {
	if !yes {
		fmt.Fprintf(stdout, "Remove %s? [y/N] ", i.path)
		input, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		answer := strings.ToLower(strings.TrimSpace(input))
		if err == io.EOF || (answer != "y" && answer != "yes") {
			fmt.Fprintln(stdout, "uninstall cancelled")
			return nil
		}
	}
	if err := i.checkIdentity(); err != nil {
		return err
	}
	if err := syscall.Unlink(i.path); err != nil {
		return fmt.Errorf("remove %s: %w", i.path, err)
	}
	fmt.Fprintln(stdout, "uninstalled sshm from", i.path)
	return nil
}

func releaseAsset(client *http.Client, base, name string) (io.ReadCloser, error) {
	res, err := client.Get(base + name)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("download %s: %s", name, res.Status)
	}
	return res.Body, nil
}

func releaseChecksum(client *http.Client, base, asset string) (string, error) {
	body, err := releaseAsset(client, base, "SHA256SUMS")
	if err != nil {
		return "", err
	}
	defer body.Close()
	var expected string
	lines := bufio.NewScanner(body)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if expected != "" || err != nil || len(digest) != sha256.Size {
			return "", fmt.Errorf("invalid or duplicate checksum for %s", asset)
		}
		expected = strings.ToLower(fields[0])
	}
	if err := lines.Err(); err != nil {
		return "", fmt.Errorf("read SHA256SUMS: %w", err)
	}
	if expected == "" {
		return "", fmt.Errorf("checksum for %s is missing from the release", asset)
	}
	return expected, nil
}

func (i installation) update(client *http.Client, base, asset string) error {
	expected, err := releaseChecksum(client, base, asset)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(i.path), ".sshm-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	body, err := releaseAsset(client, base, asset)
	if err != nil {
		return err
	}
	defer body.Close()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(staged, hash), body); err != nil {
		return fmt.Errorf("stage %s: %w", asset, err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("checksum verification failed for %s", asset)
	}
	if err := staged.Chmod(0755); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := i.checkIdentity(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), i.path)
}
