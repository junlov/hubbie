#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd -P)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export HOME=$tmp/real-home
export RP_HUBSPOT_PROFILES=$tmp/profiles
export PATH=$tmp/bin:$PATH
mkdir -p "$HOME" "$tmp/bin"

case "$(uname -s)" in
  Darwin) auth_rel='Library/Application Support/hubspot/auth.json' ;;
  *) auth_rel='.config/hubspot/auth.json' ;;
esac
mkdir -p "$HOME/$(dirname "$auth_rel")"
printf '{"hub_id":999,"refresh_token":"real-home-secret"}\n' > "$HOME/$auth_rel"

cat > "$tmp/bin/hubspot" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$HOME" > "$CAPTURE_HOME"
printf '%s\n' "$@" > "$CAPTURE_ARGS"
cat > "$CAPTURE_STDIN"
if [[ ${1-} == auth && ${2-} == login ]]; then
  case "$(uname -s)" in
    Darwin) file="$HOME/Library/Application Support/hubspot/auth.json" ;;
    *) file="$HOME/.config/hubspot/auth.json" ;;
  esac
  # CLI 0.16.0 and later write accounts/<portal>/auth.json instead.
  [[ -z ${FAKE_ACCOUNTS:-} ]] || file="$(dirname "$file")/accounts/${FAKE_PORTAL:-1000001}/auth.json"
  mkdir -p "$(dirname "$file")"
  printf '{"hub_id":%s,"refresh_token":"test-secret"}\n' "${FAKE_PORTAL:-1000001}" > "$file"
fi
exit "${FAKE_EXIT:-0}"
FAKE
chmod +x "$tmp/bin/hubspot"
export CAPTURE_HOME=$tmp/captured-home CAPTURE_ARGS=$tmp/captured-args CAPTURE_STDIN=$tmp/captured-stdin
wrapper=$repo/bin/hubbie
export HUBBIE_SECRET_COMMAND='fake-injector inject +{name} -- printenv {name}'

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
expect_failure() {
  if "$@" > "$tmp/out" 2> "$tmp/err"; then fail "unexpected success: $*"; fi
  [[ ! -s $tmp/out ]] || fail 'diagnostic leaked to stdout'
}

expect_failure "$wrapper" acme objects list
grep -Eq 'not logged in|no expected portal' "$tmp/err" || fail 'missing login diagnostic'
expect_failure "$wrapper" '../acme' whoami
expect_failure "$wrapper" 'ACME' whoami
expect_failure "$wrapper" 'acme/bar' whoami

FAKE_PORTAL=123 "$wrapper" acme auth login --portal 1000001 > "$tmp/out" 2> "$tmp/err" && fail 'mismatched first login accepted'
[[ ! -f $RP_HUBSPOT_PROFILES/acme/portal-id ]] || fail 'mismatched portal recorded'
grep -q 'expected 1000001' "$tmp/err" || fail 'missing mismatch diagnostic'

"$wrapper" acme auth login --portal 1000001
[[ $(<"$RP_HUBSPOT_PROFILES/acme/portal-id") == 1000001 ]] || fail 'portal not recorded'
[[ $(<"$CAPTURE_HOME") == "$RP_HUBSPOT_PROFILES/acme" ]] || fail 'child HOME differs from profile'
[[ $(cat "$CAPTURE_ARGS") == $'auth\nlogin' ]] || fail 'wrapper portal option reached child'
printf 'hello stdin\n' | "$wrapper" acme objects search 'two words' --limit 3
[[ $(<"$CAPTURE_STDIN") == 'hello stdin' ]] || fail 'stdin was not passed through'
[[ $(cat "$CAPTURE_ARGS") == $'objects\nsearch\ntwo words\n--limit\n3' ]] || fail 'arguments changed'

if FAKE_EXIT=37 "$wrapper" acme objects list > "$tmp/out" 2> "$tmp/err"; then fail 'exit code swallowed'; else
  [[ $? -eq 37 ]] || fail 'exit code changed'
fi

printf '{"hub_id":222,"refresh_token":"test-secret"}\n' > "$RP_HUBSPOT_PROFILES/acme/$auth_rel"
rm -f "$CAPTURE_ARGS"
expect_failure "$wrapper" acme objects list
grep -q 'expects portal 1000001 but login is for portal 222' "$tmp/err" || fail 'missing mismatch diagnostic'
[[ ! -e $CAPTURE_ARGS ]] || fail 'child ran despite portal mismatch'
"$wrapper" acme whoami
"$wrapper" acme auth login --portal 1000001

rm "$RP_HUBSPOT_PROFILES/acme/$auth_rel"
rm -f "$CAPTURE_ARGS"
expect_failure "$wrapper" acme objects list
[[ ! -e $CAPTURE_ARGS ]] || fail 'child ran without login'
"$wrapper" acme whoami

"$wrapper" list > "$tmp/list"
grep -q $'^acme\t1000001\tno\t-$' "$tmp/list" || fail 'list does not show profile status'
! grep -q 'secret' "$tmp/list" || fail 'list leaked token'
[[ $(<"$HOME/$auth_rel") == '{"hub_id":999,"refresh_token":"real-home-secret"}' ]] || fail 'real HOME auth changed'
[[ $(find "$HOME" -type f | wc -l | tr -d ' ') == 1 ]] || fail 'real HOME was modified'

# CLI 0.16.0 per-portal layout
cfg=$(dirname "$auth_rel")
FAKE_ACCOUNTS=1 FAKE_PORTAL=4000001 "$wrapper" newl auth login --portal 4000001
[[ -f $RP_HUBSPOT_PROFILES/newl/$cfg/accounts/4000001/auth.json ]] || fail 'fake did not write per-portal file'
[[ ! -e $RP_HUBSPOT_PROFILES/newl/$auth_rel ]] || fail 'old file unexpectedly present'
[[ $(<"$RP_HUBSPOT_PROFILES/newl/portal-id") == 4000001 ]] || fail 'per-portal login not recorded'
rm -f "$CAPTURE_ARGS"
"$wrapper" newl objects list
[[ -e $CAPTURE_ARGS ]] || fail 'child did not run for per-portal login'
FAKE_ACCOUNTS=1 FAKE_PORTAL=7 expect_failure "$wrapper" newl auth login
grep -q 'more than one portal (7 4000001)' "$tmp/err" || fail 'missing ambiguity diagnostic'
rm -f "$CAPTURE_ARGS"
expect_failure "$wrapper" newl objects list
grep -q 'more than one portal' "$tmp/err" || fail 'ambiguous login accepted'
! grep -q 'not logged in' "$tmp/err" || fail 'ambiguity reported as not logged in'
[[ ! -e $CAPTURE_ARGS ]] || fail 'child ran despite ambiguity'
"$wrapper" list > "$tmp/list"
grep -q $'^newl\t4000001\tno\t-$' "$tmp/list" || fail 'ambiguous profile listed as logged in'
rm -r "$RP_HUBSPOT_PROFILES/newl/$cfg/accounts/7"
"$wrapper" list > "$tmp/list"
grep -q $'^newl\t4000001\tyes\t-$' "$tmp/list" || fail 'list does not show per-portal login'
# both layouts, disagreeing: per-portal file wins and the wrapper says so
printf '{"hub_id":222,"refresh_token":"test-secret"}\n' > "$RP_HUBSPOT_PROFILES/newl/$auth_rel"
"$wrapper" newl objects list 2> "$tmp/err"
grep -q 'old auth.json says portal 222 but accounts/ says 4000001' "$tmp/err" || fail 'missing disagreement note'
# old layout alone still works
FAKE_PORTAL=321 "$wrapper" oldl auth login --portal 321
[[ $(<"$RP_HUBSPOT_PROFILES/oldl/portal-id") == 321 ]] || fail 'old layout login not recorded'
"$wrapper" oldl objects list
"$wrapper" list > "$tmp/list"
grep -q $'^oldl\t321\tyes\t-$' "$tmp/list" || fail 'list does not show old-layout login'
# login that writes neither layout fails closed
printf '{"refresh_token":"x"}\n' > "$RP_HUBSPOT_PROFILES/oldl/$auth_rel"
rm -rf "$RP_HUBSPOT_PROFILES/oldl/$cfg/accounts"
expect_failure "$wrapper" oldl objects list
grep -q 'not logged in' "$tmp/err" || fail 'unreadable hub_id accepted'
! grep -q 'secret' "$tmp/err" || fail 'diagnostic leaked token'

# service-key mode (secret command provider)
cat > "$tmp/bin/fake-injector" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == inject && $2 == +* ]] || exit 64
key=${2#+}; shift 2
[[ $1 == -- ]] && shift
printf '%s\n' "$key" > "$CAPTURE_SECRET_NAME"
export "$key=fake-injected-secret-value"
exec "$@"
FAKE
chmod +x "$tmp/bin/fake-injector"
cat > "$tmp/bin/hubspot" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
if [[ ${1-} == whoami ]]; then
  printf '{"portalId":%s}\n' "${FAKE_SK_PORTAL:-1000001}"
  [[ -z ${FAKE_LEAK:-} ]] || printf '%s\n' "$HUBSPOT_ACCESS_TOKEN"
  exit 0
fi
printf '%s\n' "$@" > "$CAPTURE_ARGS"
printf '%s\n' "$HUBSPOT_ACCESS_TOKEN" > "$CAPTURE_TOKEN"
exit "${FAKE_EXIT:-0}"
FAKE
chmod +x "$tmp/bin/hubspot"
export CAPTURE_SECRET_NAME=$tmp/secret-name CAPTURE_TOKEN=$tmp/token
secret=fake-injected-secret-value
no_secret() { ! grep -q "$secret" "$tmp/out" "$tmp/err" || fail 'injected secret leaked'; }

rm -f "$CAPTURE_ARGS"
expect_failure "$wrapper" acme --service-key objects list
grep -q 'no service key' "$tmp/err" || fail 'missing key-name diagnostic'
[[ ! -e $CAPTURE_ARGS ]] || fail 'child ran without key name'
expect_failure "$wrapper" acme service-key set 'bad name'
"$wrapper" acme service-key set ACME_SERVICE_KEY 2> "$tmp/err"
[[ $(<"$RP_HUBSPOT_PROFILES/acme/service-key-name") == ACME_SERVICE_KEY ]] || fail 'key name not stored'
"$wrapper" list > "$tmp/list"
grep -q $'^acme\t1000001\tno\tACME_SERVICE_KEY$' "$tmp/list" || fail 'list lacks key name'

FAKE_SK_PORTAL=555 expect_failure "$wrapper" acme --service-key objects list
grep -q 'is for portal 555, expected 1000001' "$tmp/err" || fail 'missing service portal mismatch diagnostic'
[[ ! -e $CAPTURE_ARGS ]] || fail 'child ran despite service portal mismatch'
no_secret

"$wrapper" acme --service-key objects search 'two words' --limit 3 > "$tmp/out" 2> "$tmp/err"
[[ $(cat "$CAPTURE_ARGS") == $'objects\nsearch\ntwo words\n--limit\n3' ]] || fail 'service-key arguments changed'
[[ $(<"$CAPTURE_SECRET_NAME") == ACME_SERVICE_KEY ]] || fail 'wrong secret name requested'
[[ $(<"$CAPTURE_TOKEN") == "$secret" ]] || fail 'token not passed to hubspot'
no_secret
if FAKE_EXIT=41 "$wrapper" acme --service-key objects list > "$tmp/out" 2> "$tmp/err"; then fail 'service exit swallowed'; else
  [[ $? -eq 41 ]] || fail 'service exit code changed'
fi
no_secret

bash "$repo/tests/service-key-collisions.sh"
bash "$repo/tests/auth-layout-conflicts.sh"

printf 'All tests passed\n'

