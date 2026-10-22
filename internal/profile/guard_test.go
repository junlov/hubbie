package profile

import (
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	if err := Guard("acme", "100", "100"); err != nil {
		t.Fatalf("match refused: %v", err)
	}
	err := Guard("acme", "100", "222")
	if err == nil {
		t.Fatal("mismatch accepted")
	}
	for _, want := range []string{"expects portal 100 but login is for portal 222", "hubbie acme auth login --portal 100"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
}

func TestServiceGuard(t *testing.T) {
	if err := ServiceGuard("acme", "100", "100"); err != nil {
		t.Fatalf("match refused: %v", err)
	}
	err := ServiceGuard("acme", "100", "555")
	if err == nil || !strings.Contains(err.Error(), "is for portal 555, expected 100") {
		t.Fatalf("unexpected: %v", err)
	}
}
