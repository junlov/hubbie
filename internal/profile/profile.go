// Package profile models one client profile: a folder that holds the
// expected portal, the secret name, and a private HOME for the HubSpot CLI.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var (
	clientRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	digitsRe = regexp.MustCompile(`^[0-9]+$`)
	nameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ValidClient reports whether name is a legal client name.
func ValidClient(name string) bool { return clientRe.MatchString(name) }

// ValidPortal reports whether s is a numeric portal id.
func ValidPortal(s string) bool { return digitsRe.MatchString(s) }

// ValidSecretName reports whether s is usable as an environment variable name.
func ValidSecretName(s string) bool { return nameRe.MatchString(s) }

// ErrAmbiguous means the per-portal login folder holds more than one portal.
// The error text is the diagnostic for the user.
type ErrAmbiguous struct {
	Portals []string
	Dir     string
}

func (e *ErrAmbiguous) Error() string {
	return fmt.Sprintf("login folder holds more than one portal (%s); a profile is one client, remove the extra accounts under %s/accounts",
		strings.Join(e.Portals, " "), e.Dir)
}

// ErrNoLogin means no readable login exists in either layout.
var ErrNoLogin = errors.New("no login")

// Profile is the folder for one client.
type Profile struct {
	Client string
	Dir    string
}

// ConfigDir is where the HubSpot CLI keeps its files when HOME is home.
func ConfigDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "hubspot")
	}
	return filepath.Join(home, ".config", "hubspot")
}

// ConfigDir is the HubSpot CLI config folder under this profile.
func (p Profile) ConfigDir() string { return ConfigDir(p.Dir) }

// LegacyAuthFile is the single auth.json written by HubSpot CLI before 0.16.0.
func (p Profile) LegacyAuthFile() string { return filepath.Join(p.ConfigDir(), "auth.json") }

// PortalFile holds the expected portal.
func (p Profile) PortalFile() string { return filepath.Join(p.Dir, "portal-id") }

// SecretNameFile holds the name of the secret used with --service-key.
func (p Profile) SecretNameFile() string { return filepath.Join(p.Dir, "service-key-name") }

// Expected returns the expected portal, or "" when none is recorded.
func (p Profile) Expected() (string, error) {
	b, err := os.ReadFile(p.PortalFile())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	s := trimNewlines(string(b))
	if !ValidPortal(s) {
		return "", fmt.Errorf("invalid expected portal in %s", p.PortalFile())
	}
	return s, nil
}

// SecretName returns the recorded secret name and whether the file exists.
func (p Profile) SecretName() (string, bool, error) {
	b, err := os.ReadFile(p.SecretNameFile())
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return trimNewlines(string(b)), true, nil
}

func trimNewlines(s string) string { return strings.TrimRight(s, "\n") }

// authPortal reads hub_id from an auth file. ok is false when the file is
// missing, unreadable, or hub_id is not a plain number or digit string.
func authPortal(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var doc struct {
		HubID json.RawMessage `json:"hub_id"`
	}
	if json.NewDecoder(f).Decode(&doc) != nil || len(doc.HubID) == 0 {
		return "", false
	}
	s := string(doc.HubID)
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(doc.HubID, &str) != nil {
			return "", false
		}
		s = str
	}
	if !ValidPortal(s) {
		return "", false
	}
	return s, true
}

// accountPortals lists the distinct portals found under accounts/*/auth.json,
// sorted numerically.
func (p Profile) accountPortals() []string {
	accounts := filepath.Join(p.ConfigDir(), "accounts")
	entries, _ := os.ReadDir(accounts)
	seen := map[string]bool{}
	var out []string
	for _, entry := range entries {
		if portal, ok := authPortal(filepath.Join(accounts, entry.Name(), "auth.json")); ok && !seen[portal] {
			seen[portal] = true
			out = append(out, portal)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// HasAccountLogin reports whether the per-portal layout holds a readable login.
func (p Profile) HasAccountLogin() bool { return len(p.accountPortals()) > 0 }

// Login is the resolved cached login of a profile.
type Login struct {
	Portal string
	// Note is set when both layouts disagree and the per-portal file won.
	Note string
}

// CachedLogin resolves the profile's login. The per-portal layout wins over
// the old single file when they disagree. It returns *ErrAmbiguous when the
// per-portal folder holds more than one portal and ErrNoLogin when neither
// layout holds a readable login.
func (p Profile) CachedLogin() (Login, error) {
	old, hasOld := authPortal(p.LegacyAuthFile())
	portals := p.accountPortals()
	switch {
	case len(portals) > 1:
		return Login{}, &ErrAmbiguous{Portals: portals, Dir: p.ConfigDir()}
	case len(portals) == 1:
		l := Login{Portal: portals[0]}
		if hasOld && old != portals[0] {
			l.Note = fmt.Sprintf("old auth.json says portal %s but accounts/ says %s; using %s", old, portals[0], portals[0])
		}
		return l, nil
	case hasOld:
		return Login{Portal: old}, nil
	}
	return Login{}, ErrNoLogin
}

// RetireLegacyAuth moves the old auth.json aside when the per-portal layout
// already holds a login. The CLI imports the old file even then, which would
// add a second account.
func (p Profile) RetireLegacyAuth() error {
	if _, err := os.Stat(p.LegacyAuthFile()); err != nil || !p.HasAccountLogin() {
		return nil
	}
	tmp, err := os.CreateTemp(p.ConfigDir(), "auth.json.legacy.*")
	if err != nil {
		return err
	}
	tmp.Close()
	return os.Rename(p.LegacyAuthFile(), tmp.Name())
}
