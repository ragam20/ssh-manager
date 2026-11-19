package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"sshm/internal/keys"
	"sshm/internal/sshconfig"
)

var (
	cGreen  = lipgloss.Color("42")
	cYellow = lipgloss.Color("214")
	cRed    = lipgloss.Color("203")
	cDim    = lipgloss.Color("244")
	cAccent = lipgloss.Color("75")

	sBold   = lipgloss.NewStyle().Bold(true)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sGreen  = lipgloss.NewStyle().Foreground(cGreen)
	sRed    = lipgloss.NewStyle().Foreground(cRed)
	sWarn   = lipgloss.NewStyle().Foreground(cYellow)
	sSel    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(cAccent)
	sBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cDim).Padding(0, 1)
	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sTabOn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(cAccent)
	sNotice = lipgloss.NewStyle().Foreground(cYellow)
)

const titleText = " sshm "

func tabLabel(s screen) string { return fmt.Sprintf(" %d %s ", int(s)+1, screenNames[s]) }

func (m Model) tabAt(x int) (screen, bool) {
	pos := lipgloss.Width(titleText)
	for s := screen(0); s < nScreens; s++ {
		w := lipgloss.Width(tabLabel(s))
		if x >= pos && x < pos+w {
			return s, true
		}
		pos += w
	}
	return 0, false
}

func (m Model) paneWidths() (lw, rw int) {
	if m.w < 80 {
		return m.w, 0
	}
	lw = m.w * 2 / 5
	if lw < 32 {
		lw = 32
	}
	return lw, m.w - lw
}

func (m Model) View() string {
	if m.w == 0 {
		return ""
	}
	switch m.mode {
	case mHelp:
		return m.helpView()
	case mForm:
		return m.formView()
	case mConfirm:
		return m.confirmView()
	}

	var tabs strings.Builder
	for s := screen(0); s < nScreens; s++ {
		if s == m.sc {
			tabs.WriteString(sTabOn.Render(tabLabel(s)))
		} else {
			tabs.WriteString(sDim.Render(tabLabel(s)))
		}
	}
	agentTxt := sRed.Render("○ stopped")
	if m.agent.Running {
		agentTxt = sGreen.Render("● running") + sDim.Render(fmt.Sprintf(" · %d key", m.agent.Count))
	}
	header := ansi.Truncate(sTitle.Render(titleText)+tabs.String()+"  "+sDim.Render("agent ")+agentTxt+
		sDim.Render(" · timeout "+lifetimeLabel(m.st.KeyLifetime)), m.w, "…")

	inner := m.h - 5
	if inner < 3 {
		inner = 3
	}
	lw, rw := m.paneWidths()
	body := sBox.Width(lw - 2).Height(inner).Render(m.listView(lw - 4))
	if rw > 0 {
		right := clipLines(m.detailView(rw-4), inner)
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, sBox.Width(rw-2).Height(inner).Render(right))
	}

	status := ""
	switch {
	case m.notice != "":
		status = sNotice.Render(" " + m.notice)
	case m.mode == mSearch:
		status = " " + sBold.Render("/") + m.query + "█"
	case m.query != "":
		status = sDim.Render(" filter: ") + m.query + sDim.Render("  (Esc clear)")
	}
	return strings.Join([]string{header, body,
		ansi.Truncate(status, m.w, "…"), ansi.Truncate(sDim.Render(m.helpBar()), m.w, "…")}, "\n")
}

func clipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func (m Model) helpBar() string {
	switch m.sc {
	case scKeys:
		return " Space load/unload · c copy pub · n new key · t timeout · / search · Tab screen · ? · q"
	case scTunnels:
		return " Space start/stop · a add · e edit · D delete · / search · Tab screen · ? · q"
	case scKnown:
		return " D delete entry · / search · r reload · Tab screen · ? · q"
	}
	return " Enter connect · Space load · a/e/D host · f ★ · t tag · p deploy · c copy · / search · Tab screen · ? · q"
}

// ---------- list ----------

func padTo(s string, w int) string {
	if n := ansi.StringWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func (m Model) rowText(i int) string {
	switch m.sc {
	case scKeys:
		return m.keyRow(m.keyPaths[i])
	case scTunnels:
		return m.tunnelRow(i)
	case scKnown:
		return m.knownRow(i)
	}
	return m.hostRow(m.hosts[i])
}

func (m Model) listView(w int) string {
	vis := m.vis[m.sc]
	if len(vis) == 0 {
		return sDim.Render(map[screen]string{
			scHosts:   "no hosts yet — press a to add one",
			scKeys:    "no keys in ~/.ssh — press n to create one",
			scTunnels: "no tunnels yet — press a to add one",
			scKnown:   "known_hosts is empty",
		}[m.sc])
	}
	l := m.lists[m.sc]
	end := l.offset + m.listRows()
	if end > len(vis) {
		end = len(vis)
	}
	var lines []string
	for pos := l.offset; pos < end; pos++ {
		line := m.rowText(vis[pos])
		if pos == l.cursor {
			line = sSel.Render(padTo(ansi.Truncate("▸ "+ansi.Strip(line), w, "…"), w))
		} else {
			line = ansi.Truncate("  "+line, w, "…")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) hostDot(h sshconfig.Host) string {
	if len(h.IdentityFiles) == 0 {
		return sDim.Render("·")
	}
	for _, k := range h.IdentityFiles {
		if m.keyState(k) != ksLoaded {
			return sDim.Render("○")
		}
	}
	return sGreen.Render("●")
}

func (m Model) hostRow(h sshconfig.Host) string {
	meta := m.st.Meta(h.Alias)
	fav := " "
	if meta.Favorite {
		fav = sWarn.Render("★")
	}
	line := m.hostDot(h) + " " + fav + " " + h.Alias
	if t := h.Target(); t != h.Alias {
		line += sDim.Render("  " + t)
	}
	if len(meta.Tags) > 0 {
		line += sDim.Render("  #" + strings.Join(meta.Tags, " #"))
	}
	return line
}

func (m Model) keyDot(p string) string {
	switch m.keyState(p) {
	case ksLoaded:
		return sGreen.Render("●")
	case ksNotLoaded:
		return sDim.Render("○")
	case ksNotFound:
		return sRed.Render("✗")
	}
	return sWarn.Render("?")
}

func (m Model) keyRow(p string) string {
	e := m.infos[p].info
	line := m.keyDot(p) + " " + baseName(p)
	if e.Type != "" {
		line += sDim.Render(fmt.Sprintf("  %s %d", e.Type, e.Bits))
	}
	if keys.Weak(e) != "" || keys.PermProblem(p) != "" {
		line += " " + sWarn.Render("⚠")
	}
	return line
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (m Model) tunnelRow(i int) string {
	t := m.st.Tunnels[i]
	dot := sDim.Render("○")
	if m.tunUp[t.ID] {
		dot = sGreen.Render("●")
	}
	return dot + " " + t.Name + sDim.Render("  "+t.Host)
}

func (m Model) knownRow(i int) string {
	e := m.known[i]
	line := e.Label() + sDim.Render("  "+e.Type)
	if len(m.knownW[e.Line]) > 0 {
		line += " " + sWarn.Render("⚠")
	}
	return line
}

// ---------- detail ----------

func kv(w int, k, v string) string {
	if v == "" {
		v = sDim.Render("-")
	}
	return ansi.Truncate(sDim.Render(fmt.Sprintf("%-12s", k))+v, w, "…")
}

func (m Model) statusText(s keyStatus) string {
	switch s {
	case ksLoaded:
		return sGreen.Render("● loaded")
	case ksNotLoaded:
		return sDim.Render("○ not loaded")
	case ksNotFound:
		return sRed.Render("✗ not found")
	}
	return sWarn.Render("? unknown")
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

func (m Model) detailView(w int) string {
	i, ok := m.selIndex(m.sc)
	if !ok {
		return ""
	}
	switch m.sc {
	case scKeys:
		return m.keyDetail(m.keyPaths[i], w)
	case scTunnels:
		return m.tunnelDetail(i, w)
	case scKnown:
		return m.knownDetail(i, w)
	}
	return m.hostDetail(m.hosts[i], w)
}

func (m Model) hostDetail(h sshconfig.Host, w int) string {
	meta := m.st.Meta(h.Alias)
	port := h.Port
	if port == "" {
		port = "22"
	}
	title := h.Alias
	if meta.Favorite {
		title += " ★"
	}
	src := ""
	if h.Source != m.p.Config {
		src = tilde(h.Source)
	}
	lines := []string{sTitle.Render(title), "",
		kv(w, "HostName", h.HostName), kv(w, "User", h.User), kv(w, "Port", port), kv(w, "ProxyJump", h.ProxyJump),
		kv(w, "Tags", strings.Join(meta.Tags, ", ")), kv(w, "Last used", relTime(meta.LastUsed)), kv(w, "Source", src),
		"", sDim.Render("Keys")}
	if len(h.IdentityFiles) == 0 {
		lines = append(lines, sDim.Render("  (no IdentityFile)"))
	}
	for _, k := range h.IdentityFiles {
		line := "  " + m.statusText(m.keyState(k))
		if why := keys.Weak(m.infos[k].info); why != "" {
			line += " " + sWarn.Render("⚠ "+why)
		}
		lines = append(lines, ansi.Truncate(line, w, "…"), ansi.Truncate(sDim.Render("    "+tilde(k)), w, "…"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) keyDetail(p string, w int) string {
	e := m.infos[p].info
	lines := []string{sTitle.Render(baseName(p)), "",
		kv(w, "Status", m.statusText(m.keyState(p))), kv(w, "Path", tilde(p)),
		kv(w, "Type", fmt.Sprintf("%s %d bit", e.Type, e.Bits)), kv(w, "Fingerprint", e.FP),
		kv(w, "Comment", e.Comment), kv(w, ".pub file", map[bool]string{true: "yes", false: "no"}[e.HasPub]),
		kv(w, "Used by", strings.Join(m.hostsUsing(p), ", "))}
	if why := keys.Weak(e); why != "" {
		lines = append(lines, "", sWarn.Render("⚠ "+why))
	}
	if why := keys.PermProblem(p); why != "" {
		lines = append(lines, "", sWarn.Render("⚠ "+why))
	}
	return strings.Join(lines, "\n")
}

func (m Model) tunnelDetail(i, w int) string {
	t := m.st.Tunnels[i]
	st := sDim.Render("○ stopped")
	if m.tunUp[t.ID] {
		st = sGreen.Render("● running")
	}
	return strings.Join([]string{sTitle.Render(t.Name), "",
		kv(w, "Status", st), kv(w, "Host", t.Host), kv(w, "Type", t.Describe()), kv(w, "Bind", t.Bind)}, "\n")
}

func (m Model) knownDetail(i, w int) string {
	e := m.known[i]
	lines := []string{sTitle.Render(e.Label()), "",
		kv(w, "Line", fmt.Sprint(e.Line)), kv(w, "Type", fmt.Sprintf("%s %d bit", e.Type, e.Bits)),
		kv(w, "Fingerprint", e.FP), kv(w, "Marker", e.Marker)}
	for _, wn := range m.knownW[e.Line] {
		lines = append(lines, "", sWarn.Render("⚠ "+wn))
	}
	return strings.Join(lines, "\n")
}

// ---------- overlay ----------

func (m Model) center(s string) string {
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, sBox.Render(s))
}

func (m Model) confirmView() string {
	return m.center(sTitle.Render("Konfirmasi") + "\n\n" + m.confirm.text + "\n\n" + sDim.Render("[y] ya   [n / Esc] batal"))
}

func (m Model) formView() string {
	f := m.form
	var b strings.Builder
	b.WriteString(sTitle.Render(f.title) + "\n\n")
	width := 44
	for i, fl := range f.fields {
		val := string(fl.val)
		if i == f.idx {
			before, after := string(fl.val[:fl.pos]), string(fl.val[fl.pos:])
			cur := " "
			if len(after) > 0 {
				r := []rune(after)
				cur, after = string(r[0]), string(r[1:])
			}
			val = before + sSel.Render(cur) + after
			b.WriteString(sBold.Render(fmt.Sprintf("%-18s", fl.label)) + ansi.Truncate(val, width, "…") + "\n")
			if fl.hint != "" {
				b.WriteString(strings.Repeat(" ", 18) + sDim.Render(ansi.Truncate(fl.hint, width, "…")) + "\n")
			}
			continue
		}
		b.WriteString(sDim.Render(fmt.Sprintf("%-18s", fl.label)) + ansi.Truncate(val, width, "…") + "\n")
	}
	if f.err != "" {
		b.WriteString("\n" + sRed.Render("✗ "+f.err) + "\n")
	}
	b.WriteString("\n" + sDim.Render("Tab/↑↓ pindah · Enter lanjut/simpan · Ctrl+S simpan · Esc batal"))
	return m.center(b.String())
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"Tab / 1-4", "ganti layar: Hosts, Keys, Tunnels, Known"},
		{"↑ ↓ j k", "move selection (mouse: click / scroll)"},
		{"g G PgUp PgDn", "awal / akhir / halaman"},
		{"/  Esc", "search (#tag for tags) / clear filter"},
		{"r", "reload"},
		{"", ""},
		{"Hosts", ""},
		{"Enter", "connect (auto-load key)"},
		{"Space", "load / unload key host"},
		{"a e D", "add / edit / delete host"},
		{"f  t  s", "favorite ★ / tag / sort"},
		{"p  c", "deploy public key / copy public key"},
		{"", ""},
		{"Keys", ""},
		{"Space  c", "load/unload / copy public key"},
		{"n  t", "buat key baru / batas waktu di agent"},
		{"", ""},
		{"Tunnels", ""},
		{"Space  a e D", "start/stop / add, edit, delete"},
		{"", ""},
		{"Known", ""},
		{"D", "delete known_hosts entry"},
		{"", ""},
		{"?  q  Ctrl-C", "help / quit"},
	}
	var b strings.Builder
	b.WriteString(sTitle.Render("sshm — help") + "\n\n")
	for _, r := range rows {
		switch {
		case r[0] != "" && r[1] == "":
			b.WriteString(sBold.Render(r[0]) + "\n")
		default:
			b.WriteString(fmt.Sprintf("%s%s\n", sBold.Render(fmt.Sprintf("%-16s", r[0])), r[1]))
		}
	}
	b.WriteString("\n" + sDim.Render("Press Esc to return"))
	return m.center(b.String())
}
