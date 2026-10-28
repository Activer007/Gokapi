#!/usr/bin/env bash
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--target) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec020-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec020-XXXXXX)
git -C "$repo" -c core.autocrlf=false archive HEAD | tar -x -C "$work"
files=(internal/test/securityfixture/authorization_matrix_test.go internal/webserver/api/Api.go internal/webserver/api/Api_test.go internal/webserver/api/security_authorization_test.go internal/webserver/Webserver.go internal/webserver/authentication/tokengeneration/TokenGeneration.go)
for file in "${files[@]}"; do cp "$repo/$file" "$work/$file"; done
mkdir -p "$work/internal/authorization"
for file in "$repo"/internal/authorization/*.go; do
    relative=${file#"$repo/"};files+=("$relative");cp "$file" "$work/$relative"
done
(cd "$repo" && sha256sum "${files[@]}") > "$out/overlay.sha256"
git -C "$repo" rev-parse HEAD > "$out/head.txt"
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
run matrix go test ./internal/test/securityfixture ./internal/authorization ./internal/webserver/api ./internal/webserver/authentication/tokengeneration -count=1 -tags=test,noaws -run 'TestSEC020|TestGenerate' -v
run api go test ./internal/webserver/api -count=1 -tags=test,noaws
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run matrix-race go test -race ./internal/test/securityfixture ./internal/authorization ./internal/webserver/api ./internal/webserver/authentication/tokengeneration -count=1 -tags=test,noaws -run 'TestSEC020|TestGenerate'
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
