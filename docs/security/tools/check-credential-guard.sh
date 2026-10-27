#!/usr/bin/env bash
# Mutate only an isolated archive; never change the working tree's authorization.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../../.." && pwd)
mkdir -p "$repo/docs/security/evidence"
out=$(mktemp -d "$repo/docs/security/evidence/guard-XXXXXX")
work=$(mktemp -d /tmp/gokapi-guard-XXXXXX)
git -C "$repo" archive HEAD | tar -x -C "$work"
mkdir -p "$work/internal/test/securityfixture"
cp "$repo"/internal/test/securityfixture/*.go "$work/internal/test/securityfixture/"
git -C "$repo" rev-parse HEAD > "$out/head.txt"
(cd "$repo" && sha256sum internal/test/securityfixture/*.go) > "$out/overlay.sha256"
sha256sum "$0" > "$out/runner.sha256"
printf '%s\n' "$work" > "$out/workspace.txt"
cd "$work"
run() {
    name=$1
    shift
    printf '%s\n' "$*" > "$out/$name.command.txt"
    set +e
    env -i HOME="$HOME" PATH="$PATH" GOTOOLCHAIN=auto \
        HTTPS_PROXY="${HTTPS_PROXY:-${https_proxy:-}}" HTTP_PROXY="${HTTP_PROXY:-${http_proxy:-}}" \
        NO_PROXY=localhost,127.0.0.1,::1 \
        timeout 900 "$@" > "$out/$name.log" 2>&1
    code=$?
    set -e
    printf '%s\t%s\n' "$name" "$code" | tee -a "$out/results.tsv"
}
run generate go generate ./...
test "$code" -eq 0
run control go test ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestM0ActorBoundaries -v
test "$code" -eq 0
python3 - <<'PY'
from pathlib import Path
p = Path('internal/webserver/api/Api.go')
s = p.read_text()
guard = 'if routing.IsFileRequestApi != apiKey.IsUploadRequestKey() {'
assert s.count(guard) == 1, 'expected exactly one credential-type guard'
p.write_text(s.replace(guard, 'if false { // isolated mutation probe'))
PY
run mutant go test ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestM0ActorBoundaries -v
test "$code" -eq 1
# Require all four intended assertion failures, not an unrelated build failure.
for backend in sqlite redis; do
    for boundary in request-key-on-management ordinary-key-on-request-route; do
        grep -Fq -- "--- FAIL: TestM0ActorBoundaries/$backend/$boundary " "$out/mutant.log"
    done
done
printf 'PASS: credential-type mutation detected on both providers\n' | tee "$out/verdict.txt"
printf 'Evidence: %s\n' "$out"
