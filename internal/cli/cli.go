// Package cli wires the hubbie commands.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	envProfiles      = "RP_HUBSPOT_PROFILES"
	envBinary        = "RP_HUBSPOT_BIN"
	envSecretCommand = "HUBBIE_SECRET_COMMAND"

	usageText = "Usage: hubbie <client> <hubspot args...>\n" +
		"       hubbie <client> --service-key <hubspot args...>\n" +
		"       hubbie <client> service-key set <SECRET_NAME>\n" +
		"       hubbie list\n"
)

// exitError carries the process exit code and the message to print.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func die(format string, a ...any) error {
	return &exitError{code: 1, msg: "hubbie: " + fmt.Sprintf(format, a...) + "\n"}
}

func usage() error { return &exitError{code: 2, msg: usageText} }

// App holds the process wiring so tests and main share one code path.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	// Exec replaces the process with the child; it only returns on failure.
	Exec func(path string, argv []string, env []string) error
	// Env is the process environment as KEY=VALUE pairs.
	Env []string

	errw io.Writer
}

// New returns an App wired to the real process.
func New() *App {
	return &App{Stdout: os.Stdout, Stderr: os.Stderr, Exec: execve, Env: os.Environ()}
}

func (a *App) getenv(key string) string {
	for i := len(a.Env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(a.Env[i], key+"="); ok {
			return v
		}
	}
	return ""
}

func (a *App) profilesRoot() string {
	if v := a.getenv(envProfiles); v != "" {
		return v
	}
	return filepath.Join(a.getenv("HOME"), ".config", "hubbie", "profiles")
}

// Run executes the command line and returns the exit code.
func (a *App) Run(args []string) int {
	a.errw = a.Stderr
	var err error
	if len(args) > 0 && args[0] == "list" {
		err = a.list(args[1:])
	} else {
		err = a.client(args)
	}
	code := 0
	if err != nil {
		code = 1
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code
			fmt.Fprint(a.errw, ee.msg)
		} else {
			fmt.Fprintf(a.errw, "hubbie: %v\n", err)
		}
	}
	if r, ok := a.errw.(interface{ Flush() error }); ok {
		_ = r.Flush()
	}
	return code
}
