// Package cli handles non-interactive subcommands and opens the TUI.
package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"sshm/internal/agent"
	"sshm/internal/doctor"
	"sshm/internal/keys"
	"sshm/internal/paths"
	"sshm/internal/sshcmd"
	"sshm/internal/sshconfig"
	"sshm/internal/store"
	"sshm/internal/tui"
)

//go:embed VERSION
var versionFile string

var Version = strings.TrimSpace(versionFile)

const usage = `sshm — SSH management TUI

Usage:
  sshm                      open the TUI
  sshm list [-n] [--json]   list hosts (-n: aliases only)
  sshm connect <host>       connect directly (auto-load key)
  sshm env                  print "export SSH_AUTH_SOCK=..." (eval "$(sshm env)")
  sshm doctor               inspect the SSH environment
  sshm update               install the latest stable release
  sshm uninstall [--yes]    remove the installed executable
  sshm completion bash|zsh  print completion script
  sshm version | help

Environment:
  SSHM_CONFIG   alternate SSH config path (default ~/.ssh/config);
                also used with "ssh -F"
`

// Run runs sshm and returns the exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	p := paths.Default()
	if len(args) == 0 {
		return runTUI(p, stderr)
	}
	switch args[0] {
	case "list", "ls":
		return list(p, args[1:], stdout, stderr)
	case "connect", "c":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: sshm connect <host>")
			return 2
		}
		return fail(stderr, connect(p, args[1]))
	case "env":
		if err := agent.Ensure(); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "export SSH_AUTH_SOCK=%q\n", os.Getenv("SSH_AUTH_SOCK"))
		if pid := os.Getenv("SSH_AGENT_PID"); pid != "" {
			fmt.Fprintf(stdout, "export SSH_AGENT_PID=%q\n", pid)
		}
		return 0
	case "doctor":
		return doctor.Run(stdout, p)
	case "update", "uninstall":
		return lifecycle(args, stdin, stdout, stderr)
	case "completion":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: sshm completion bash|zsh")
			return 2
		}
		return completion(args[1], stdout, stderr)
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, "sshm", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "sshm: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func fail(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "sshm:", err)
	return 1
}

func runTUI(p paths.Paths, stderr io.Writer) int {
	final, err := tui.Run(p)
	if err != nil {
		return fail(stderr, err)
	}
	if len(final) > 0 {
		return fail(stderr, sshcmd.Exec(final)) // only returns if exec fails
	}
	return 0
}

func loadHosts(p paths.Paths) ([]sshconfig.Host, error) {
	hs, err := sshconfig.ParseConfig(p.Config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read %s: %w", p.Config, err)
	}
	return hs, nil
}

type listItem struct {
	Alias     string     `json:"alias"`
	HostName  string     `json:"hostname,omitempty"`
	User      string     `json:"user,omitempty"`
	Port      string     `json:"port,omitempty"`
	ProxyJump string     `json:"proxy_jump,omitempty"`
	Keys      []string   `json:"identity_files,omitempty"`
	Favorite  bool       `json:"favorite,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

func list(p paths.Paths, args []string, stdout, stderr io.Writer) int {
	names, asJSON := false, false
	for _, a := range args {
		switch a {
		case "-n", "--names":
			names = true
		case "--json":
			asJSON = true
		default:
			fmt.Fprintf(stderr, "sshm list: unknown option %q\n", a)
			return 2
		}
	}
	hosts, err := loadHosts(p)
	if err != nil {
		return fail(stderr, err)
	}
	st, _ := store.Load(p.State)
	var items []listItem
	for _, h := range hosts {
		m := st.Meta(h.Alias)
		var last *time.Time
		if !m.LastUsed.IsZero() {
			last = &m.LastUsed
		}
		items = append(items, listItem{h.Alias, h.HostName, h.User, h.Port, h.ProxyJump, h.IdentityFiles, m.Favorite, m.Tags, last})
	}
	switch {
	case names:
		for _, it := range items {
			fmt.Fprintln(stdout, it.Alias)
		}
	case asJSON:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if items == nil {
			items = []listItem{}
		}
		if err := enc.Encode(items); err != nil {
			return fail(stderr, err)
		}
	default:
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ALIAS\tTARGET\tUSER\tPORT\tTAGS")
		for _, it := range items {
			t := it.HostName
			if t == "" {
				t = it.Alias
			}
			star := ""
			if it.Favorite {
				star = "★ "
			}
			fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\t%s\n", star, it.Alias, t, it.User, it.Port, strings.Join(it.Tags, ","))
		}
		tw.Flush()
	}
	return 0
}

func connect(p paths.Paths, alias string) error {
	hosts, err := loadHosts(p)
	if err != nil {
		return err
	}
	var h *sshconfig.Host
	var near []string
	for i := range hosts {
		if hosts[i].Alias == alias {
			h = &hosts[i]
		} else if strings.Contains(strings.ToLower(hosts[i].Alias), strings.ToLower(alias)) {
			near = append(near, hosts[i].Alias)
		}
	}
	if h == nil {
		msg := fmt.Sprintf("host %q not found", alias)
		if len(near) > 0 {
			msg += "; mungkin maksud Anda: " + strings.Join(near, ", ")
		}
		return errors.New(msg)
	}
	if err := agent.Ensure(); err != nil {
		return err
	}
	st, _ := store.Load(p.State)
	if missing := missingKeys(h.IdentityFiles); len(missing) > 0 {
		cmd := agent.LoadCmd(st.KeyLifetime, missing)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ssh-add failed: %w", err)
		}
	}
	st.Touch(alias)
	_ = st.Save(p.State)
	return sshcmd.Exec(sshcmd.Args(alias))
}

// missingKeys returns keys present on disk that are not yet loaded in the agent.
func missingKeys(paths []string) []string {
	loaded := agent.Read().FPs
	var out []string
	for _, k := range paths {
		if _, err := os.Stat(k); err != nil {
			continue
		}
		if fp := keys.Inspect(k).FP; fp != "" && !loaded[fp] {
			out = append(out, k)
		}
	}
	return out
}

const bashCompletion = `_sshm() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "list connect env doctor update uninstall completion version help" -- "$cur"))
    return
  fi
  case "${COMP_WORDS[1]}" in
    connect|c) COMPREPLY=($(compgen -W "$(sshm list --names 2>/dev/null)" -- "$cur")) ;;
    completion) COMPREPLY=($(compgen -W "bash zsh" -- "$cur")) ;;
    list|ls) COMPREPLY=($(compgen -W "--names --json" -- "$cur")) ;;
    uninstall) COMPREPLY=($(compgen -W "--yes" -- "$cur")) ;;
  esac
}
complete -F _sshm sshm
`

const zshCompletion = `#compdef sshm
_sshm() {
  local -a cmds
  cmds=(list:'list hosts' connect:'connect to host' env:'print export SSH_AUTH_SOCK' doctor:'inspect the environment' update:'install the latest stable release' uninstall:'remove the installed executable' completion:'completion script' version:'show version' help:'help')
  if (( CURRENT == 2 )); then
    _describe 'perintah' cmds
    return
  fi
  case $words[2] in
    connect|c) compadd -- ${(f)"$(sshm list --names 2>/dev/null)"} ;;
    completion) compadd bash zsh ;;
    list|ls) compadd -- --names --json ;;
    uninstall) compadd -- --yes ;;
  esac
}
compdef _sshm sshm
`

func completion(shell string, stdout, stderr io.Writer) int {
	switch shell {
	case "bash":
		fmt.Fprint(stdout, bashCompletion)
	case "zsh":
		fmt.Fprint(stdout, zshCompletion)
	default:
		fmt.Fprintf(stderr, "unsupported shell: %q (bash or zsh)\n", shell)
		return 2
	}
	return 0
}
