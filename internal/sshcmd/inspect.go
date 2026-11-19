package sshcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type IdentityFile struct {
	Path       string
	CheckError error
}

type EffectiveConfig struct {
	HostName      string
	User          string
	Port          string
	ProxyJump     string
	IdentityFiles []IdentityFile
}

// Inspect reports the settings OpenSSH would use for the same alias as Args.
func Inspect(ctx context.Context, alias string) (EffectiveConfig, error) {
	argv := Args(alias)
	values, err := dumpConfig(ctx, argv[1:]...)
	if err != nil {
		return EffectiveConfig{}, err
	}
	cfg := EffectiveConfig{
		HostName:  firstValue(values, "hostname"),
		User:      firstValue(values, "user"),
		Port:      firstValue(values, "port"),
		ProxyJump: firstValue(values, "proxyjump"),
	}
	for _, path := range values["identityfile"] {
		file := IdentityFile{Path: path}
		if !strings.EqualFold(path, "none") {
			file.CheckError = checkIdentityFile(ctx, path, alias, cfg, firstValue(values, "hostkeyalias"))
		}
		cfg.IdentityFiles = append(cfg.IdentityFiles, file)
	}
	return cfg, nil
}

func dumpConfig(ctx context.Context, args ...string) (map[string][]string, error) {
	out, err := exec.CommandContext(ctx, "ssh", append([]string{"-G"}, args...)...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("ssh -G: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("ssh -G: %w", err)
	}
	values := make(map[string][]string)
	for _, line := range strings.Split(string(out), "\n") {
		if key, value, ok := strings.Cut(line, " "); ok {
			values[key] = append(values[key], value)
		}
	}
	return values, nil
}

func firstValue(values map[string][]string, key string) string {
	if fields := values[key]; len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func checkIdentityFile(ctx context.Context, path, alias string, cfg EffectiveConfig, keyAlias string) error {
	if strings.HasPrefix(path, "~") || strings.ContainsAny(path, "%$") {
		// ssh -G prints IdentityFile literally. ControlPath uses the same path
		// expansion, so ask OpenSSH to expand it with the effective connection
		// settings and an empty config to avoid rerunning Match directives.
		args := []string{"-F", os.DevNull, "-S", path,
			"-o", "HostName=" + strings.ReplaceAll(cfg.HostName, "%", "%%"), "-l", cfg.User, "-p", cfg.Port}
		if cfg.ProxyJump != "" {
			args = append(args, "-J", cfg.ProxyJump)
		}
		if keyAlias != "" {
			args = append(args, "-o", "HostKeyAlias="+keyAlias)
		}
		values, err := dumpConfig(ctx, append(args, "--", alias)...)
		if err != nil {
			return err
		}
		path = firstValue(values, "controlpath")
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return os.ErrNotExist
	}
	return err
}
