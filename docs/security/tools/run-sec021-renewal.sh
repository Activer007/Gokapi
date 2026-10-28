#!/usr/bin/env bash
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--target|--mutation) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec021-renewal-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec021-renewal-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/configuration/database/security_session_test.go internal/configuration/database/Database.go internal/webserver/authentication/sessionmanager/SessionManager.go internal/webserver/authentication/sessionmanager/security_rotation_contract_test.go)
for file in "${files[@]}"; do cp "$repo/$file" "$work/$file"; done
(cd "$repo" && sha256sum "${files[@]}") > "$out/overlay.sha256"
printf '%s\n' "$base" > "$out/head.txt"
sha256sum "$0" > "$out/runner.sha256"
cd "$work"
failed=0
run() {
    name=$1; shift
    printf '%s\n' "$*" > "$out/$name.command.txt"
    set +e
    env -i HOME="$HOME" PATH="$PATH" GOTOOLCHAIN=auto HTTPS_PROXY="${HTTPS_PROXY:-${https_proxy:-}}" HTTP_PROXY="${HTTP_PROXY:-${http_proxy:-}}" NO_PROXY=localhost,127.0.0.1,::1 timeout 900 "$@" > "$out/$name.log" 2>&1
    code=$?; set -e
    printf '%s\t%s\n' "$name" "$code" | tee -a "$out/results.tsv"
    if [ "$code" -ne 0 ]; then failed=1; fi
}
run environment go version
run generate go generate ./...
if [ "$failed" -ne 0 ]; then exit 1; fi
if [ "${1:-}" = "--mutation" ]; then
    python3 - <<'PY'
from pathlib import Path
p = Path('internal/configuration/database/Database.go')
s = p.read_text()
for signature in ['func DeleteAllSessions() {', 'func DeleteAllSessionsByUser(userId int) {']:
    before = signature + '\n\tsessionMutation.Lock()\n\tdefer sessionMutation.Unlock()'
    assert s.count(before) == 1
    s = s.replace(before, signature)
p.write_text(s)
PY
    run ordering-red go test ./internal/configuration/database -count=1 -tags=test,noaws -run TestSEC021SessionMutationOrdering -timeout 60s -v
    code=$(tail -n 1 "$out/results.tsv" | cut -f2)
    if [ "$code" != 1 ] || ! grep -q '^--- FAIL: TestSEC021SessionMutationOrdering (' "$out/ordering-red.log" || [ "$(grep -c 'revocation left a session usable' "$out/ordering-red.log")" != 4 ]; then
        printf 'Mutation failed to trigger expected assertion\n'
        exit 1
    fi
    printf 'Evidence: %s\n' "$out"
    exit 0
fi
run contract go test ./internal/configuration/database ./internal/webserver/authentication/sessionmanager -count=1 -tags=test,noaws -run 'TestSEC021Session' -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/configuration/database/... ./internal/webserver/authentication/sessionmanager -count=1 -tags=test,noaws
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
