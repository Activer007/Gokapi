#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in ""|--target) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec032-managed-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec032-managed-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/webserver/api/security_managed_guard_test.go internal/webserver/api/Api_test.go internal/storage/chunking/Chunking.go internal/storage/chunking/security_scope_test.go internal/storage/FileServing.go internal/storage/FileServing_test.go internal/storage/processingstatus/pstatusdb/PStatusDb.go internal/storage/processingstatus/pstatusdb/PStatusDb_test.go internal/storage/processingstatus/pstatusdb/security_scope_test.go internal/webserver/fileupload/FileUpload.go internal/webserver/fileupload/FileUpload_test.go internal/webserver/api/Api.go internal/webserver/Webserver.go internal/webserver/security_chunk_upload_test.go)
for file in "${files[@]}"; do cp "$repo/$file" "$work/$file"; done
(cd "$repo" && sha256sum "${files[@]}") > "$out/source.sha256"
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
run scope go test ./internal/webserver/api ./internal/webserver/fileupload ./internal/webserver ./internal/storage ./internal/storage/chunking ./internal/storage/processingstatus/pstatusdb -count=1 -tags=test,noaws -run 'TestSEC032|TestNewFileFromChunk|TestChunkUpload|TestChunkComplete|TestProcessNewChunk|TestCompleteChunk' -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/webserver/api ./internal/webserver/fileupload ./internal/webserver ./internal/storage ./internal/storage/chunking ./internal/storage/processingstatus/pstatusdb -count=1 -tags=test,noaws -run TestSEC032
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
