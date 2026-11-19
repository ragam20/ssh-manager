// Package store saves sshm-owned data (favorites, tags, history, tunnels,
// settings) in ~/.config/sshm/state.json. The host list remains in ~/.ssh/config.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sshm/internal/fsutil"
	"sshm/internal/tunnel"
)

type HostMeta struct {
	Favorite bool      `json:"favorite,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	LastUsed time.Time `json:"last_used,omitempty"`
}

type State struct {
	Hosts       map[string]HostMeta `json:"hosts"`
	Tunnels     []tunnel.Spec       `json:"tunnels"`
	KeyLifetime int                 `json:"key_lifetime_seconds"` // 0 = tanpa batas
}

// DefaultPath follows XDG_CONFIG_HOME.
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		h, _ := os.UserHomeDir()
		dir = filepath.Join(h, ".config")
	}
	return filepath.Join(dir, "sshm", "state.json")
}

// Load reads state; a missing file produces an empty state.
func Load(path string) (*State, error) {
	s := &State{Hosts: map[string]HostMeta{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return &State{Hosts: map[string]HostMeta{}}, err
	}
	if s.Hosts == nil {
		s.Hosts = map[string]HostMeta{}
	}
	return s, nil
}

func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(path, append(b, '\n'), 0o600)
}

func (s *State) Meta(alias string) HostMeta { return s.Hosts[alias] }

func (s *State) ToggleFavorite(alias string) bool {
	m := s.Hosts[alias]
	m.Favorite = !m.Favorite
	s.Hosts[alias] = m
	return m.Favorite
}

// SetTags saves tags (lowercase, unique, sorted).
func (s *State) SetTags(alias string, tags []string) {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	m := s.Hosts[alias]
	m.Tags = out
	s.Hosts[alias] = m
}

func (s *State) Touch(alias string) {
	m := s.Hosts[alias]
	m.LastUsed = time.Now()
	s.Hosts[alias] = m
}

// Rename moves metadata when an alias changes; Forget removes it.
func (s *State) Rename(from, to string) {
	if m, ok := s.Hosts[from]; ok && from != to {
		s.Hosts[to] = m
		delete(s.Hosts, from)
		for i := range s.Tunnels {
			if s.Tunnels[i].Host == from {
				s.Tunnels[i].Host = to
			}
		}
	}
}

func (s *State) Forget(alias string) { delete(s.Hosts, alias) }

// SaveTunnel adds or replaces a tunnel by ID.
func (s *State) SaveTunnel(t tunnel.Spec) {
	for i := range s.Tunnels {
		if s.Tunnels[i].ID == t.ID {
			s.Tunnels[i] = t
			return
		}
	}
	s.Tunnels = append(s.Tunnels, t)
}

func (s *State) DeleteTunnel(id string) {
	out := s.Tunnels[:0]
	for _, t := range s.Tunnels {
		if t.ID != id {
			out = append(out, t)
		}
	}
	s.Tunnels = out
}

// LifetimeOptions lists key timeout options for the agent (seconds).
var LifetimeOptions = []int{0, 3600, 4 * 3600, 8 * 3600}

// NextLifetime cycles to the next option.
func (s *State) NextLifetime() int {
	for i, v := range LifetimeOptions {
		if v == s.KeyLifetime {
			s.KeyLifetime = LifetimeOptions[(i+1)%len(LifetimeOptions)]
			return s.KeyLifetime
		}
	}
	s.KeyLifetime = 0
	return 0
}
