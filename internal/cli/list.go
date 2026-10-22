package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junlov/hubbie/internal/profile"
)

func (a *App) list(args []string) error {
	if len(args) != 0 {
		return usage()
	}
	fmt.Fprint(a.Stdout, "CLIENT\tPORTAL\tLOGIN\tSERVICE_KEY\n")
	root := a.profilesRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if st, err := os.Stat(dir); err != nil || !st.IsDir() || !profile.ValidClient(e.Name()) {
			continue
		}
		p := profile.Profile{Client: e.Name(), Dir: dir}
		expected, skey, login := "-", "-", "no"
		if b, err := os.ReadFile(p.PortalFile()); err == nil {
			expected = strings.TrimRight(string(b), "\n")
		}
		if _, err := p.CachedLogin(); err == nil {
			login = "yes"
		}
		if b, err := os.ReadFile(p.SecretNameFile()); err == nil {
			skey = strings.TrimRight(string(b), "\n")
		}
		fmt.Fprintf(a.Stdout, "%s\t%s\t%s\t%s\n", p.Client, expected, login, skey)
	}
	return nil
}
