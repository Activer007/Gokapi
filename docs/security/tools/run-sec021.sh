#!/usr/bin/env bash
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--target|--red) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec021-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec021-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
if [ "${1:-}" = --red ]; then base=bf5918e69827b75b5321acc37efd66f4b7e1b498; fi
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/test/securityfixture/revocation_test.go)
if [ "${1:-}" != --red ]; then
    files+=(internal/configuration/database/provider/redis/apikeys.go internal/configuration/database/provider/redis/users.go internal/configuration/database/provider/redis/usage.go internal/configuration/database/provider/redis/Redis_test.go)
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
run revocation go test ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestSEC021 -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/test/securityfixture ./internal/configuration/database/provider/redis ./internal/configuration/database/provider/sqlite -count=1 -tags=test,noaws
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
