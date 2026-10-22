// Package secret provides secret retrieval and output redaction.
package secret

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Provider returns the value of the named secret. The value lives in memory
// only; callers pass it to a child through its environment.
type Provider interface {
	Get(name string) (string, error)
}

// Env reads secrets from the environment variable of the same name.
type Env struct {
	Lookup func(string) (string, bool)
}

// NewEnv returns an Env provider backed by the process environment.
func NewEnv() Env { return Env{Lookup: os.LookupEnv} }

func (e Env) Get(name string) (string, error) {
	if v, ok := e.Lookup(name); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("environment variable %s is not set", name)
}

// Command runs a configured command and reads the secret from its standard
// output. Every {name} in Argv is replaced with the secret name. The command
// runs without a shell. A tool that injects secrets into a child's
// environment fits by ending the command with `printenv {name}`.
type Command struct {
	Argv   []string
	Stderr io.Writer
}

// ParseCommand splits a configured command on whitespace.
func ParseCommand(s string) ([]string, error) {
	argv := strings.Fields(s)
	if len(argv) == 0 {
		return nil, errors.New("secret command is empty")
	}
	return argv, nil
}

// Available reports an error when the command's program is not on PATH.
func (c Command) Available() error {
	if _, err := exec.LookPath(c.Argv[0]); err != nil {
		return fmt.Errorf("%s not found on PATH", c.Argv[0])
	}
	return nil
}

func (c Command) Get(name string) (string, error) {
	argv := make([]string, len(c.Argv))
	for i, a := range c.Argv {
		argv[i] = strings.ReplaceAll(a, "{name}", name)
	}
	var out bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = c.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("secret command failed for %s: %v", name, err)
	}
	v := strings.TrimRight(out.String(), "\r\n")
	if v == "" {
		return "", fmt.Errorf("secret command did not return %s", name)
	}
	return v, nil
}
