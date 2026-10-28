#!/usr/bin/env bash
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--target|--red) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec021-presign-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec021-presign-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
if [ "${1:-}" = --red ]; then base=d599aabfbc7f52aa06a14a006348992adc94247a; fi
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/webserver/security_presign_test.go)
if [ "${1:-}" != --red ]; then
    files+=(internal/models/Presign.go internal/storage/presign/presign.go internal/storage/presign/presign_test.go internal/webserver/api/Api.go internal/webserver/Webserver.go)
fi
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
run presign go test ./internal/webserver ./internal/storage/presign -count=1 -tags=test,noaws -run TestSEC021Presign -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/webserver ./internal/storage/presign -count=1 -tags=test,noaws -run TestSEC021Presign
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
