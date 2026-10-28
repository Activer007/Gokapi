#!/usr/bin/env bash
set -euo pipefail
mode=${1:-target}
case "$mode" in target|race) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec030-whole-XXXXXX.secret")
cd "$repo"
git rev-parse HEAD > "$out/head.txt"
git ls-files -- internal/configuration/database internal/environment internal/storage internal/uploadquota internal/webserver/fileupload internal/webserver/api internal/webserver/errorHandling/errorcodes | grep -E '\.go$' | LC_ALL=C sort | xargs sha256sum > "$out/source.sha256"
sha256sum "$0" .github/workflows/test-code.yml > "$out/harness.sha256"
export GOTOOLCHAIN=local GOFLAGS=-mod=readonly
failed=0
run() {
    local name=$1 code
    shift
    printf '%s\n' "$*" > "$out/$name.command.txt"
    set +e
    timeout 900 "$@" > "$out/$name.log" 2>&1
    code=$?
    set -e
    cat "$out/$name.log"
    printf 'SEC030_WHOLE_RESULT %s %s\n' "$name" "$code" | tee -a "$out/results.tsv"
    if [ "$code" -ne 0 ]; then failed=1; fi
}
run environment go version
race=()
if [ "$mode" = race ]; then race=(-race); fi
packages=(./internal/environment ./internal/uploadquota ./internal/configuration/database ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis ./internal/storage ./internal/storage/filesystem/s3filesystem/aws ./internal/webserver/fileupload ./internal/webserver/api)
run whole go test "${race[@]}" "${packages[@]}" -count=1 -tags=test,noaws,security_whole -run 'TestSEC030Whole|TestSEC030Blob|TestSEC030AWS' -timeout 10m -v
run mock go test "${race[@]}" ./internal/storage/filesystem/s3filesystem/aws -count=1 -tags=test,awsmock -run TestSEC030AWS -v
run sdk go test "${race[@]}" ./internal/storage/filesystem/s3filesystem/aws -count=1 -tags=test -run TestSEC030AWS -v
printf 'Evidence: %s\n' "$out"
exit "$failed"
