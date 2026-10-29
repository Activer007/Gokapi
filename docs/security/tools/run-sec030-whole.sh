#!/usr/bin/env bash
set -euo pipefail
mode=${1:-target}
case "$mode" in target|race|large) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec030-whole-XXXXXX.secret")
cd "$repo"
git rev-parse HEAD > "$out/head.txt"
git ls-files --cached --others --exclude-standard -- cmd/gokapi/Main.go internal/configuration/database internal/environment internal/storage internal/uploadquota internal/webserver | grep -E '\.go$' | LC_ALL=C sort -u | xargs sha256sum > "$out/source.sha256"
sha256sum "$0" .github/workflows/test-code.yml > "$out/harness.sha256"
cat "$out/harness.sha256"
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
if [ "$mode" = large ]; then
    run large env GOKAPI_SEC030_CHUNK_LARGE=1 go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole,security_chunk,security_large -run '^TestSEC030ChunkLargeFiles$' -timeout 14m30s -v
    metrics=$(grep -c 'CHUNK_LARGE_METRIC' "$out/large.log" || true)
    if [ "$metrics" -ne 2 ]; then
        printf 'SEC030_CHUNK_LARGE_METRICS expected=2 actual=%s\n' "$metrics" | tee -a "$out/results.tsv"
        failed=1
    fi
    run source-unchanged sha256sum --check --status "$out/source.sha256"
    printf 'Evidence: %s\n' "$out"
    exit "$failed"
fi
race=()
if [ "$mode" = race ]; then race=(-race); fi
packages=(./internal/environment ./internal/uploadquota ./internal/configuration/database ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis ./internal/storage ./internal/storage/chunking/chunksession ./internal/storage/filerequest ./internal/storage/processingstatus/... ./internal/storage/filesystem/s3filesystem/aws ./internal/webserver/fileupload ./internal/webserver/fileupload/uploadservice ./internal/webserver/api ./internal/webserver)
run whole go test "${race[@]}" "${packages[@]}" -count=1 -tags=test,noaws,security_whole,security_chunk -run 'TestSEC030Whole|TestSEC030Blob|TestSEC030AWS|TestSEC030Chunk|TestSEC030Request|TestSEC030UploadService|TestSEC030UploadShutdown' -timeout 10m -v
run mock go test "${race[@]}" ./internal/storage/filesystem/s3filesystem/aws -count=1 -tags=test,awsmock -run TestSEC030AWS -v
run sdk go test "${race[@]}" ./internal/storage/filesystem/s3filesystem/aws -count=1 -tags=test -run TestSEC030AWS -v
run source-unchanged sha256sum --check --status "$out/source.sha256"
printf 'Evidence: %s\n' "$out"
exit "$failed"
