package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"sshm/internal/agent"
	"sshm/internal/clipboard"
	"sshm/internal/keys"
	"sshm/internal/knownhosts"
	"sshm/internal/sshcmd"
	"sshm/internal/sshconfig"
	"sshm/internal/tunnel"
)

func (m Model) ask(text string, yes func(m Model) (Model, tea.Cmd)) Model {
	m.mode, m.confirm = mConfirm, &confirmState{text, yes}
	return m
}

func (m Model) loadCmd(paths []string, kind, extra string) tea.Cmd {
	return tea.ExecProcess(agent.LoadCmd(m.st.KeyLifetime, paths), func(err error) tea.Msg {
		return execDoneMsg{kind: kind, err: err, extra: extra}
	})
}

func unloadCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		for _, p := range paths {
			if err := agent.Unload(p); err != nil {
				return opMsg{"unload failed: " + err.Error()}
			}
		}
		return opMsg{"key unloaded"}
	}
}

func copyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		how, err := clipboard.Copy(text)
		if err != nil {
			return opMsg{"copy failed: " + err.Error()}
		}
		return opMsg{"public key disalin (" + how + ")"}
	}
}

// hostKey selects the host primary key: the first with a .pub file, or the first key.
func (m Model) hostKey(h sshconfig.Host) string {
	for _, k := range h.IdentityFiles {
		if e, ok := m.infos[k]; ok && e.info.HasPub {
			return k
		}
	}
	if len(h.IdentityFiles) > 0 {
		return h.IdentityFiles[0]
	}
	return ""
}

// ---------- Hosts ----------

func (m Model) hostsKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "a":
		return m.openForm(m.hostForm(nil)), nil
	case "s":
		m.sortAlpha = !m.sortAlpha
		m.rebuild()
		m.notice = map[bool]string{true: "sort: name", false: "sort: last used"}[m.sortAlpha]
		return m, nil
	}
	h, ok := m.selHost()
	if !ok {
		return m, nil
	}
	switch k {
	case "enter":
		return m.connect(h)
	case "i":
		return m.inspectHost(h.Alias)
	case " ":
		return m.toggleHost(h)
	case "e":
		return m.openForm(m.hostForm(&h)), nil
	case "D":
		return m.ask(fmt.Sprintf("Hapus host %q dari %s?\n(backup otomatis dibuat)", h.Alias, tilde(h.Source)),
			func(m Model) (Model, tea.Cmd) {
				if err := sshconfig.DeleteHost(h.Source, h.Line, h.Alias, m.p.BackupDir); err != nil {
					m.notice = err.Error()
					return m, nil
				}
				m.st.Forget(h.Alias)
				m.save()
				m.reloadHosts()
				if m.notice == "" {
					m.notice = "host deleted: " + h.Alias
				}
				return m, m.refresh()
			}), nil
	case "f":
		on := m.st.ToggleFavorite(h.Alias)
		m.save()
		m.rebuild()
		m.focusHost(h.Alias)
		m.notice = map[bool]string{true: "★ added to favorites", false: "removed from favorites"}[on]
	case "t":
		return m.openForm(m.tagForm(h)), nil
	case "p":
		return m.deploy(h)
	case "c":
		key := m.hostKey(h)
		pk, err := keys.PublicKey(key)
		if key == "" || err != nil {
			m.notice = "this host has no public key (.pub)"
			return m, nil
		}
		return m, copyCmd(pk)
	}
	return m, nil
}

func (m Model) connect(h sshconfig.Host) (tea.Model, tea.Cmd) {
	if err := agent.Ensure(); err != nil {
		m.notice = err.Error()
		return m, nil
	}
	if _, missing := m.keysByState(h.IdentityFiles); len(missing) > 0 {
		return m, m.loadCmd(missing, "connect", h.Alias)
	}
	m.final = sshcmd.Args(h.Alias)
	m.st.Touch(h.Alias)
	m.save()
	return m, tea.Quit
}

func (m Model) toggleHost(h sshconfig.Host) (tea.Model, tea.Cmd) {
	if len(h.IdentityFiles) == 0 {
		m.notice = "this host has no IdentityFile"
		return m, nil
	}
	return m.toggleKeys(h.IdentityFiles)
}

func (m Model) toggleKeys(paths []string) (tea.Model, tea.Cmd) {
	if err := agent.Ensure(); err != nil {
		m.notice = err.Error()
		return m, nil
	}
	loaded, missing := m.keysByState(paths)
	switch {
	case len(missing) == 0 && len(loaded) > 0:
		return m, unloadCmd(loaded)
	case len(missing) == 0:
		m.notice = "no keys available to load"
		return m, nil
	}
	return m, m.loadCmd(missing, "load", "")
}

func (m Model) deploy(h sshconfig.Host) (tea.Model, tea.Cmd) {
	key := m.hostKey(h)
	switch {
	case key == "":
		m.notice = "this host has no IdentityFile"
	case sshcmd.ConfigArgs() != nil:
		m.notice = "deployment does not support SSHM_CONFIG (ssh-copy-id uses the default config)"
	default:
		if _, err := os.Stat(key + ".pub"); err != nil {
			m.notice = "file " + tilde(key) + ".pub does not exist"
			return m, nil
		}
		if _, err := exec.LookPath("ssh-copy-id"); err != nil {
			m.notice = "ssh-copy-id not found"
			return m, nil
		}
		cmd := exec.Command("ssh-copy-id", "-i", key+".pub", h.Alias)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return execDoneMsg{kind: "deploy", err: err, extra: h.Alias}
		})
	}
	return m, nil
}

// ---------- Keys ----------

func (m Model) keysKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "n":
		return m.openForm(m.keygenForm()), nil
	case "t":
		v := m.st.NextLifetime()
		m.save()
		m.notice = "batas waktu key di agent: " + lifetimeLabel(v)
		return m, nil
	}
	i, ok := m.selIndex(scKeys)
	if !ok {
		return m, nil
	}
	path := m.keyPaths[i]
	switch k {
	case " ":
		return m.toggleKeys([]string{path})
	case "c":
		pk, err := keys.PublicKey(path)
		if err != nil {
			m.notice = err.Error()
			return m, nil
		}
		return m, copyCmd(pk)
	}
	return m, nil
}

func lifetimeLabel(sec int) string {
	if sec <= 0 {
		return "tanpa batas"
	}
	return fmt.Sprintf("%d jam", sec/3600)
}

// ---------- Tunnels ----------

func (m Model) tunnelsKey(k string) (tea.Model, tea.Cmd) {
	if k == "a" {
		return m.openForm(m.tunnelForm(nil)), nil
	}
	i, ok := m.selIndex(scTunnels)
	if !ok {
		return m, nil
	}
	t := m.st.Tunnels[i]
	switch k {
	case "e":
		return m.openForm(m.tunnelForm(&t)), nil
	case "D":
		return m.ask(fmt.Sprintf("Hapus tunnel %q (%s)?", t.Name, t.Describe()), func(m Model) (Model, tea.Cmd) {
			if tunnel.Running(t) {
				if err := tunnel.Stop(t); err != nil {
					m.notice = "stop failed: " + err.Error()
					return m, m.refresh()
				}
			}
			m.st.DeleteTunnel(t.ID)
			m.save()
			m.rebuild()
			m.notice = "tunnel deleted"
			return m, m.refresh()
		}), nil
	case " ":
		if err := t.Validate(); err != nil {
			m.notice = err.Error()
			return m, nil
		}
		if tunnel.Running(t) {
			return m, func() tea.Msg {
				if err := tunnel.Stop(t); err != nil {
					return opMsg{"stop failed: " + err.Error()}
				}
				return opMsg{"tunnel dihentikan"}
			}
		}
		if err := agent.Ensure(); err != nil {
			m.notice = err.Error()
			return m, nil
		}
		return m, tea.ExecProcess(tunnel.StartCmd(t), func(err error) tea.Msg {
			return execDoneMsg{kind: "tunnel", err: err}
		})
	}
	return m, nil
}

// ---------- Known hosts ----------

func (m Model) knownKey(k string) (tea.Model, tea.Cmd) {
	i, ok := m.selIndex(scKnown)
	if !ok || k != "D" {
		return m, nil
	}
	e := m.known[i]
	return m.ask(fmt.Sprintf("Hapus entri known_hosts baris %d?\n%s %s", e.Line, e.Label(), e.Type), func(m Model) (Model, tea.Cmd) {
		if err := knownhosts.Delete(m.p.KnownHosts, e.Line, e.Raw, m.p.BackupDir); err != nil {
			m.notice = err.Error()
			return m, m.loadKnown()
		}
		m.notice = "entry deleted"
		return m, m.loadKnown()
	}), nil
}
