package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

func execve(path string, argv, env []string) error { return syscall.Exec(path, argv, env) }

// withEnv returns env with each KEY=VALUE pair replacing any earlier KEY.
func withEnv(env []string, pairs ...string) []string {
	drop := map[string]bool{}
	for _, p := range pairs {
		drop[strings.SplitN(p, "=", 2)[0]] = true
	}
	out := make([]string, 0, len(env)+len(pairs))
	for _, kv := range env {
		if !drop[strings.SplitN(kv, "=", 2)[0]] {
			out = append(out, kv)
		}
	}
	return append(out, pairs...)
}

// without returns env minus the named variable.
func without(env []string, name string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// resolveBinary finds the HubSpot CLI and refuses to run hubbie itself.
func (a *App) resolveBinary() (string, error) {
	bin := a.getenv(envBinary)
	if bin == "" {
		p, err := exec.LookPath("hubspot")
		if err != nil {
			return "", die("hubspot executable not found on PATH")
		}
		bin = p
	}
	st, err := os.Stat(bin)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
		return "", die("hubspot executable is not runnable: %s", bin)
	}
	if self, err := os.Executable(); err == nil {
		if si, err := os.Stat(self); err == nil && os.SameFile(st, si) {
			return "", die("hubspot executable resolves to hubbie itself")
		}
	}
	return bin, nil
}

// runChild runs the child to completion with the terminal attached and
// passes signals through. A non-zero exit becomes hubbie's exit code.
func runChild(bin string, args, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	sigs := make(chan os.Signal, 8)
	signal.Notify(sigs)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return die("cannot run %s: %v", bin, err)
	}
	go func() {
		for s := range sigs {
			if s != syscall.SIGCHLD && s != syscall.SIGURG {
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	err := cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			code = 128 + int(ws.Signal())
		}
		return &exitError{code: code}
	}
	if err != nil {
		return die("cannot run %s: %v", bin, err)
	}
	return nil
}

var (
	whoamiPortalRe = regexp.MustCompile(`(?m)^[ \t]*Portal:[ \t]*([0-9]+)`)
	digitsOnly     = regexp.MustCompile(`^[0-9]+$`)
)

// whoamiPortal reads the portal from `hubspot whoami` output, either the
// text form or JSON.
func whoamiPortal(out []byte) string {
	if m := whoamiPortalRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	var doc map[string]any
	if json.Unmarshal(out, &doc) != nil {
		return ""
	}
	data, _ := doc["data"].(map[string]any)
	for _, v := range []any{doc["portal_id"], doc["portal"], doc["hub_id"], data["portal_id"], data["portal"]} {
		var s string
		switch t := v.(type) {
		case nil:
			continue
		case float64:
			s = strconv.FormatFloat(t, 'f', -1, 64)
		case string:
			s = t
		default:
			continue
		}
		if digitsOnly.MatchString(s) {
			return s
		}
		return ""
	}
	return ""
}
