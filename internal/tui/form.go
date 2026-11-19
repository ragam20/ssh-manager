package tui

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"sshm/internal/keys"
	"sshm/internal/sshconfig"
	"sshm/internal/tunnel"
)

type field struct {
	label, hint string
	val         []rune
	pos         int
}

type form struct {
	title  string
	fields []field
	idx    int
	err    string
	submit func(m Model, v []string) (Model, tea.Cmd, error)
}

func newField(label, hint, val string) field {
	r := []rune(val)
	return field{label: label, hint: hint, val: r, pos: len(r)}
}

func (m Model) openForm(f *form) Model {
	m.mode, m.form = mForm, f
	return m
}

func (f *form) values() []string {
	out := make([]string, len(f.fields))
	for i, fl := range f.fields {
		out[i] = strings.TrimSpace(string(fl.val))
	}
	return out
}

func (m Model) formKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.form
	fl := &f.fields[f.idx]
	k := msg.String()
	f.err = ""
	switch k {
	case "esc":
		m.mode, m.form = mNormal, nil
		return m, nil
	case "tab", "down":
		f.idx = (f.idx + 1) % len(f.fields)
	case "shift+tab", "up":
		f.idx = (f.idx + len(f.fields) - 1) % len(f.fields)
	case "enter", "ctrl+s":
		if k == "enter" && f.idx < len(f.fields)-1 {
			f.idx++
			return m, nil
		}
		nm, cmd, err := f.submit(m, f.values())
		if err != nil {
			f.err = err.Error()
			return m, nil
		}
		nm.mode, nm.form = mNormal, nil
		return nm, cmd
	case "left":
		if fl.pos > 0 {
			fl.pos--
		}
	case "right":
		if fl.pos < len(fl.val) {
			fl.pos++
		}
	case "home", "ctrl+a":
		fl.pos = 0
	case "end", "ctrl+e":
		fl.pos = len(fl.val)
	case "ctrl+u":
		fl.val, fl.pos = fl.val[fl.pos:], 0
	case "backspace":
		if fl.pos > 0 {
			fl.val = append(fl.val[:fl.pos-1], fl.val[fl.pos:]...)
			fl.pos--
		}
	case "delete", "ctrl+d":
		if fl.pos < len(fl.val) {
			fl.val = append(fl.val[:fl.pos], fl.val[fl.pos+1:]...)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			var in []rune
			for _, r := range msg.Runes {
				if !unicode.IsControl(r) { // remove newlines from pasted text
					in = append(in, r)
				}
			}
			fl.val = append(fl.val[:fl.pos], append(in, fl.val[fl.pos:]...)...)
			fl.pos += len(in)
		}
	}
	return m, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// hostForm creates a form to add (h == nil) or edit a host.
func (m Model) hostForm(h *sshconfig.Host) *form {
	var e sshconfig.Entry
	title := "Add host"
	if h != nil {
		e, title = h.Own, "Edit host: "+h.Alias
	}
	f := &form{title: title, fields: []field{
		newField("Alias", "name entered after `ssh`", e.Alias),
		newField("HostName", "alamat/IP server", e.HostName),
		newField("User", "", e.User),
		newField("Port", "empty = 22", e.Port),
		newField("ProxyJump", "mis. bastion", e.ProxyJump),
		newField("IdentityFile", "comma-separated; ~ is allowed", strings.Join(e.IdentityFiles, ", ")),
	}}
	var edit *sshconfig.Host
	if h != nil {
		c := *h
		edit = &c
	}
	f.submit = func(m Model, v []string) (Model, tea.Cmd, error) {
		ne := sshconfig.Entry{Alias: v[0], HostName: v[1], User: v[2], Port: v[3], ProxyJump: v[4], IdentityFiles: splitList(v[5])}
		if err := sshconfig.ValidateEntry(ne); err != nil {
			return m, nil, err
		}
		if (edit == nil || ne.Alias != edit.Alias) && sshconfig.HasAlias(m.hosts, ne.Alias) {
			return m, nil, errors.New("alias already exists")
		}
		var err error
		if edit == nil {
			err = sshconfig.AddHost(m.p.Config, m.p.BackupDir, ne)
		} else if err = sshconfig.UpdateHost(edit.Source, edit.Line, edit.Alias, ne, m.p.BackupDir); err == nil && ne.Alias != edit.Alias {
			m.st.Rename(edit.Alias, ne.Alias)
			m.save()
		}
		if err != nil {
			return m, nil, err
		}
		m.reloadHosts()
		m.focusHost(ne.Alias)
		if edit == nil {
			m.notice = "host added: " + ne.Alias
		} else {
			m.notice = "host saved: " + ne.Alias
		}
		return m, m.refresh(), nil
	}
	return f
}

func (m Model) tagForm(h sshconfig.Host) *form {
	return &form{title: "Tags for " + h.Alias,
		fields: []field{newField("Tag", "comma-separated, e.g. work, prod", strings.Join(m.st.Meta(h.Alias).Tags, ", "))},
		submit: func(m Model, v []string) (Model, tea.Cmd, error) {
			m.st.SetTags(h.Alias, splitList(v[0]))
			m.save()
			m.rebuild()
			m.focusHost(h.Alias)
			m.notice = "tags saved"
			return m, nil, nil
		}}
}

func (m Model) keygenForm() *form {
	u, _ := user.Current()
	host, _ := os.Hostname()
	comment := host
	if u != nil {
		comment = u.Username + "@" + host
	}
	f := &form{title: "Create ed25519 key", fields: []field{
		newField("Filename", "saved in ~/.ssh, e.g. id_ed25519_work", "id_ed25519_"),
		newField("Comment", "", comment),
		newField("Passphrase (y/n)", "y = ssh-keygen will prompt; sshm does not store it", "y"),
	}}
	f.submit = func(m Model, v []string) (Model, tea.Cmd, error) {
		if err := keys.ValidName(v[0]); err != nil {
			return m, nil, err
		}
		path := filepath.Join(m.p.SSHDir, v[0])
		for _, p := range []string{path, path + ".pub"} {
			if _, err := os.Stat(p); err == nil {
				return m, nil, errors.New(v[0] + " already exists")
			}
		}
		if !strings.EqualFold(v[2], "y") && !strings.EqualFold(v[2], "n") {
			return m, nil, errors.New("passphrase: enter y or n")
		}
		pass := strings.EqualFold(v[2], "y")
		if err := os.MkdirAll(m.p.SSHDir, 0o700); err != nil {
			return m, nil, err
		}
		cmd := keys.GenerateCmd(path, v[1], pass)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return execDoneMsg{kind: "keygen", err: err, extra: path}
		}), nil
	}
	return f
}

func (m Model) tunnelForm(t *tunnel.Spec) *form {
	var s tunnel.Spec
	title := "Add tunnel"
	if t != nil {
		s, title = *t, "Edit tunnel: "+t.Name
	} else if h, ok := m.selHost(); ok {
		s.Host, s.Kind = h.Alias, "L"
	}
	port := func(p int) string {
		if p == 0 {
			return ""
		}
		return strconv.Itoa(p)
	}
	f := &form{title: title, fields: []field{
		newField("Nama", "", s.Name),
		newField("Host", "alias from config", s.Host),
		newField("Type", "L = local, R = remote, D = SOCKS", s.Kind),
		newField("Bind", "opsional, mis. 127.0.0.1", s.Bind),
		newField("Port listen", "", port(s.ListenPort)),
		newField("Destination host", "not used for D", s.DestHost),
		newField("Destination port", "not used for D", port(s.DestPort)),
	}}
	f.submit = func(m Model, v []string) (Model, tea.Cmd, error) {
		ns := tunnel.Spec{ID: s.ID, Name: v[0], Host: v[1], Kind: strings.ToUpper(v[2]), Bind: v[3], DestHost: v[5]}
		if ns.ID == "" {
			ns.ID = tunnel.NewID()
		}
		var err error
		if ns.ListenPort, err = tunnel.ParsePort(v[4]); err != nil {
			return m, nil, errors.New("port listen: " + err.Error())
		}
		if ns.Kind != "D" {
			if ns.DestPort, err = tunnel.ParsePort(v[6]); err != nil {
				return m, nil, errors.New("destination port: " + err.Error())
			}
		} else {
			ns.DestHost = ""
		}
		if ns.Name == "" {
			ns.Name = ns.Describe()
		}
		if err := ns.Validate(); err != nil {
			return m, nil, err
		}
		if !sshconfig.HasAlias(m.hosts, ns.Host) {
			return m, nil, errors.New("host " + ns.Host + " does not exist di config")
		}
		m.st.SaveTunnel(ns)
		m.save()
		m.rebuild()
		m.notice = "tunnel saved (restart it if currently active)"
		return m, nil, nil
	}
	return f
}
