package secret

import (
	"bytes"
	"strings"
	"testing"
)

func TestEnv(t *testing.T) {
	e := Env{Lookup: func(k string) (string, bool) { return "v", k == "KEY" }}
	if v, err := e.Get("KEY"); v != "v" || err != nil {
		t.Fatalf("got %q %v", v, err)
	}
	if _, err := e.Get("OTHER"); err == nil {
		t.Fatal("missing variable accepted")
	}
}

func TestCommand(t *testing.T) {
	c := Command{Argv: []string{"printf", "%s\n", "{name}-value"}}
	if v, err := c.Get("K"); v != "K-value" || err != nil {
		t.Fatalf("got %q %v", v, err)
	}
	if _, err := (Command{Argv: []string{"false"}}).Get("K"); err == nil {
		t.Fatal("failing command accepted")
	}
	if _, err := (Command{Argv: []string{"true"}}).Get("K"); err == nil {
		t.Fatal("empty output accepted")
	}
	if err := (Command{Argv: []string{"no-such-tool-xyz"}}).Available(); err == nil {
		t.Fatal("missing tool reported available")
	}
}

func TestParseCommand(t *testing.T) {
	if _, err := ParseCommand("  "); err == nil {
		t.Fatal("empty command accepted")
	}
	argv, _ := ParseCommand("a  b {name}")
	if strings.Join(argv, ",") != "a,b,{name}" {
		t.Fatalf("got %v", argv)
	}
}

func TestRedactor(t *testing.T) {
	var out bytes.Buffer
	r := NewRedactor(&out, "s3cret")
	for _, chunk := range []string{"a s3cret b ", "s3", "cret ", "tail s3c"} {
		if _, err := r.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	r.Flush()
	got := out.String()
	if strings.Contains(got, "s3cret") {
		t.Fatalf("secret leaked: %q", got)
	}
	if got != "a [redacted] b [redacted] tail s3c" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactorEmptySecret(t *testing.T) {
	var out bytes.Buffer
	r := NewRedactor(&out, "")
	r.Write([]byte("plain"))
	if out.String() != "plain" {
		t.Fatalf("got %q", out.String())
	}
}
