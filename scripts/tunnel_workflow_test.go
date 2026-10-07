package scripts

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTunnelProfileDirectoryAndDefaults(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  []string
		want string
	}{
		{name: "home fallback", want: "home/.config/tunnel-client"},
		{name: "XDG", env: []string{"XDG_CONFIG_HOME=xdg"}, want: "xdg/tunnel-client"},
		{name: "client override", env: []string{"XDG_CONFIG_HOME=xdg", "TUNNEL_CLIENT_PROFILE_DIR=client profiles"}, want: "client profiles"},
		{name: "tldw override", env: []string{"TUNNEL_CLIENT_PROFILE_DIR=client", "TLDW_TUNNEL_PROFILE_DIR=custom profiles"}, want: "custom profiles"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newTunnelFixture(t)
			fixture.env = append(fixture.env, tt.env...)
			for _, recipe := range []string{"tunnel-init", "tunnel-doctor", "tunnel-update"} {
				fixture.run(t, false, recipe)
			}
			log := fixture.log(t)
			for _, want := range []string{"[--profile-dir][" + tt.want + "]", "[--health-listen-addr][127.0.0.1:8080]", "[--mcp-server-url][http://127.0.0.1:8765/mcp]", "[8765][" + tt.want + "]"} {
				if !strings.Contains(log, want) {
					t.Errorf("missing %q in commands:\n%s", want, log)
				}
			}
			// Foreground runs select the same directory as init, editor, and launchd.
			output := fixture.run(t, false, "--dry-run", "tunnel-run")
			if !strings.Contains(output, `--profile-dir "`+tt.want+`"`) {
				t.Fatalf("foreground profile directory missing:\n%s", output)
			}
		})
	}
}

func TestTunnelUpdateValidatesBeforeReinstalling(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("editor_failure=%t", fail), func(t *testing.T) {
			fixture := newTunnelFixture(t)
			if fail {
				fixture.env = append(fixture.env, "TEST_EDITOR_EXIT=1")
			}
			fixture.run(t, fail, "tunnel-update")
			log := fixture.log(t)
			if !strings.Contains(log, "client[profiles][edit][tldw]") {
				t.Fatalf("profile editor was not called:\n%s", log)
			}
			if installed := strings.Contains(log, "launchd[install]"); installed != !fail {
				t.Fatalf("unexpected install after editor result:\n%s", log)
			}
			if !fail && strings.Index(log, "client[profiles][edit]") > strings.Index(log, "launchd[install]") {
				t.Fatalf("service restarted before editing:\n%s", log)
			}
			for _, forbidden := range []string{"[--force]", "[uninstall]", "client[init]"} {
				if strings.Contains(log, forbidden) {
					t.Fatalf("destructive update command %s:\n%s", forbidden, log)
				}
			}
		})
	}
}

func TestTunnelInitUsesCustomSettings(t *testing.T) {
	fixture := newTunnelFixture(t)
	fixture.env = append(fixture.env,
		"TLDW_TUNNEL_PROFILE=custom",
		"TLDW_MCP_HTTP_HOST=127.0.0.2",
		"TLDW_MCP_HTTP_PORT=9876",
		"TLDW_TUNNEL_HEALTH_ADDR=127.0.0.1:18080",
	)
	fixture.run(t, false, "tunnel-init")
	log := fixture.log(t)
	for _, want := range []string{"[--profile][custom]", "[--mcp-server-url][http://127.0.0.2:9876/mcp]", "[--health-listen-addr][127.0.0.1:18080]"} {
		if !strings.Contains(log, want) {
			t.Errorf("missing custom setting %q in:\n%s", want, log)
		}
	}
}

func TestTunnelInitFailureDoesNotInstallService(t *testing.T) {
	fixture := newTunnelFixture(t)
	fixture.env = append(fixture.env, "TEST_INIT_EXIT=1")
	fixture.run(t, true, "tunnel-init")
	log := fixture.log(t)
	if strings.Contains(log, "[--force]") || strings.Contains(log, "launchd") {
		t.Fatalf("init should not replace a profile or restart the service:\n%s", log)
	}
}

func TestUpdateTunnelHTTPDelegatesFromAnotherDirectory(t *testing.T) {
	fixture := newTunnelFixture(t)
	cmd := exec.Command("bash", filepath.Join(fixture.dir, "scripts", "update-tunnel-http"))
	cmd.Dir = t.TempDir()
	cmd.Env = fixture.env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compatibility wrapper failed: %v\n%s", err, output)
	}
	log := fixture.log(t)
	if !strings.Contains(log, "client[profiles][edit]") || !strings.Contains(log, "launchd[install]") {
		t.Fatalf("wrapper did not use the shared workflow:\n%s", log)
	}
}

func TestLaunchdRunAcceptsOldAndNewProfileArguments(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			fixture := newTunnelFixture(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			port := listener.Addr().(*net.TCPAddr).Port
			args := []string{"-c", `source ./tunnel-launchd; run_agent "$@"`, "test", "service", filepath.Join(fixture.dir, "bin", "tunnel-client"), "tldw", filepath.Join(fixture.dir, "bin", "mcp"), "127.0.0.1", fmt.Sprint(port)}
			want := "xdg/tunnel-client"
			if explicit {
				want = "custom profiles"
				args = append(args, want)
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = append(fixture.env, "CONTROL_PLANE_API_KEY=local-test-key", "XDG_CONFIG_HOME=xdg")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("launchd runner failed: %v\n%s", err, output)
			}
			if log := fixture.log(t); !strings.Contains(log, "client[run][--profile][tldw][--profile-dir]["+want+"]") {
				t.Fatalf("unexpected runtime profile selection:\n%s", log)
			}
		})
	}
}

type tunnelFixture struct {
	dir string
	env []string
}

func newTunnelFixture(t *testing.T) *tunnelFixture {
	t.Helper()
	if _, err := exec.LookPath("just"); err != nil {
		t.Skip("just is required for tunnel recipe integration tests")
	}
	dir := t.TempDir()
	write := func(path string, content []byte) {
		t.Helper()
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"Justfile", "scripts/update-tunnel-http"} {
		content, err := os.ReadFile(filepath.Join("..", file))
		if err != nil {
			t.Fatal(err)
		}
		write(file, content)
	}
	write("bin/tunnel-client", []byte(`#!/usr/bin/env bash
printf 'client' >> "$TEST_LOG"
printf '[%s]' "$@" >> "$TEST_LOG"
printf '\n' >> "$TEST_LOG"
if [[ "$1" == profiles && "$2" == edit ]]; then exit "${TEST_EDITOR_EXIT:-0}"; fi
if [[ "$1" == init ]]; then exit "${TEST_INIT_EXIT:-0}"; fi
`))
	write("scripts/tunnel-launchd", []byte(`#!/usr/bin/env bash
printf 'launchd' >> "$TEST_LOG"
printf '[%s]' "$@" >> "$TEST_LOG"
printf '\n' >> "$TEST_LOG"
`))
	write("bin/mcp", []byte("#!/usr/bin/env bash\nexec sleep 30\n"))
	env := make([]string, 0)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key == "HOME" || key == "PATH" || key == "XDG_CONFIG_HOME" || strings.HasPrefix(key, "TLDW_") || strings.HasPrefix(key, "TUNNEL_CLIENT_") || strings.HasPrefix(key, "TEST_") {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "HOME=home", "PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "TLDW_TUNNEL_ID=tunnel_test", "TEST_LOG="+filepath.Join(dir, "commands.log"))
	return &tunnelFixture{dir: dir, env: env}
}

func (f *tunnelFixture) run(t *testing.T, wantFailure bool, args ...string) string {
	t.Helper()
	args = append([]string{"--justfile", filepath.Join(f.dir, "Justfile")}, args...)
	cmd := exec.Command("just", args...)
	cmd.Env = f.env
	output, err := cmd.CombinedOutput()
	if (err != nil) != wantFailure {
		t.Fatalf("just %v error = %v, want failure %t:\n%s", args, err, wantFailure, output)
	}
	return string(output)
}

func (f *tunnelFixture) log(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(f.dir, "commands.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
