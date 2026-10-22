#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd -P)
tmp=$(mktemp -d "$repo/.auth-layout-conflicts.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
export HOME=$tmp/outer-home RP_HUBSPOT_PROFILES=$tmp/profiles
unset HUBSPOT_PORTAL_ID HUBSPOT_ACCESS_TOKEN
mkdir -p "$HOME" "$tmp/bin"
case "$(uname -s)" in
  Darwin) cfg='Library/Application Support/hubspot' ;;
  *) cfg='.config/hubspot' ;;
esac

# Model the CLI's legacy import and account selection without network access.
cat > "$tmp/bin/hubspot" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
case "$(uname -s)" in
  Darwin) dir="$HOME/Library/Application Support/hubspot" ;;
  *) dir="$HOME/.config/hubspot" ;;
esac
if [[ -f $dir/auth.json ]]; then
  portal=$(jq -r .hub_id "$dir/auth.json")
  mkdir -p "$dir/accounts/$portal"
  cp "$dir/auth.json" "$dir/accounts/$portal/auth.json"
fi
if [[ $1 == history ]]; then exit 0; fi
accounts=("$dir"/accounts/*/auth.json)
if ((${#accounts[@]} != 1)); then
  printf '{"ok":false,"error":{"code":"ACCOUNT_AMBIGUOUS"}}\n'
  exit 1
fi
portal=$(jq -r .hub_id "${accounts[0]}")
if [[ $1 == auth && $2 == login ]]; then
  printf 'Authenticated successfully (portal %s).\n' "$portal"
else
  printf '{"portalId":%s}\n' "$portal"
fi
FAKE
chmod +x "$tmp/bin/hubspot"
export RP_HUBSPOT_BIN=${RP_HUBSPOT_TEST_BIN:-$tmp/bin/hubspot}
wrapper=$repo/bin/hubbie; export HUBBIE_SECRET_COMMAND='av inject +{name} -- printenv {name}'

seed() {
  local client=$1 portal=$2 layout=$3 file
  file="$RP_HUBSPOT_PROFILES/$client/$cfg/$layout/auth.json"
  mkdir -p "$(dirname "$file")"
  printf '{"access_token":"disposable-access","refresh_token":"disposable-refresh","hub_id":%s,"user_id":12345,"user":"test@example.invalid","scopes":[],"expires_at":4070908800,"obtained_at":1792627200}\n' "$portal" > "$file"
}
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# Reproduce migration during a guarded command, then use whoami repeatedly.
seed conflict 4000001 accounts/4000001
seed conflict 222 .
printf '4000001\n' > "$RP_HUBSPOT_PROFILES/conflict/portal-id"
"$wrapper" conflict history --format jsonl
for _ in 1 2; do
  if ! "$wrapper" conflict whoami > "$tmp/whoami"; then
    cat "$tmp/whoami" >&2
    fail 'whoami failed after the guarded command'
  fi
  [[ $(jq -r .portalId "$tmp/whoami") == 4000001 ]] || fail 'wrong portal selected'
  "$wrapper" conflict history --format jsonl
done
[[ ! -d $RP_HUBSPOT_PROFILES/conflict/$cfg/accounts/222 ]] || fail 'legacy account imported'
backups=("$RP_HUBSPOT_PROFILES/conflict/$cfg"/auth.json.legacy.*)
[[ ${#backups[@]} -eq 1 && -f ${backups[0]} ]] || fail 'legacy login backup missing'
[[ $(jq -r .hub_id "${backups[0]}") == 222 ]] || fail 'legacy login backup changed'

# Login and repair commands can be the first child invocation, too.
for command in login whoami; do
  seed "$command" 4000001 accounts/4000001
  seed "$command" 222 .
  if [[ $command == login ]]; then
    "$wrapper" login auth login --portal 4000001
    [[ $(<"$RP_HUBSPOT_PROFILES/login/portal-id") == 4000001 ]] || fail 'login portal not saved'
  else
    "$wrapper" whoami whoami > "$tmp/whoami"
    [[ $(jq -r .portalId "$tmp/whoami") == 4000001 ]] || fail 'repair selected legacy portal'
  fi
done

# An explicit second account still blocks guarded commands.
seed conflict 7 accounts/7
if "$wrapper" conflict history --format jsonl > "$tmp/out" 2> "$tmp/err"; then
  fail 'multiple accounts accepted'
fi
grep -q 'more than one portal (7 4000001)' "$tmp/err" || fail 'missing ambiguity diagnostic'

# A matching legacy cache cannot bypass a newer login's portal mismatch.
seed mismatch 4000001 accounts/4000001
seed mismatch 222 .
printf '222\n' > "$RP_HUBSPOT_PROFILES/mismatch/portal-id"
if "$wrapper" mismatch history --format jsonl > "$tmp/out" 2> "$tmp/err"; then
  fail 'portal mismatch accepted'
fi
grep -q 'expects portal 222 but login is for portal 4000001' "$tmp/err" || fail 'missing portal mismatch diagnostic'
[[ -f $RP_HUBSPOT_PROFILES/mismatch/$cfg/auth.json ]] || fail 'blocked command changed login files'

printf 'Auth layout conflict tests passed\n'
