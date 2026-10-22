# hubbie

Per-client logins for the HubSpot CLI. One static binary, no runtime dependencies besides the `hubspot` CLI itself.

hubbie gives each client a private profile folder and runs `hubspot` with `HOME` pointed at it, so logins never mix. Before it runs a command, it checks that the cached login belongs to the portal you recorded for that client, and refuses when it does not.

## Install

Download the binary for your platform from the [releases page](https://github.com/junlov/hubbie/releases), make it executable, and put it on your `PATH`. To build from source, run `CGO_ENABLED=0 go build -o hubbie .` with Go 1.27 or later.

## Use

```
hubbie list                                   # profiles, portals, login state
hubbie acme auth login --portal 1234567       # first login records the expected portal
hubbie acme objects list                      # any hubspot command, guarded
hubbie acme service-key set ACME_TOKEN        # name the secret to use with --service-key
hubbie acme --service-key objects list        # run with a service key instead of the login
```

Client names are lowercase letters, digits, and single hyphens. Everything after the client name goes to `hubspot` unchanged, and stdin, signals, and the exit code pass through. `auth` and `whoami` skip the portal guard so you can repair a login.

## Profile layout

Each profile is a folder under the profiles root:

```
<profiles root>/<client>/
  portal-id             expected portal, digits only
  service-key-name      secret name set by `service-key set`
  <HubSpot CLI config>  the child's HOME; holds the login
```

hubbie reads both HubSpot CLI login layouts: the single `auth.json` and `accounts/<portal>/auth.json` (CLI 0.16.0 and later). If the per-portal folder holds more than one portal, hubbie refuses to run. If both layouts disagree, the per-portal file wins.

## Environment variables

| Variable | Meaning |
| --- | --- |
| `RP_HUBSPOT_PROFILES` | Profiles root. Default `~/.config/hubbie/profiles`. |
| `RP_HUBSPOT_BIN` | Path to the `hubspot` executable. Default: `hubspot` on `PATH`. |
| `HUBBIE_SECRET_COMMAND` | Command that prints a secret, see below. Unset means the environment provider. |

## Secret providers

`--service-key` runs `hubspot whoami` with the key first and refuses unless the key's portal matches the expected portal. The key stays in memory, reaches `hubspot` only through its environment as `HUBSPOT_ACCESS_TOKEN`, and is masked in anything hubbie prints.

- **Environment provider** (default): reads the variable named by `service-key set` from hubbie's own environment.
- **Command provider**: set `HUBBIE_SECRET_COMMAND` to a command that prints the secret on stdout. It runs without a shell, and `{name}` is replaced with the secret name. For a tool that injects secrets into a child's environment, end the command with `printenv {name}`:

```
export HUBBIE_SECRET_COMMAND='my-vault read {name}'
export HUBBIE_SECRET_COMMAND='my-injector run +{name} -- printenv {name}'
```

## Tests

```
go test ./...
CGO_ENABLED=0 go build -o bin/hubbie . && bash tests/run.sh
```

The shell tests need `bash` and `jq`.

## License

MIT
