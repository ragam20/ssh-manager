// Package tui provides the Bubble Tea interface for sshm.
package tui

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"sshm/internal/agent"
	"sshm/internal/keys"
	"sshm/internal/knownhosts"
	"sshm/internal/paths"
	"sshm/internal/sshcmd"
	"sshm/internal/sshconfig"
	"sshm/internal/store"
	"sshm/internal/tunnel"
)

type screen int

const (
	scHosts screen = iota
	scKeys
	scTunnels
	scKnown
	nScreens
)

var screenNames = [nScreens]string{"Hosts", "Keys", "Tunnels", "Known"}

type mode int

const (
	mNormal mode = iota
	mSearch
	mForm
	mConfirm
	mHelp
)

type keyStatus int

const (
	ksUnknown keyStatus = iota
	ksLoaded
	ksNotLoaded
	ksNotFound
)

type infoEntry struct {
	mtime time.Time
	info  keys.Info
}

type listState struct{ cursor, offset int }

type confirmState struct {
	text string
	yes  func(m Model) (Model, tea.Cmd)
}

// Model adalah state seluruh aplikasi.
type Model struct {
	p     paths.Paths
	st    *store.State
	hosts []sshconfig.Host

	sc    screen
	mode  mode
	w, h  int
	ready bool

	agent    agent.Info
	infos    map[string]infoEntry
	keyPaths []string
	tunUp    map[string]bool
	known    []knownhosts.Entry
	knownW   map[int][]string

	lists [nScreens]listState
	vis   [nScreens][]int

	query     string
	sortAlpha bool
	form      *form
	confirm   *confirmState
	notice    string
	final     []string
}

// ---------- pesan ----------

type (
	tickMsg    time.Time
	refreshMsg struct {
		agent    agent.Info
		infos    map[string]infoEntry
		keyPaths []string
		tun      map[string]bool
	}
	knownMsg struct {
		entries []knownhosts.Entry
		warn    map[int][]string
		err     error
	}
	execDoneMsg struct {
		kind  string
		err   error
		extra string
	}
	opMsg struct{ notice string }
)

// New creates a model. hosts may be empty.
func New(p paths.Paths, hosts []sshconfig.Host, st *store.State) Model {
	if st == nil {
		st = &store.State{Hosts: map[string]store.HostMeta{}}
	}
	m := Model{p: p, st: st, hosts: hosts, infos: map[string]infoEntry{}, agent: agent.Info{FPs: map[string]bool{}}, tunUp: map[string]bool{}}
	m.rebuild()
	return m
}

// Run starts the TUI and returns ssh arguments to execute (nil if none).
func Run(p paths.Paths) ([]string, error) {
	hosts, err := sshconfig.ParseConfig(p.Config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	st, serr := store.Load(p.State)
	m := New(p, hosts, st)
	if serr != nil {
		m.notice = "state.json is corrupted; using defaults: " + serr.Error()
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return nil, err
	}
	if fm, ok := final.(Model); ok {
		return fm.final, nil
	}
	return nil, nil
}

// ---------- data turunan ----------

func (m Model) matchHost(h sshconfig.Host) bool {
	q := m.query
	if q == "" {
		return true
	}
	tags := m.st.Meta(h.Alias).Tags
	if strings.HasPrefix(q, "#") {
		for _, t := range tags {
			if strings.Contains(t, strings.ToLower(q[1:])) {
				return true
			}
		}
		return false
	}
	return sshconfig.MatchHost(h, q)
}

func contains(s, q string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(q)) }

func (m *Model) rebuild() {
	// hosts
	var hi []int
	for i, h := range m.hosts {
		if m.sc != scHosts || m.matchHost(h) {
			hi = append(hi, i)
		}
	}
	sort.SliceStable(hi, func(a, b int) bool {
		ha, hb := m.hosts[hi[a]], m.hosts[hi[b]]
		ma, mb := m.st.Meta(ha.Alias), m.st.Meta(hb.Alias)
		if ma.Favorite != mb.Favorite {
			return ma.Favorite
		}
		if m.sortAlpha {
			return strings.ToLower(ha.Alias) < strings.ToLower(hb.Alias)
		}
		if !ma.LastUsed.Equal(mb.LastUsed) {
			return ma.LastUsed.After(mb.LastUsed)
		}
		return false
	})
	m.vis[scHosts] = hi

	filter := func(s screen, n int, match func(i int) bool) []int {
		var out []int
		for i := 0; i < n; i++ {
			if m.query == "" || m.sc != s || match(i) {
				out = append(out, i)
			}
		}
		return out
	}
	m.vis[scKeys] = filter(scKeys, len(m.keyPaths), func(i int) bool { return contains(m.keyPaths[i], m.query) })
	m.vis[scTunnels] = filter(scTunnels, len(m.st.Tunnels), func(i int) bool {
		t := m.st.Tunnels[i]
		return contains(t.Name, m.query) || contains(t.Host, m.query)
	})
	m.vis[scKnown] = filter(scKnown, len(m.known), func(i int) bool {
		e := m.known[i]
		return contains(e.Label(), m.query) || contains(e.Type, m.query)
	})
	for s := screen(0); s < nScreens; s++ {
		m.clampList(s)
	}
}

func (m Model) listRows() int {
	if r := m.h - 5; r > 1 {
		return r
	}
	return 1
}

func (m *Model) clampList(s screen) {
	l := &m.lists[s]
	n := len(m.vis[s])
	if l.cursor >= n {
		l.cursor = n - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if rows := m.listRows(); l.cursor < l.offset {
		l.offset = l.cursor
	} else if l.cursor >= l.offset+rows {
		l.offset = l.cursor - rows + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

// selIndex returns the selected data index (not its position) on screen s.
func (m Model) selIndex(s screen) (int, bool) {
	c := m.lists[s].cursor
	if c < 0 || c >= len(m.vis[s]) {
		return 0, false
	}
	return m.vis[s][c], true
}

func (m Model) selHost() (sshconfig.Host, bool) {
	i, ok := m.selIndex(scHosts)
	if !ok {
		return sshconfig.Host{}, false
	}
	return m.hosts[i], true
}

func (m *Model) focusHost(alias string) {
	for pos, i := range m.vis[scHosts] {
		if m.hosts[i].Alias == alias {
			m.lists[scHosts].cursor = pos
		}
	}
	m.clampList(scHosts)
}

func (m *Model) reloadHosts() {
	hs, err := sshconfig.ParseConfig(m.p.Config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.notice = "failed to read config: " + err.Error()
		return
	}
	m.hosts = hs
	m.rebuild()
}

func (m *Model) save() {
	if err := m.st.Save(m.p.State); err != nil {
		m.notice = "failed to save state: " + err.Error()
	}
}

func (m Model) keyState(path string) keyStatus {
	if !m.ready {
		return ksUnknown
	}
	e, ok := m.infos[path]
	switch {
	case !ok:
		return ksNotFound
	case e.info.FP == "":
		return ksUnknown
	case m.agent.FPs[e.info.FP]:
		return ksLoaded
	}
	return ksNotLoaded
}

func (m Model) keysByState(paths []string) (loaded, missing []string) {
	for _, k := range paths {
		switch m.keyState(k) {
		case ksLoaded:
			loaded = append(loaded, k)
		case ksNotLoaded:
			missing = append(missing, k)
		}
	}
	return
}

func (m Model) hostsUsing(keyPath string) []string {
	var out []string
	for _, h := range m.hosts {
		for _, k := range h.IdentityFiles {
			if k == keyPath {
				out = append(out, h.Alias)
				break
			}
		}
	}
	return out
}

// ---------- commands ----------

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refresh() tea.Cmd {
	old := m.infos
	sshDir := m.p.SSHDir
	var hostKeys []string
	for _, h := range m.hosts {
		hostKeys = append(hostKeys, h.IdentityFiles...)
	}
	var specs []tunnel.Spec
	if m.sc == scTunnels {
		specs = append(specs, m.st.Tunnels...)
	}
	return func() tea.Msg {
		kp := keys.ListPaths(sshDir)
		seen := map[string]bool{}
		infos := map[string]infoEntry{}
		for _, p := range append(append([]string{}, hostKeys...), kp...) {
			if seen[p] {
				continue
			}
			seen[p] = true
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if e, ok := old[p]; ok && e.mtime.Equal(st.ModTime()) {
				infos[p] = e
				continue
			}
			infos[p] = infoEntry{st.ModTime(), keys.Inspect(p)}
		}
		tun := map[string]bool{}
		for _, s := range specs {
			tun[s.ID] = tunnel.Running(s)
		}
		return refreshMsg{agent.Read(), infos, kp, tun}
	}
}

func (m Model) loadKnown() tea.Cmd {
	path := m.p.KnownHosts
	return func() tea.Msg {
		es, err := knownhosts.Parse(path)
		return knownMsg{es, knownhosts.Warnings(es), err}
	}
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.refresh(), m.loadKnown(), tick()) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.rebuild()
	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())
	case refreshMsg:
		m.agent, m.infos, m.keyPaths, m.ready = msg.agent, msg.infos, msg.keyPaths, true
		if m.sc == scTunnels {
			m.tunUp = msg.tun
		}
		m.rebuild()
	case knownMsg:
		m.known, m.knownW = msg.entries, msg.warn
		if msg.err != nil {
			m.notice = "known_hosts: " + msg.err.Error()
		}
		m.rebuild()
	case opMsg:
		m.notice = msg.notice
		return m, tea.Batch(m.refresh(), m.loadKnown())
	case execDoneMsg:
		return m.onExecDone(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	}
	return m, nil
}

func (m Model) onExecDone(msg execDoneMsg) (tea.Model, tea.Cmd) {
	fail := func(prefix string) (tea.Model, tea.Cmd) {
		m.notice = prefix + " failed: " + msg.err.Error()
		return m, m.refresh()
	}
	switch msg.kind {
	case "load", "connect":
		if msg.err != nil {
			return fail("ssh-add")
		}
		if msg.kind == "connect" {
			m.final = sshcmd.Args(msg.extra)
			m.st.Touch(msg.extra)
			m.save()
			return m, tea.Quit
		}
		m.notice = "key loaded"
	case "keygen":
		if msg.err != nil {
			return fail("ssh-keygen")
		}
		m.notice = "key created: " + tilde(msg.extra)
	case "deploy":
		if msg.err != nil {
			return fail("ssh-copy-id")
		}
		m.notice = "public key installed at " + msg.extra
	case "tunnel":
		if msg.err != nil {
			return fail("tunnel")
		}
		m.notice = "tunnel is running"
	}
	return m, m.refresh()
}

func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}
