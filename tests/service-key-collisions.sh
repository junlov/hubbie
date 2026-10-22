#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd -P)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export RP_HUBSPOT_PROFILES=$tmp/profiles
export HUBBIE_SECRET_COMMAND='fake-injector inject +{name} -- printenv {name}'
export PATH=$tmp/bin:$PATH
export RP_HUBSPOT_BIN=$tmp/bin/hubspot
export CAPTURE_CALLS=$tmp/calls
mkdir -p "$tmp/bin" "$RP_HUBSPOT_PROFILES/acme"
printf '1000001\n' > "$RP_HUBSPOT_PROFILES/acme/portal-id"

cat > "$tmp/bin/fake-injector" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == inject && $2 == +* && $3 == -- ]] || exit 64
export "${2#+}=fake-injected-secret-value"
shift 3
exec "$@"
FAKE
cat > "$tmp/bin/hubspot" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
[[ ${HUBSPOT_ACCESS_TOKEN-} == fake-injected-secret-value ]] || exit 65
printf '%s\n' "$*" >> "$CAPTURE_CALLS"
if [[ $1 == whoami ]]; then
  printf '{"portalId":1000001}\n'
fi
FAKE
chmod +x "$tmp/bin/fake-injector" "$tmp/bin/hubspot"

for key in HUBSPOT_ACCESS_TOKEN name expected binary profile client who actual _hubspot_service_token; do
  "$repo/bin/hubbie" acme service-key set "$key" > "$tmp/out" 2> "$tmp/err"
  : > "$CAPTURE_CALLS"
  if ! "$repo/bin/hubbie" acme --service-key objects list >> "$tmp/out" 2>> "$tmp/err"; then
    printf 'FAIL: service key named %s failed\n' "$key" >&2
    exit 1
  fi
  [[ $(cat "$CAPTURE_CALLS") == $'whoami\nobjects list' ]] || exit 1
  if grep -q 'fake-injected-secret-value' "$tmp/out" "$tmp/err"; then
    printf 'FAIL: injected secret leaked\n' >&2
    exit 1
  fi
done
printf 'Service-key collision tests passed\n'
