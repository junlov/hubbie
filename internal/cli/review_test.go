package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/junlov/hubbie/internal/profile"
)

func TestReviewCLIProcess(t *testing.T) {
	if os.Getenv("HUBBIE_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(New().Run(os.Args[i+1:]))
		}
	}
	os.Exit(99)
}

type reviewCLI struct {
	root    string
	capture string
	env     []string
}

func newReviewCLI(t *testing.T) reviewCLI {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "hubspot")
	script := `#!/bin/sh
printf 'HOME=%s\nTOKEN=%s\nCOMMAND=%s\n' "$HOME" "${HUBSPOT_ACCESS_TOKEN-}" "$*" >> "$HUBBIE_TEST_CAPTURE"
if [ "$1" = whoami ]; then
  printf '{"portalId":%s}\n' "${HUBBIE_TEST_PORTAL:-123}"
elif [ "$1" = auth ] && [ "$2" = login ]; then
  if [ "${3-}" = url ]; then
    printf 'https://example.com/authorize\n'
    exit "${HUBBIE_TEST_URL_EXIT:-0}"
  fi
  case "$(uname -s)" in
    Darwin) cfg="$HOME/Library/Application Support/hubspot" ;;
    *) cfg="$HOME/.config/hubspot" ;;
  esac
  mkdir -p "$cfg/accounts/123"
  printf '{"hub_id":123}\n' > "$cfg/accounts/123/auth.json"
  if [ -n "${HUBBIE_TEST_RELEASE-}" ]; then
    : > "$HUBBIE_TEST_READY"
    while [ ! -e "$HUBBIE_TEST_RELEASE" ]; do sleep 0.02; done
  fi
fi
`
	writeReviewFile(t, bin, script, 0o700)
	capture := filepath.Join(dir, "calls")
	root := filepath.Join(dir, "profiles[dev]*?")
	return reviewCLI{
		root: root, capture: capture,
		env: withEnv(os.Environ(), "HUBBIE_TEST_PROCESS=1", "HUBSPOT_ACCESS_TOKEN=", "HUBBIE_SECRET_COMMAND=",
			envProfiles+"="+root, envBinary+"="+bin, "HUBBIE_TEST_CAPTURE="+capture),
	}
}

func writeReviewFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func (f reviewCLI) seed(t *testing.T, client string) profile.Profile {
	t.Helper()
	p := profile.Profile{Client: client, Dir: filepath.Join(f.root, client)}
	writeReviewFile(t, p.PortalFile(), "123\n", 0o600)
	writeReviewFile(t, filepath.Join(p.ConfigDir(), "accounts", "123", "auth.json"), `{"hub_id":123}`, 0o600)
	return p
}

func (f reviewCLI) command(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestReviewCLIProcess$", "--"}, args...)...)
	cmd.Env = f.env
	return cmd
}

func requireNoReviewChild(t *testing.T, f reviewCLI) {
	t.Helper()
	if _, err := os.Stat(f.capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child ran: %v", err)
	}
}

func TestReviewCredentialIsolation(t *testing.T) {
	for _, source := range []string{"environment", ".hubspot/.env", ".config/hubspot/.env"} {
		for _, args := range [][]string{
			{"objects", "list"}, {"auth", "login", "--portal", "123"}, {"auth", "logout"}, {"whoami"},
		} {
			t.Run(source+"/"+strings.Join(args, "-"), func(t *testing.T) {
				f := newReviewCLI(t)
				p := f.seed(t, "acme")
				writeReviewFile(t, p.LegacyAuthFile(), `{"hub_id":456}`, 0o600)
				if source == "environment" {
					f.env = withEnv(f.env, "HUBSPOT_ACCESS_TOKEN=ambient-secret")
				} else {
					f.env = without(f.env, "HUBSPOT_ACCESS_TOKEN")
					writeReviewFile(t, filepath.Join(p.Dir, source), "HUBSPOT_ACCESS_TOKEN=ambient-secret\n", 0o600)
				}
				out, err := f.command(t, append([]string{"acme"}, args...)...).CombinedOutput()
				if err == nil || !strings.Contains(string(out), "HUBSPOT_ACCESS_TOKEN") || strings.Contains(string(out), "ambient-secret") {
					t.Fatalf("unexpected refusal: %v %s", err, out)
				}
				requireNoReviewChild(t, f)
				if data, err := os.ReadFile(p.LegacyAuthFile()); err != nil || string(data) != `{"hub_id":456}` {
					t.Fatalf("refused command changed login files: %v %s", err, data)
				}
			})
		}
	}
}

func TestReviewProfileEnvFiles(t *testing.T) {
	for _, source := range []string{".hubspot/.env", ".config/hubspot/.env"} {
		for _, tc := range []struct {
			name    string
			content string
			blocked bool
		}{
			{"unquoted", "HUBSPOT_ACCESS_TOKEN=profile-secret\n", true},
			{"export", "export HUBSPOT_ACCESS_TOKEN = 'profile-secret'\n", true},
			{"double quotes and CRLF", " \tHUBSPOT_ACCESS_TOKEN = \"profile-secret\" # token\r\n", true},
			{"multiline", "HUBSPOT_ACCESS_TOKEN=\"profile-secret\nsecond-line\"\n", true},
			{"indirect", "TOKEN=profile-secret\nHUBSPOT_ACCESS_TOKEN=${TOKEN}\n", true},
			{"empty declaration", "HUBSPOT_ACCESS_TOKEN=\n", true},
			{"comment", "# HUBSPOT_ACCESS_TOKEN=profile-secret\nOTHER=value\n", false},
			{"unrelated value", "NOTE=\"HUBSPOT_ACCESS_TOKEN=profile-secret\"\n", false},
			{"empty file", "", false},
			{"malformed", "OTHER='profile-secret\n", true},
			{"directory", "", true},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				f := newReviewCLI(t)
				p := f.seed(t, "acme")
				f.env = without(f.env, "HUBSPOT_ACCESS_TOKEN")
				path := filepath.Join(p.Dir, source)
				if tc.name == "directory" {
					if err := os.MkdirAll(path, 0o700); err != nil {
						t.Fatal(err)
					}
				} else {
					writeReviewFile(t, path, tc.content, 0o600)
				}
				out, err := f.command(t, "acme", "objects", "list").CombinedOutput()
				if strings.Contains(string(out), "profile-secret") {
					t.Fatalf("file content leaked: %s", out)
				}
				if tc.blocked {
					if err == nil || !strings.Contains(string(out), path) {
						t.Fatalf("unsafe input did not block command: %v %s", err, out)
					}
					requireNoReviewChild(t, f)
				} else if err != nil {
					t.Fatalf("safe input blocked command: %v %s", err, out)
				} else if _, err := os.Stat(f.capture); err != nil {
					t.Fatalf("safe input did not reach child: %v", err)
				}
				if tc.name != "directory" {
					if data, err := os.ReadFile(path); err != nil || string(data) != tc.content {
						t.Fatalf("profile environment file changed: %v %s", err, data)
					}
				}
			})
		}
	}
}

func TestReviewProfileEnvMetadata(t *testing.T) {
	f := newReviewCLI(t)
	p := f.seed(t, "acme")
	writeReviewFile(t, filepath.Join(p.Dir, ".hubspot", ".env"), "HUBSPOT_ACCESS_TOKEN=profile-secret\n", 0o600)
	if out, err := f.command(t, "acme", "service-key", "set", "ACME_KEY").CombinedOutput(); err != nil {
		t.Fatalf("secret name could not be stored: %v %s", err, out)
	}
	if name, ok, err := p.SecretName(); err != nil || !ok || name != "ACME_KEY" {
		t.Fatalf("wrong secret name: %q %v %v", name, ok, err)
	}
	out, err := f.command(t, "list").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "acme\t123\tyes\tACME_KEY") || strings.Contains(string(out), "profile-secret") {
		t.Fatalf("list failed: %v %s", err, out)
	}
	requireNoReviewChild(t, f)
}

func TestReviewClientDispatch(t *testing.T) {
	for _, client := range []string{"acme", "no-help", "help"} {
		t.Run(client, func(t *testing.T) {
			f := newReviewCLI(t)
			f.seed(t, client)
			if out, err := f.command(t, client, "objects", "list").CombinedOutput(); err != nil {
				t.Fatalf("command failed: %v %s", err, out)
			}
			calls, err := os.ReadFile(f.capture)
			if err != nil || !strings.Contains(string(calls), "COMMAND=objects list\n") {
				t.Fatalf("client did not reach child: %v %s", err, calls)
			}
		})
	}
	f := newReviewCLI(t)
	f.seed(t, "no-help")
	out, err := f.command(t, "list").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "no-help\t123\tyes\t-") {
		t.Fatalf("list failed: %v %s", err, out)
	}
	requireNoReviewChild(t, f)
	for _, args := range [][]string{nil, {"list", "extra"}} {
		out, err := f.command(t, args...).CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "Usage:") {
			t.Fatalf("usage changed: %v %s", err, out)
		}
	}
}

func TestReviewLoginURL(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, code := range []string{"0", "37"} {
			name := "fresh/" + code
			if existing {
				name = "recorded/" + code
			}
			t.Run(name, func(t *testing.T) {
				f := newReviewCLI(t)
				p := profile.Profile{Client: "acme", Dir: filepath.Join(f.root, "acme")}
				if existing {
					writeReviewFile(t, p.PortalFile(), "123\n", 0o600)
				}
				f.env = withEnv(f.env, "HUBBIE_TEST_URL_EXIT="+code)
				out, err := f.command(t, "acme", "auth", "login", "url", "--portal", "123").CombinedOutput()
				if code == "0" {
					if err != nil {
						t.Fatalf("URL generation failed: %v %s", err, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 37 {
						t.Fatalf("child exit code changed: %v %s", err, out)
					}
				}
				if string(out) != "https://example.com/authorize\n" {
					t.Fatalf("URL output changed: %s", out)
				}
				calls, err := os.ReadFile(f.capture)
				want := "HOME=" + p.Dir + "\nTOKEN=\nCOMMAND=auth login url\n"
				if err != nil || string(calls) != want {
					t.Fatalf("wrong child environment or arguments: %v %s", err, calls)
				}
				portal, err := p.Expected()
				wantPortal := ""
				if existing {
					wantPortal = "123"
				}
				if err != nil || portal != wantPortal {
					t.Fatalf("URL generation changed expected portal: %q %v", portal, err)
				}
				if _, err := p.CachedLogin(); !errors.Is(err, profile.ErrNoLogin) {
					t.Fatalf("URL generation created a login: %v", err)
				}
			})
		}
	}
}

func TestReviewLoginLiteralRoot(t *testing.T) {
	f := newReviewCLI(t)
	if out, err := f.command(t, "acme", "auth", "login", "--portal", "123").CombinedOutput(); err != nil {
		t.Fatalf("login failed: %v %s", err, out)
	}
	p := profile.Profile{Client: "acme", Dir: filepath.Join(f.root, "acme")}
	if portal, err := p.Expected(); err != nil || portal != "123" {
		t.Fatalf("login did not record portal: %q %v", portal, err)
	}
	writeReviewFile(t, p.LegacyAuthFile(), `{"hub_id":456}`, 0o600)
	if out, err := f.command(t, "acme", "objects", "list").CombinedOutput(); err != nil {
		t.Fatalf("guarded command failed: %v %s", err, out)
	}
	if _, err := os.Stat(p.LegacyAuthFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy import remains reachable: %v", err)
	}
}

func TestReviewServiceProvider(t *testing.T) {
	for _, mode := range []string{"unset", "command", "environment-command", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			f := newReviewCLI(t)
			p := f.seed(t, "acme")
			writeReviewFile(t, p.SecretNameFile(), "ACME_KEY\n", 0o600)
			for _, source := range []string{".hubspot/.env", ".config/hubspot/.env"} {
				writeReviewFile(t, filepath.Join(p.Dir, source), "HUBSPOT_ACCESS_TOKEN=profile-secret\n", 0o600)
			}
			f.env = withEnv(f.env, "ACME_KEY=fetched-secret", "HUBSPOT_ACCESS_TOKEN=ambient-secret")
			providerCapture := filepath.Join(filepath.Dir(f.capture), "provider-name")
			if mode == "environment-command" {
				f.env = withEnv(f.env, envSecretCommand+"=printenv {name}")
			} else if mode != "unset" {
				provider := filepath.Join(filepath.Dir(f.capture), "fake-injector")
				writeReviewFile(t, provider, "#!/bin/sh\nprintf '%s' \"$1\" > \"$HUBBIE_TEST_PROVIDER_CAPTURE\"\nprintf '%s\\n' fetched-secret\n", 0o700)
				f.env = withEnv(f.env, envSecretCommand+"="+provider+" {name}", "HUBBIE_TEST_PROVIDER_CAPTURE="+providerCapture)
			}
			if mode == "mismatch" {
				f.env = withEnv(f.env, "HUBBIE_TEST_PORTAL=456")
			}
			out, err := f.command(t, "acme", "--service-key", "objects", "list").CombinedOutput()
			if strings.Contains(string(out), "fetched-secret") || strings.Contains(string(out), "ambient-secret") {
				t.Fatalf("secret leaked: %s", out)
			}
			if mode == "unset" {
				if err == nil || !strings.Contains(string(out), "HUBBIE_SECRET_COMMAND") || !strings.Contains(string(out), "ACME_KEY") || strings.Count(string(out), "\n") != 1 {
					t.Fatalf("unexpected provider refusal: %v %s", err, out)
				}
				requireNoReviewChild(t, f)
				return
			}
			calls, readErr := os.ReadFile(f.capture)
			if readErr != nil || strings.Contains(string(calls), "ambient-secret") || !strings.Contains(string(calls), "TOKEN=fetched-secret\nCOMMAND=whoami\n") {
				t.Fatalf("identity used wrong credentials: %v %s", readErr, calls)
			}
			if mode == "mismatch" {
				if err == nil || !strings.Contains(string(out), "is for portal 456, expected 123") || strings.Contains(string(calls), "COMMAND=objects list") {
					t.Fatalf("mismatch did not block command: %v %s %s", err, out, calls)
				}
				return
			}
			if err != nil || !strings.Contains(string(calls), "TOKEN=fetched-secret\nCOMMAND=objects list\n") {
				t.Fatalf("service command failed: %v %s %s", err, out, calls)
			}
			if mode == "command" {
				name, err := os.ReadFile(providerCapture)
				if err != nil || string(name) != "ACME_KEY" {
					t.Fatalf("provider did not receive secret name: %v %s", err, name)
				}
			}
		})
	}
}

func TestReviewLoginSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU, syscall.SIGTERM, syscall.SIGUSR1} {
		t.Run(sig.String(), func(t *testing.T) {
			f := newReviewCLI(t)
			ready := filepath.Join(filepath.Dir(f.capture), "ready")
			release := filepath.Join(filepath.Dir(f.capture), "release")
			f.env = withEnv(f.env, "HUBBIE_TEST_READY="+ready, "HUBBIE_TEST_RELEASE="+release)
			cmd := f.command(t, "acme", "auth", "login", "--portal", "123")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				_ = cmd.Wait()
			})
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("login child did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if sig == syscall.SIGTERM || sig == syscall.SIGUSR1 {
				var exit *exec.ExitError
				if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 128+int(sig) {
					t.Fatalf("termination status changed: %v", err)
				}
				return
			}
			deadline = time.Now().Add(3 * time.Second)
			for {
				var status syscall.WaitStatus
				pid, err := syscall.Wait4(cmd.Process.Pid, &status, syscall.WNOHANG|syscall.WUNTRACED, nil)
				if err != nil {
					t.Fatal(err)
				}
				if pid != 0 {
					if !status.Stopped() || status.StopSignal() != sig {
						t.Fatalf("login did not stop normally: %v", status)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("parent intercepted the terminal stop signal")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
				t.Fatal(err)
			}
			writeReviewFile(t, release, "", 0o600)
			if err := cmd.Wait(); err != nil {
				t.Fatalf("resumed login failed: %v", err)
			}
			p := profile.Profile{Client: "acme", Dir: filepath.Join(f.root, "acme")}
			if portal, err := p.Expected(); err != nil || portal != "123" {
				t.Fatalf("resumed login did not record portal: %q %v", portal, err)
			}
		})
	}
}
