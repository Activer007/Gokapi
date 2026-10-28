#!/usr/bin/env bash
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--target|--mutation) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec020-capability-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec020-capability-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/webserver/api/security_capability_test.go internal/webserver/security_public_capability_test.go)
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
    # Deliberate faults only in the disposable archive, never in the checkout.
    for fault in password-binding pending-state; do
        if [ "$fault" = password-binding ]; then
            target=internal/webserver/authentication/downloadPasswordToken/DownloadPasswordToken.go
            pattern='if token.FileId != fileId {'
            filter='TestSEC020PublicDownloadMatrix/.*/(download|filename)/password-wrong-file'
            expected='denied public request returned file bytes'
        else
            target=internal/storage/FileServing.go
            pattern='if file.IsPendingForDeletion() {'
            filter='TestSEC020PublicDownloadMatrix/.*/.*/pending'
            expected='denied public request returned file bytes'
        fi
        cp "$target" "$target.original"
        python3 - "$target" "$pattern" <<'PY'
import sys
from pathlib import Path
p = Path(sys.argv[1])
s = p.read_text()
assert s.count(sys.argv[2]) == 1, 'mutation target changed'
p.write_text(s.replace(sys.argv[2], 'if false {'))
PY
        run "$fault-red" go test ./internal/webserver -count=1 -tags=test,noaws -run "$filter" -v
        code=$(tail -n 1 "$out/results.tsv" | cut -f2)
        cp "$target.original" "$target"
        if [ "$code" != 1 ] || ! grep -q "$expected" "$out/$fault-red.log"; then
            printf 'Mutation did not trigger expected assertion: %s\n' "$fault"
            exit 1
        fi
        failed=0
    done
    printf 'Evidence: %s\n' "$out"
    exit 0
fi
run matrix go test ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC020CapabilityEveryRoute|TestSEC020PublicDownloadMatrix' -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC020CapabilityEveryRoute|TestSEC020PublicDownloadMatrix'
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
