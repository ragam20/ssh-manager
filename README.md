# sshm

A personal TUI for managing SSH, written in Go with Bubble Tea.
`~/.ssh/config` remains the source of truth for hosts; `sshm` invokes the
`ssh`, `ssh-add`, `ssh-agent`, `ssh-keygen`, and `ssh-copy-id` binaries and
**never stores passphrases**.

## Install

Prebuilt static binaries (no Go required) are available in `bin/` for Linux
and macOS, amd64 and arm64:

    sh install.sh                   # to ~/.local/bin
    PREFIX=/usr/local sudo sh install.sh

If a matching prebuilt binary is unavailable, `install.sh` builds from source
(requires Go, Make, and internet access to download modules):

    make install                    # build and install to ~/.local/bin
    make test

## Screens (Tab or 1-4; mouse supported)

| Screen | Features |
|---|---|
| Hosts | Connect (auto-load key), load/unload key, add/edit/delete hosts (preserves comments and config order, creates automatic backups), favorites ★, tags, recently used order, deploy and copy public keys, search (`#tag`) |
| Keys | List keys in `~/.ssh`, agent status, weak-key and loose-permission warnings, create ed25519 keys, copy public keys, set agent key timeout (`ssh-add -t`) |
| Tunnels | Save local/remote/SOCKS tunnels, start/stop (ControlMaster), view status |
| Known | Audit `known_hosts`: weak keys, duplicate keys per host, revoked keys; delete entries (automatic backup) |

## Commands

    sshm list [-n] [--json]     list hosts
    sshm connect <host>         connect directly
    sshm env                    eval "$(sshm env)"
    sshm doctor                 inspect the SSH environment
    sshm completion bash|zsh    print shell completion script

`SSHM_CONFIG` overrides the SSH config path (also used with `ssh -F`).

## Data owned by sshm

- `~/.config/sshm/state.json` — favorites, tags, history, tunnels, key timeouts
- `~/.local/state/sshm/backups/` — backups of config and known_hosts before changes (20 most recent)

## Code layout

    cmd/sshm/             thin entry point
    internal/cli/         non-interactive commands and completion
    internal/tui/         Bubble Tea model (model, input, actions, form, view)
    internal/sshconfig/   parser and writer for ~/.ssh/config
    internal/agent/       ssh-agent / ssh-add
    internal/keys/        key inspection, listing, and creation
    internal/knownhosts/  parser, audit, and deletion
    internal/tunnel/      tunnel definitions and control
    internal/store/       state.json
    internal/doctor/      SSH environment checks
    internal/clipboard/   copy (wl-copy/xclip/xsel/pbcopy, OSC 52 fallback)
    internal/fsutil/      atomic writes and backups
    internal/sshcmd/      ssh arguments and execution
    internal/paths/       file locations
