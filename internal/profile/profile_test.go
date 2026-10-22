package profile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newProfile(t *testing.T) Profile {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "profiles[dev]*?", "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return Profile{Client: "acme", Dir: dir}
}

func writeAuth(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p Profile) account(t *testing.T, portal, body string) {
	writeAuth(t, filepath.Join(p.ConfigDir(), "accounts", portal, "auth.json"), body)
}

func TestCachedLogin(t *testing.T) {
	tests := []struct {
		name       string
		legacy     string
		accounts   map[string]string
		wantPortal string
		wantNote   bool
		wantErr    error
		wantAmbig  []string
	}{
		{name: "no login", wantErr: ErrNoLogin},
		{name: "legacy only", legacy: `{"hub_id":321}`, wantPortal: "321"},
		{name: "legacy string id", legacy: `{"hub_id":"321"}`, wantPortal: "321"},
		{name: "legacy without hub_id", legacy: `{"refresh_token":"x"}`, wantErr: ErrNoLogin},
		{name: "legacy non numeric", legacy: `{"hub_id":"abc"}`, wantErr: ErrNoLogin},
		{name: "legacy negative", legacy: `{"hub_id":-5}`, wantErr: ErrNoLogin},
		{name: "legacy not json", legacy: `nope`, wantErr: ErrNoLogin},
		{name: "per portal only", accounts: map[string]string{"900": `{"hub_id":900}`}, wantPortal: "900"},
		{
			name:       "per portal wins on disagreement",
			legacy:     `{"hub_id":222}`,
			accounts:   map[string]string{"900": `{"hub_id":900}`},
			wantPortal: "900", wantNote: true,
		},
		{
			name:       "both layouts agree",
			legacy:     `{"hub_id":900}`,
			accounts:   map[string]string{"900": `{"hub_id":900}`},
			wantPortal: "900",
		},
		{
			name:      "two portals are ambiguous",
			accounts:  map[string]string{"900": `{"hub_id":900}`, "7": `{"hub_id":7}`},
			wantAmbig: []string{"7", "900"},
		},
		{
			name:       "unreadable account is ignored",
			legacy:     `{"hub_id":5}`,
			accounts:   map[string]string{"900": `{"hub_id":900}`, "8": `{}`},
			wantPortal: "900", wantNote: true,
		},
		{
			name:       "same portal in two folders is not ambiguous",
			accounts:   map[string]string{"a": `{"hub_id":900}`, "b": `{"hub_id":900}`},
			wantPortal: "900",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newProfile(t)
			if tc.legacy != "" {
				writeAuth(t, p.LegacyAuthFile(), tc.legacy)
			}
			for portal, body := range tc.accounts {
				p.account(t, portal, body)
			}
			l, err := p.CachedLogin()
			if tc.wantAmbig != nil {
				var amb *ErrAmbiguous
				if !errors.As(err, &amb) {
					t.Fatalf("want ambiguity, got %v", err)
				}
				if strings.Join(amb.Portals, " ") != strings.Join(tc.wantAmbig, " ") {
					t.Fatalf("portals %v, want %v", amb.Portals, tc.wantAmbig)
				}
				return
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if l.Portal != tc.wantPortal {
				t.Fatalf("portal %q, want %q", l.Portal, tc.wantPortal)
			}
			if (l.Note != "") != tc.wantNote {
				t.Fatalf("note %q, wantNote %v", l.Note, tc.wantNote)
			}
		})
	}
}

func TestAmbiguityMessage(t *testing.T) {
	p := newProfile(t)
	p.account(t, "900", `{"hub_id":900}`)
	p.account(t, "7", `{"hub_id":7}`)
	_, err := p.CachedLogin()
	if err == nil || !strings.Contains(err.Error(), "more than one portal (7 900)") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestRetireLegacyAuth(t *testing.T) {
	p := newProfile(t)
	writeAuth(t, p.LegacyAuthFile(), `{"hub_id":222}`)
	if err := p.RetireLegacyAuth(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.LegacyAuthFile()); err != nil {
		t.Fatal("legacy file moved without a per-portal login")
	}
	p.account(t, "900", `{"hub_id":900}`)
	if err := p.RetireLegacyAuth(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.LegacyAuthFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy file still in place")
	}
	entries, err := os.ReadDir(p.ConfigDir())
	if err != nil {
		t.Fatal(err)
	}
	var backups []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "auth.json.legacy.") {
			backups = append(backups, filepath.Join(p.ConfigDir(), entry.Name()))
		}
	}
	if len(backups) != 1 {
		t.Fatalf("want one backup, got %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != `{"hub_id":222}` {
		t.Fatalf("backup changed: %s", b)
	}
}

func TestExpected(t *testing.T) {
	p := newProfile(t)
	if got, err := p.Expected(); got != "" || err != nil {
		t.Fatalf("missing file: %q %v", got, err)
	}
	if err := os.WriteFile(p.PortalFile(), []byte("123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := p.Expected(); got != "123" || err != nil {
		t.Fatalf("got %q %v", got, err)
	}
	if err := os.WriteFile(p.PortalFile(), []byte("12x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Expected(); err == nil {
		t.Fatal("invalid portal accepted")
	}
}

func TestValidClient(t *testing.T) {
	for _, ok := range []string{"a", "acme", "a-b", "a1-b2-c3"} {
		if !ValidClient(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "a--b", "A", "a/b", "../a", "a_b", "list "} {
		if ValidClient(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
