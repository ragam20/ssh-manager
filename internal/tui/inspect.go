package tui

import (
	"context"
	"errors"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"sshm/internal/sshcmd"
)

type inspectState struct {
	alias  string
	cancel context.CancelFunc
	result *inspectMsg
	offset int
}

type inspectMsg struct {
	request *inspectState
	config  sshcmd.EffectiveConfig
	err     error
}

func (m Model) inspectHost(alias string) (Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	request := &inspectState{alias: alias, cancel: cancel}
	m.mode, m.inspect = mInspect, request
	return m, func() tea.Msg {
		defer cancel()
		cfg, err := sshcmd.Inspect(ctx, alias)
		return inspectMsg{request, cfg, err}
	}
}

func (m Model) inspectKey(key string) Model {
	if key == "esc" || key == "q" {
		m.inspect.cancel()
		m.mode, m.inspect = mNormal, nil
		return m
	}
	page := max(1, m.h-5)
	last := max(0, len(m.inspectLines())-page)
	offset := min(m.inspect.offset, last)
	switch key {
	case "up", "k":
		offset--
	case "down", "j":
		offset++
	case "pgup":
		offset -= page
	case "pgdown":
		offset += page
	default:
		return m
	}
	m.inspect.offset = min(max(0, offset), last)
	return m
}

func (m Model) inspectLines() []string {
	result := m.inspect.result
	width := max(1, m.w-4)
	if result == nil {
		return []string{"Reading effective SSH configuration…"}
	}
	if result.err != nil {
		return strings.Split(ansi.Hardwrap(ansi.Strip(result.err.Error()), width, false), "\n")
	}
	cfg := result.config
	jump := cfg.ProxyJump
	if jump == "" {
		jump = "-"
	}
	lines := []string{"HostName    " + cfg.HostName, "User        " + cfg.User,
		"Port        " + cfg.Port, "ProxyJump   " + jump, "", "IdentityFile"}
	for _, file := range cfg.IdentityFiles {
		path := "  " + file.Path
		switch {
		case errors.Is(file.CheckError, os.ErrNotExist):
			path = "  (not found) " + file.Path
		case file.CheckError != nil:
			path = "  (unable to check) " + file.Path
		}
		lines = append(lines, path)
	}
	return strings.Split(ansi.Hardwrap(ansi.Strip(strings.Join(lines, "\n")), width, false), "\n")
}

func (m Model) inspectView() string {
	width := max(1, m.w-4)
	lines := m.inspectLines()
	page := max(1, m.h-5)
	offset := min(m.inspect.offset, max(0, len(lines)-page))
	title := sTitle.Render(ansi.Truncate("Inspect "+ansi.Strip(m.inspect.alias), width, "…"))
	foot := "Esc back"
	if len(lines) > page {
		foot = "Esc back · ↑↓ PgUp PgDn scroll"
	}
	return m.center(title + "\n" + strings.Join(lines[offset:min(offset+page, len(lines))], "\n") +
		"\n" + sDim.Render(ansi.Truncate(foot, width, "…")))
}
