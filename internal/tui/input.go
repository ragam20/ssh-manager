package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	m.notice = ""
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case mHelp:
		if k == "esc" || k == "?" || k == "q" {
			m.mode = mNormal
		}
		return m, nil
	case mForm:
		return m.formKey(msg)
	case mConfirm:
		c := m.confirm
		m.mode, m.confirm = mNormal, nil
		if k == "y" || k == "Y" {
			return c.yes(m)
		}
		return m, nil
	case mSearch:
		switch k {
		case "enter":
			m.mode = mNormal
		case "esc":
			m.mode, m.query = mNormal, ""
		case "backspace":
			if r := []rune(m.query); len(r) > 0 {
				m.query = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.query += string(msg.Runes)
			}
		}
		m.lists[m.sc].cursor = 0
		m.rebuild()
		return m, nil
	}
	return m.normalKey(k)
}

func (m Model) move(delta int) Model {
	m.lists[m.sc].cursor += delta
	m.clampList(m.sc)
	return m
}

func (m Model) switchScreen(s screen) (Model, tea.Cmd) {
	m.sc, m.query = s, ""
	m.rebuild()
	switch s {
	case scTunnels:
		return m, m.refresh()
	case scKnown:
		return m, m.loadKnown()
	}
	return m, nil
}

func (m Model) normalKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = mHelp
	case "tab":
		return m.switchScreen((m.sc + 1) % nScreens)
	case "shift+tab":
		return m.switchScreen((m.sc + nScreens - 1) % nScreens)
	case "1", "2", "3", "4":
		return m.switchScreen(screen(k[0] - '1'))
	case "up", "k":
		return m.move(-1), nil
	case "down", "j":
		return m.move(1), nil
	case "pgup":
		return m.move(-m.listRows()), nil
	case "pgdown":
		return m.move(m.listRows()), nil
	case "g", "home":
		return m.move(-1 << 20), nil
	case "G", "end":
		return m.move(1 << 20), nil
	case "/":
		m.mode = mSearch
	case "esc":
		m.query = ""
		m.rebuild()
	case "r":
		m.notice = "disegarkan"
		return m, tea.Batch(m.refresh(), m.loadKnown())
	default:
		switch m.sc {
		case scHosts:
			return m.hostsKey(k)
		case scKeys:
			return m.keysKey(k)
		case scTunnels:
			return m.tunnelsKey(k)
		case scKnown:
			return m.knownKey(k)
		}
	}
	return m, nil
}

func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != mNormal {
		return m, nil
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		return m.move(-3), nil
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		return m.move(3), nil
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		if msg.Y == 0 { // baris tab
			if s, ok := m.tabAt(msg.X); ok {
				return m.switchScreen(s)
			}
			return m, nil
		}
		lw, _ := m.paneWidths()
		row := msg.Y - 2 // header + garis atas kotak
		if msg.X < lw && row >= 0 && row < m.listRows() {
			pos := m.lists[m.sc].offset + row
			if pos < len(m.vis[m.sc]) {
				m.lists[m.sc].cursor = pos
				m.clampList(m.sc)
			}
		}
	}
	return m, nil
}
