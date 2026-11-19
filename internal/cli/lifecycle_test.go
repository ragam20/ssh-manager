package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testInstallation(t *testing.T) installation {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "custom prefix", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sshm")
	if err := os.WriteFile(path, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	target, err := findInstallation(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

type releaseTransport func(*http.Request) (*http.Response, error)

func (fn releaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestUpdateInstallation(t *testing.T) {
	const asset = "sshm-linux-amd64"
	const payload = "new binary that cannot be executed"
	for _, mode := range []string{"success", "wrong checksum", "missing checksum", "download failure", "rename failure"} {
		t.Run(mode, func(t *testing.T) {
			target := testInstallation(t)
			dir := filepath.Dir(target.path)
			handler := func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/SHA256SUMS":
					if mode == "missing checksum" {
						return
					}
					content := payload
					if mode == "wrong checksum" {
						content = "different binary"
					}
					fmt.Fprintf(w, "%x  %s\n", sha256.Sum256([]byte(content)), asset)
				case "/" + asset:
					if mode == "download failure" {
						http.NotFound(w, r)
						return
					}
					if mode == "rename failure" {
						// The open staged file is still writable after unlink, but rename fails.
						stages, err := filepath.Glob(filepath.Join(dir, ".sshm-update-*"))
						if err != nil || len(stages) != 1 {
							t.Errorf("staged file: %v, %v", stages, err)
						} else if err := os.Remove(stages[0]); err != nil {
							t.Error(err)
						}
					}
					fmt.Fprint(w, payload)
				default:
					http.NotFound(w, r)
				}
			}
			client := &http.Client{Transport: releaseTransport(func(req *http.Request) (*http.Response, error) {
				response := httptest.NewRecorder()
				handler(response, req)
				return response.Result(), nil
			})}

			want := "old binary"
			err := target.update(client, "https://releases.example/", asset)
			if mode == "success" {
				want = payload
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("update succeeded unexpectedly")
			}
			if got, err := os.ReadFile(target.path); err != nil || string(got) != want {
				t.Fatalf("binary: %q, %v", got, err)
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
				t.Fatalf("staged file left behind: %v, %v", entries, err)
			}
		})
	}
}

func TestUninstallInstallation(t *testing.T) {
	for _, tc := range []struct {
		input       string
		yes, remove bool
	}{{"yes\n", false, true}, {"no\n", false, false}, {"", false, false}, {"", true, true}} {
		target := testInstallation(t)
		state := filepath.Join(filepath.Dir(filepath.Dir(target.path)), "state.json")
		if err := os.WriteFile(state, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := target.uninstall(strings.NewReader(tc.input), &out, tc.yes); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(target.path)
		if (tc.remove && !os.IsNotExist(err)) || (!tc.remove && err != nil) {
			t.Fatalf("unexpected uninstall result: %v (%s)", err, &out)
		}
		if data, err := os.ReadFile(state); err != nil || string(data) != "keep" {
			t.Fatalf("user data: %q, %v", data, err)
		}
	}
}

func TestUninstallRejectsReplacedExecutable(t *testing.T) {
	target := testInstallation(t)
	if err := os.Rename(target.path, target.path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.path, []byte("other executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := target.uninstall(strings.NewReader(""), io.Discard, true); err == nil {
		t.Fatal("expected refusal after executable was replaced")
	}
	if data, err := os.ReadFile(target.path); err != nil || string(data) != "other executable" {
		t.Fatalf("replacement: %q, %v", data, err)
	}
}
