package sshconfig

import (
	"bufio"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
)

const maxIncludeDepth = 5

// Host represents one alias from ~/.ssh/config.
type Host struct {
	Alias         string
	HostName      string
	User          string
	Port          string
	ProxyJump     string
	IdentityFiles []string // expanded to absolute paths

	// Source information used when writing changes back.
	Source  string   // file containing the Host block
	Line    int      // nomor baris (1-based) baris "Host"
	Aliases []string // all aliases on the same Host line
	Own     Entry    // Values belonging to this block only (without inherited Host * values), as written.
}

// Target returns HostName, or the alias if HostName is empty.
func (h Host) Target() string {
	if h.HostName != "" {
		return h.HostName
	}
	return h.Alias
}

type block struct {
	patterns []string
	opts     map[string][]string
	source   string
	line     int
}

func newBlock(patterns []string) *block {
	return &block{patterns: patterns, opts: map[string][]string{}}
}

type parser struct {
	home   string
	blocks []*block
	cur    *block
}

func newParser(home string) *parser {
	g := newBlock([]string{"*"}) // options before the first Host are global
	return &parser{home: home, blocks: []*block{g}, cur: g}
}

// ParseConfig reads an SSH config file and returns the concrete host list.
// Wildcard patterns and Match blocks are ignored; the "Host *" block provides defaults.
func ParseConfig(path string) ([]Host, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	p := newParser(home)
	if err := p.parseFile(path, 0); err != nil {
		return nil, err
	}
	return p.hosts(), nil
}

func (p *parser) parseFile(path string, depth int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	ln := 0
	for sc.Scan() {
		ln++
		fields := splitFields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key, args := strings.ToLower(fields[0]), fields[1:]
		switch key {
		case "host":
			p.cur = newBlock(args)
			p.cur.source, p.cur.line = path, ln
			p.blocks = append(p.blocks, p.cur)
		case "match":
			p.cur = newBlock(nil) // yatim: opsinya dibuang
		case "include":
			if depth >= maxIncludeDepth {
				continue
			}
			for _, pat := range args {
				p.include(pat, depth+1)
			}
		case "hostname", "user", "port", "proxyjump":
			if _, ok := p.cur.opts[key]; !ok {
				p.cur.opts[key] = []string{args[0]}
			}
		case "identityfile":
			p.cur.opts[key] = append(p.cur.opts[key], args[0])
		}
	}
	return sc.Err()
}

func (p *parser) include(pat string, depth int) {
	pat = p.tilde(pat)
	if !filepath.IsAbs(pat) {
		pat = filepath.Join(p.home, ".ssh", pat)
	}
	matches, _ := filepath.Glob(pat)
	sort.Strings(matches)
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && !st.IsDir() {
			_ = p.parseFile(m, depth)
		}
	}
}

func (p *parser) tilde(s string) string {
	if s == "~" {
		return p.home
	}
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(p.home, s[2:])
	}
	return s
}

func isWildcard(pat string) bool {
	return strings.ContainsAny(pat, "*?!") || pat == "" || strings.HasPrefix(pat, "-")
}

func (p *parser) hosts() []Host {
	defaults := map[string][]string{}
	for _, b := range p.blocks {
		for _, pat := range b.patterns {
			if pat != "*" {
				continue
			}
			for k, v := range b.opts {
				if k == "identityfile" {
					defaults[k] = append(defaults[k], v...)
				} else if _, ok := defaults[k]; !ok {
					defaults[k] = v
				}
			}
			break
		}
	}

	var out []Host
	seen := map[string]bool{}
	for _, b := range p.blocks {
		for _, alias := range b.patterns {
			if isWildcard(alias) || seen[alias] {
				continue
			}
			seen[alias] = true
			val := func(k string) string {
				if v := b.opts[k]; len(v) > 0 {
					return v[0]
				}
				if v := defaults[k]; len(v) > 0 {
					return v[0]
				}
				return ""
			}
			h := Host{
				Alias:     alias,
				HostName:  strings.ReplaceAll(val("hostname"), "%h", alias),
				User:      val("user"),
				Port:      val("port"),
				ProxyJump: val("proxyjump"),

				Source:  b.source,
				Line:    b.line,
				Aliases: b.patterns,
				Own: Entry{
					Alias:         alias,
					HostName:      first(b.opts["hostname"]),
					User:          first(b.opts["user"]),
					Port:          first(b.opts["port"]),
					ProxyJump:     first(b.opts["proxyjump"]),
					IdentityFiles: b.opts["identityfile"],
				},
			}
			dup := map[string]bool{}
			ids := append(append([]string{}, b.opts["identityfile"]...), defaults["identityfile"]...)
			for _, id := range ids {
				id = p.expandPath(id, h)
				if !dup[id] {
					dup[id] = true
					h.IdentityFiles = append(h.IdentityFiles, id)
				}
			}
			out = append(out, h)
		}
	}
	return out
}

// expandPath expands ~ and the %d %h %r %u %n %% tokens in IdentityFile.
// Path relatif dianggap relatif terhadap ~/.ssh.
func (p *parser) expandPath(s string, h Host) string {
	u := ""
	if cu, err := user.Current(); err == nil {
		u = cu.Username
	}
	r := h.User
	if r == "" {
		r = u
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'd':
			b.WriteString(p.home)
		case 'h':
			b.WriteString(h.Target())
		case 'r':
			b.WriteString(r)
		case 'u':
			b.WriteString(u)
		case 'n':
			b.WriteString(h.Alias)
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(s[i])
		}
	}
	out := p.tilde(b.String())
	if !filepath.IsAbs(out) {
		out = filepath.Join(p.home, ".ssh", out)
	}
	return filepath.Clean(out)
}

// splitFields parses one config line: "key value", "key=value",
// "key = value", double quotes, and comments starting with '#'.
func splitFields(line string) []string {
	var toks []string
	var cur strings.Builder
	inQ, has := false, false
	flush := func() {
		if has {
			toks = append(toks, cur.String())
			cur.Reset()
			has = false
		}
	}
loop:
	for _, r := range line {
		switch {
		case r == '"':
			inQ, has = !inQ, true
		case inQ:
			cur.WriteRune(r)
		case r == '#' && !has:
			break loop
		case r == ' ' || r == '\t' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	if len(toks) == 0 {
		return nil
	}
	if i := strings.Index(toks[0], "="); i > 0 {
		rest := toks[0][i+1:]
		toks[0] = toks[0][:i]
		if rest != "" {
			toks = append(toks[:1], append([]string{rest}, toks[1:]...)...)
		}
	} else if len(toks) > 1 && strings.HasPrefix(toks[1], "=") {
		if toks[1] == "=" {
			toks = append(toks[:1], toks[2:]...)
		} else {
			toks[1] = toks[1][1:]
		}
	}
	return toks
}

// MatchHost searches aliases, HostName, and User for a case-insensitive substring.
func MatchHost(h Host, q string) bool {
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(h.Alias), q) ||
		strings.Contains(strings.ToLower(h.HostName), q) ||
		strings.Contains(strings.ToLower(h.User), q)
}

// HasAlias reports whether the alias is already in the host list.
func HasAlias(hosts []Host, alias string) bool {
	for _, h := range hosts {
		if h.Alias == alias {
			return true
		}
	}
	return false
}

func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}
