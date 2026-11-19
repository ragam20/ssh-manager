package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"sshm/internal/paths"
	"sshm/internal/sshconfig"
)

func TestInspectHostUsesOpenSSHConfig(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH is required")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "custom config")
	t.Setenv("SSHM_CONFIG", config)
	t.Setenv("SSHM_TEST_KEYS", dir)
	if err := os.WriteFile(filepath.Join(dir, "demo key"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	contents := `Host dem*
    User inherited
    Port 2222
Host demo
    HostName target.invalid
    ProxyJump jump.invalid
    IdentityFile "${SSHM_TEST_KEYS}/%n key"
    IdentityFile "${SSHM_TEST_KEYS}/missing key"
`
	if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	p := paths.Default()
	hosts, err := sshconfig.ParseConfig(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	m := New(p, hosts, nil)
	m.w, m.h = 72, 10
	inspect := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}
	opened, cmd := m.Update(inspect)
	loaded, _ := opened.Update(cmd())
	view := loaded.View()
	for _, want := range []string{"Inspect demo", "target.invalid", "inherited", "2222", "jump.invalid"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	scrolled, _ := loaded.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	view = scrolled.View()
	for _, want := range []string{"IdentityFile", "  ${SSHM_TEST_KEYS}/%n key", "(not found) ${SSHM_TEST_KEYS}/missing key"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "(not found) ${SSHM_TEST_KEYS}/%n key") || strings.Contains(view, "unable to check") {
		t.Errorf("incorrect file status:\n%s", view)
	}
	closed, _ := scrolled.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if closed.(Model).mode != mNormal {
		t.Fatal("Esc did not return to Hosts")
	}
	if err := os.WriteFile(config, []byte(contents+"    NotAnSSHOption yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, cmd = closed.Update(inspect)
	// Leave room for OpenSSH's config path before checking the full diagnostic.
	opened, _ = opened.Update(tea.WindowSizeMsg{Width: len(config) + 80, Height: 10})
	loaded, _ = opened.Update(cmd())
	if view := loaded.View(); !strings.Contains(view, "Bad configuration option") {
		t.Errorf("OpenSSH error missing from view:\n%s", view)
	}
}
