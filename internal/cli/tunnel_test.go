package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sshm/internal/store"
	"sshm/internal/tunnel"
)

func TestTunnelCLIUsesSharedStateAndSocket(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("SSHM_CONFIG", filepath.Join(dir, "custom config"))
	scripts := map[string]string{
		"ssh-add": "#!/bin/sh\nexit 1\n", // agent reachable with no keys loaded
		"ssh": `#!/bin/sh
[ "$1" = -F ] && [ "$2" = "$SSHM_CONFIG" ] || exit 2
shift 2
case "$1" in
  -f)
    [ "$2" = -N ] && [ "$3" = -M ] && [ "$4" = -S ] || exit 2
    (set -C; : > "$5") ;;
  -S)
    [ "$3" = -O ] || exit 2
    case "$4" in
      check) test -f "$2" ;;
      exit) rm "$2" ;;
      *) exit 2 ;;
    esac ;;
  *) exit 2 ;;
esac
`,
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	st := store.State{Tunnels: []tunnel.Spec{
		{ID: "abcd01", Name: "work proxy", Host: "a", Kind: "D", ListenPort: 8080},
		{ID: "abcd02", Name: "shared", Host: "a", Kind: "D", ListenPort: 8081},
		{ID: "abcd03", Name: "shared", Host: "b", Kind: "D", ListenPort: 8082},
	}}
	if err := st.Save(store.DefaultPath()); err != nil {
		t.Fatal(err)
	}

	first, second, third := st.Tunnels[0], st.Tunnels[1], st.Tunnels[2]
	if code, _, err := run("tunnel", "start", first.Name); code != 0 || !tunnel.Running(first) {
		t.Fatalf("start by name did not activate the shared socket: %d %s", code, err)
	}
	if code, out, err := run("tunnel", "start", first.ID); code != 0 || !strings.Contains(out, "already running") {
		t.Fatalf("repeat start: %d %s %s", code, out, err)
	}
	if err := tunnel.Stop(first); err != nil {
		t.Fatalf("shared stop could not stop CLI tunnel: %v", err)
	}
	for _, action := range []string{"start", "stop"} {
		if code, _, err := run("tunnel", action, second.Name); code != 1 || !strings.Contains(err, "Use an ID") {
			t.Fatalf("%s with duplicate name: %d %s", action, code, err)
		}
	}
	if code, _, err := run("tunnel", "start", third.ID); code != 0 || !tunnel.Running(third) || tunnel.Running(second) {
		t.Fatalf("start did not select the requested ID: %d %s", code, err)
	}
	if err := tunnel.StartCmd(second).Run(); err != nil {
		t.Fatalf("shared start: %v", err)
	}
	code, out, stderr := run("tunnel", "list")
	rows := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(rows) != 4 || strings.Join(strings.Fields(rows[0]), " ") != "NAME ID STATUS" {
		t.Fatalf("list: %d %q %s", code, out, stderr)
	}
	for i, want := range []string{"work proxy abcd01 stopped", "shared abcd02 running", "shared abcd03 running"} {
		if got := strings.Join(strings.Fields(rows[i+1]), " "); got != want {
			t.Errorf("list row = %q, want %q", got, want)
		}
	}
	if code, _, err := run("tunnel", "stop", second.ID); code != 0 || tunnel.Running(second) || !tunnel.Running(third) {
		t.Fatalf("CLI stop did not stop the requested shared socket: %d %s", code, err)
	}
	if code, _, err := run("tunnel", "start", "unknown"); code != 1 || !strings.Contains(err, "not found") {
		t.Fatalf("unknown tunnel: %d %s", code, err)
	}
	if code, _, err := run("tunnel", "stop"); code != 2 || !strings.Contains(err, "usage:") {
		t.Fatalf("missing selector: %d %s", code, err)
	}
}
