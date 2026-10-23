# hubbie

Per-client logins for the HubSpot CLI. Profile logins need only the static hubbie binary and the `hubspot` CLI. Service keys also need a [secret provider](#secret-providers).

hubbie gives each client a private profile folder and runs `hubspot` with `HOME` pointed at it. Before ordinary profile commands, hubbie compares the cached login with the recorded portal. If the portals differ, hubbie refuses to run the command.

## Install

Download the binary for your platform from the [releases page](https://github.com/junlov/hubbie/releases).
Make it executable.
Put it on your `PATH`.

For the minimum Go version, see [go.mod](go.mod).
To build from source, run `CGO_ENABLED=0 go build -o hubbie .`.

## Use

```
hubbie list                                   # profiles, portals, login state
hubbie acme auth login --portal 1234567       # first login records the expected portal
hubbie acme objects list                      # any hubspot command, guarded
hubbie acme service-key set ACME_TOKEN        # name the secret to use with --service-key
hubbie acme --service-key objects list        # run with a service key instead of the login
```

Client names contain lowercase letters and digits, with single hyphens between groups. The name `list` is reserved for the profile listing command.

hubbie handles `service-key set` and `--service-key` itself. During `auth login`, it also consumes `--portal <id>` or `--portal=<id>` instead of forwarding it. Other arguments, stdin, signals, and the child exit code pass through. `auth` and `whoami` skip the cached portal comparison so you can repair a login.

The first login requires `--portal`. hubbie records the expected portal only after a successful login with a matching cached portal. `auth login url` only generates a URL and does not record the expected portal or complete login.

## Profile layout

Each profile folder is the child process HOME:

```
<profiles root>/<client>/
  portal-id             expected portal, digits only
  service-key-name      secret name set by `service-key set`
  <HubSpot CLI config>  holds the login at the platform path below
```

hubbie reads both HubSpot CLI login layouts: the single `auth.json` and `accounts/<portal>/auth.json` (CLI 0.16.0 and later). These paths are relative to the HubSpot configuration folder inside the profile HOME. On macOS, this folder is `Library/Application Support/hubspot`. On other platforms, it is `.config/hubspot`.

If the per-portal layout contains more than one distinct readable portal, hubbie refuses ordinary profile commands. If both layouts disagree, the per-portal file wins. Before profile commands, hubbie renames an existing legacy `auth.json` to `auth.json.legacy.*` when a readable per-portal login exists. This backup prevents the HubSpot CLI from importing the legacy login as another account.

## Environment variables

| Variable | Meaning |
| --- | --- |
| `RP_HUBSPOT_PROFILES` | Profiles root. Default `~/.config/hubbie/profiles`. |
| `RP_HUBSPOT_BIN` | Path to the `hubspot` executable. Default: `hubspot` on `PATH`. |
| `HUBBIE_SECRET_COMMAND` | Command that prints a secret, see below. Required for `--service-key`. |

## Secret providers

`--service-key` runs `hubspot whoami` with the key first and refuses unless the key's portal matches the expected portal. This mode refuses `auth` commands. The key stays in memory and reaches `hubspot` through its environment as `HUBSPOT_ACCESS_TOKEN`.

hubbie masks the fetched key in its own diagnostics after retrieval. Secret provider stderr and final HubSpot command output go directly to the terminal without this filter.

hubbie has no built-in vault. Set `HUBBIE_SECRET_COMMAND` to a command that prints the secret on stdout. hubbie splits the command on whitespace and runs it without a shell. Shell quoting, variable expansion, and pipelines are unavailable. Each `{name}` becomes the secret name from `service-key set`. For a vault that injects secrets into a child process, end the command with `printenv {name}`:

```
export HUBBIE_SECRET_COMMAND='my-vault read {name}'
export HUBBIE_SECRET_COMMAND='my-injector run +{name} -- printenv {name}'
```

For an environment secret, set the named variable and select `printenv` explicitly:

```
export ACME_TOKEN='your-service-key'
export HUBBIE_SECRET_COMMAND='printenv {name}'
```

If `HUBBIE_SECRET_COMMAND` is unset, service-key commands refuse to run. The command line never selects the environment provider implicitly.

If you use a profile login, unset `HUBSPOT_ACCESS_TOKEN` first. hubbie refuses inherited tokens for ordinary commands, repair commands, and login. Service-key commands replace inherited tokens with the fetched key.

Profile login commands also refuse token declarations in `<profile HOME>/.hubspot/.env` and `<profile HOME>/.config/hubspot/.env`. Remove `HUBSPOT_ACCESS_TOKEN` from these files before you run a profile login command. Missing files and valid files without this declaration are allowed. If hubbie cannot read or parse either file, it refuses to run the command.

## Tests

```
go test ./...
CGO_ENABLED=0 go build -o bin/hubbie . && bash tests/run.sh
```

The shell tests need `bash` and `jq`.

## License

MIT
