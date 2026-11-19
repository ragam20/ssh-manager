// Package clipboard copies text to the system clipboard, with an OSC 52 fallback.
package clipboard

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strings"
)

type tool struct {
	name string
	args []string
	ok   func() bool
}

func tools() []tool {
	return []tool{
		{"pbcopy", nil, func() bool { return true }},
		{"wl-copy", nil, func() bool { return os.Getenv("WAYLAND_DISPLAY") != "" }},
		{"xclip", []string{"-selection", "clipboard"}, func() bool { return os.Getenv("DISPLAY") != "" }},
		{"xsel", []string{"--clipboard", "--input"}, func() bool { return os.Getenv("DISPLAY") != "" }},
	}
}

// Copy copies text and returns the name of the method used.
// If no clipboard utility is available (for example, over SSH), it uses OSC 52 in the terminal.
func Copy(text string) (string, error) {
	for _, t := range tools() {
		if !t.ok() {
			continue
		}
		path, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, t.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return t.name, nil
		}
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return "", errors.New("no clipboard utility (wl-copy/xclip/xsel/pbcopy) or terminal is available")
	}
	defer tty.Close()
	if _, err := tty.WriteString(OSC52(text)); err != nil {
		return "", err
	}
	return "OSC 52", nil
}

// OSC52 menyusun escape sequence penyalinan ke clipboard terminal.
func OSC52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}
