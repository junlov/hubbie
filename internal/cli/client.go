package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/junlov/hubbie/internal/profile"
	"github.com/junlov/hubbie/internal/secret"
)

// client handles `hubbie <client> <args...>`.
func (a *App) client(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	client := args[0]
	if !profile.ValidClient(client) {
		return die("invalid client '%s': use lowercase letters, digits, and single hyphens", client)
	}
	args = args[1:]
	if len(args) == 0 {
		return usage()
	}
	p := profile.Profile{Client: client, Dir: a.profilesRoot() + string(os.PathSeparator) + client}

	if args[0] == "service-key" {
		return a.setSecretName(p, args)
	}
	service := false
	if args[0] == "--service-key" {
		service = true
		args = args[1:]
		if len(args) == 0 {
			return usage()
		}
		if args[0] == "auth" {
			return die("--service-key cannot be combined with auth")
		}
	}

	login := len(args) >= 2 && args[0] == "auth" && args[1] == "login"
	repair := args[0] == "auth" || args[0] == "whoami"

	expected, err := p.Expected()
	if err != nil {
		return die("%v", err)
	}
	if service {
		return a.serviceCommand(p, expected, args)
	}
	if err := a.profileCredentials(p); err != nil {
		return err
	}

	portalArg := ""
	if login {
		var rest []string
		portalArg, rest, err = parsePortalFlag(args)
		if err != nil {
			return err
		}
		if expected == "" && portalArg == "" {
			return die("first login requires --portal <id>")
		}
		if expected != "" && portalArg != "" && expected != portalArg {
			return die("profile %s expects portal %s, not %s", client, expected, portalArg)
		}
		args = rest
	} else if !repair {
		if expected == "" {
			return die("profile %s has no expected portal; run hubbie %s auth login --portal <id>", client, client)
		}
		l, err := a.cachedLogin(p)
		if errors.Is(err, profile.ErrNoLogin) {
			return die("profile %s is not logged in; run hubbie %s auth login --portal %s", client, client, expected)
		} else if err != nil {
			return err
		}
		if err := profile.Guard(client, expected, l.Portal); err != nil {
			return die("%v", err)
		}
	}

	bin, err := a.resolveBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return die("%v", err)
	}
	// The CLI imports auth.json even when accounts/ already holds a login.
	// Retire that input so the child uses the same layout as the guard.
	if err := p.RetireLegacyAuth(); err != nil {
		return die("%v", err)
	}
	env := withEnv(a.Env, "HOME="+p.Dir)
	// URL generation does not complete login or create a login cache.
	if !login || (len(args) >= 3 && args[2] == "url") {
		return a.exec(bin, args, env)
	}

	// A failed login must leave the expected portal unchanged.
	if err := runChild(bin, args, env); err != nil {
		return err
	}
	l, err := a.cachedLogin(p)
	if errors.Is(err, profile.ErrNoLogin) {
		return die("login finished without a readable hub_id in %s/accounts/<portal>/auth.json or %s", p.ConfigDir(), p.LegacyAuthFile())
	} else if err != nil {
		return err
	}
	target := expected
	if target == "" {
		target = portalArg
	}
	if l.Portal != target {
		return die("login returned portal %s, expected %s; profile %s is blocked until repaired", l.Portal, target, client)
	}
	if expected == "" {
		if err := os.WriteFile(p.PortalFile(), []byte(target+"\n"), 0o600); err != nil {
			return die("%v", err)
		}
	}
	return nil
}

func (a *App) profileCredentials(p profile.Profile) error {
	for _, entry := range a.Env {
		if strings.HasPrefix(entry, "HUBSPOT_ACCESS_TOKEN=") && entry != "HUBSPOT_ACCESS_TOKEN=" {
			return die("unset HUBSPOT_ACCESS_TOKEN before using a profile login; use --service-key for service-key commands")
		}
	}
	for _, path := range []string{
		filepath.Join(p.Dir, ".hubspot", ".env"),
		filepath.Join(p.Dir, ".config", "hubspot", ".env"),
	} {
		values, err := godotenv.Read(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return die("cannot read profile environment file %s. Remove or repair it before using a profile login", path)
		}
		if _, ok := values["HUBSPOT_ACCESS_TOKEN"]; ok {
			return die("remove HUBSPOT_ACCESS_TOKEN from %s before using a profile login. Use --service-key for service-key commands", path)
		}
	}
	return nil
}

// cachedLogin resolves the login and prints the layout-disagreement note.
// An ambiguous folder is returned as a ready-to-print error.
func (a *App) cachedLogin(p profile.Profile) (profile.Login, error) {
	l, err := p.CachedLogin()
	var amb *profile.ErrAmbiguous
	if errors.As(err, &amb) {
		return l, die("%v", amb)
	}
	if err == nil && l.Note != "" {
		fmt.Fprintf(a.errw, "hubbie: %s\n", l.Note)
	}
	return l, err
}

// parsePortalFlag removes --portal <id> (or --portal=<id>) from args.
func parsePortalFlag(args []string) (string, []string, error) {
	portal, seen := "", false
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--portal":
			if i+1 >= len(args) || seen {
				return "", nil, die("provide --portal exactly once with a numeric portal ID")
			}
			portal, seen = args[i+1], true
			i++
		case len(a) > 9 && a[:9] == "--portal=":
			if seen {
				return "", nil, die("provide --portal exactly once")
			}
			portal, seen = a[9:], true
		default:
			rest = append(rest, a)
		}
	}
	if portal != "" && !profile.ValidPortal(portal) {
		return "", nil, die("portal ID must contain digits only")
	}
	return portal, rest, nil
}

// exec replaces hubbie with the child, flushing redacted output first.
func (a *App) exec(bin string, args, env []string) error {
	if r, ok := a.errw.(interface{ Flush() error }); ok {
		_ = r.Flush()
	}
	err := a.Exec(bin, append([]string{bin}, args...), env)
	return die("cannot run %s: %v", bin, err)
}

func (a *App) setSecretName(p profile.Profile, args []string) error {
	if len(args) != 3 || args[1] != "set" {
		return usage()
	}
	if !profile.ValidSecretName(args[2]) {
		return die("secret name must be an environment variable name (letters, digits, underscore)")
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return die("%v", err)
	}
	if err := os.WriteFile(p.SecretNameFile(), []byte(args[2]+"\n"), 0o600); err != nil {
		return die("%v", err)
	}
	fmt.Fprintf(a.errw, "hubbie: %s will use secret %s with --service-key\n", p.Client, args[2])
	return nil
}

// provider picks the secret provider from the environment.
func (a *App) provider(name string) (secret.Provider, error) {
	cmdline := a.getenv(envSecretCommand)
	if cmdline == "" {
		return nil, die("--service-key requires HUBBIE_SECRET_COMMAND; set it to a secret-fetching command, or set %s and HUBBIE_SECRET_COMMAND='printenv {name}'", name)
	}
	argv, err := secret.ParseCommand(cmdline)
	if err != nil {
		return nil, die("%v", err)
	}
	c := secret.Command{Argv: argv, Stderr: a.errw}
	if err := c.Available(); err != nil {
		return nil, die("%v", err)
	}
	return c, nil
}

func (a *App) serviceCommand(p profile.Profile, expected string, args []string) error {
	client := p.Client
	if expected == "" {
		return die("profile %s has no expected portal; run hubbie %s auth login --portal <id>", client, client)
	}
	name, ok, err := p.SecretName()
	if err != nil {
		return die("%v", err)
	}
	if !ok {
		return die("profile %s has no service key; run hubbie %s service-key set <SECRET_NAME>", client, client)
	}
	if !profile.ValidSecretName(name) {
		return die("invalid secret name in %s", p.SecretNameFile())
	}
	prov, err := a.provider(name)
	if err != nil {
		return err
	}
	bin, err := a.resolveBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return die("%v", err)
	}
	key, err := prov.Get(name)
	if err != nil {
		return die("%v", err)
	}
	// The key lives in memory and reaches the child only through its
	// environment. Every message from here on is redacted.
	a.errw = secret.NewRedactor(a.Stderr, key)

	env := withEnv(without(a.Env, name), "HOME="+p.Dir, "HUBSPOT_ACCESS_TOKEN="+key)
	cmd := exec.Command(bin, "whoami")
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return die("whoami failed under the service key for %s", client)
	}
	actual := whoamiPortal(out.Bytes())
	if actual == "" {
		return die("could not read the portal from whoami for %s", client)
	}
	if err := profile.ServiceGuard(client, expected, actual); err != nil {
		return die("%v", err)
	}
	return a.exec(bin, args, env)
}
