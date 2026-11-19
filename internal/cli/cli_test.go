package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSHM_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host a\n  HostName a.example.com\n  User u\nHost b\n"), 0o600)
}

func run(args ...string) (int, string, string) {
	var o, e bytes.Buffer
	c := Run(args, &o, &e)
	return c, o.String(), e.String()
}

func TestListFormats(t *testing.T) {
	setup(t)
	if c, out, _ := run("list", "-n"); c != 0 || out != "a\nb\n" {
		t.Errorf("names: %d %q", c, out)
	}
	if c, out, _ := run("list"); c != 0 || !strings.Contains(out, "a.example.com") || !strings.HasPrefix(out, "ALIAS") {
		t.Errorf("tabel: %d %q", c, out)
	}
	c, out, _ := run("list", "--json")
	var items []map[string]any
	if c != 0 || json.Unmarshal([]byte(out), &items) != nil || len(items) != 2 || items[0]["alias"] != "a" {
		t.Errorf("json: %d %q", c, out)
	}
	if _, has := items[0]["last_used"]; has {
		t.Error("empty last_used should be omitted")
	}
}

func TestErrorsAndHelp(t *testing.T) {
	setup(t)
	if c, _, e := run("connect", "zzz"); c != 1 || !strings.Contains(e, "not found") {
		t.Errorf("connect: %d %q", c, e)
	}
	if c, _, _ := run("connect"); c != 2 {
		t.Errorf("connect without arguments: %d", c)
	}
	if c, _, _ := run("bogus"); c != 2 {
		t.Errorf("perintah asing: %d", c)
	}
	for _, arg := range []string{"version", "--version", "-v"} {
		if c, out, _ := run(arg); c != 0 || out != "sshm "+Version+"\n" {
			t.Errorf("%s: %d %q", arg, c, out)
		}
	}
	for _, sh := range []string{"bash", "zsh"} {
		if c, out, _ := run("completion", sh); c != 0 || !strings.Contains(out, "sshm list --names") {
			t.Errorf("completion %s: %d", sh, c)
		}
	}
	if c, _, _ := run("completion", "fish"); c != 2 {
		t.Errorf("completion fish: %d", c)
	}
}

func TestListWithMissingConfig(t *testing.T) {
	setup(t)
	os.Remove(filepath.Join(os.Getenv("HOME"), ".ssh", "config"))
	if c, out, _ := run("list", "-n"); c != 0 || out != "" {
		t.Errorf("%d %q", c, out)
	}
}
