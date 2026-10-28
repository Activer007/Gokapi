#!/usr/bin/env bash
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--target) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec023-form-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec023-form-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/webserver/BrowserCsrf.go internal/webserver/Webserver.go internal/webserver/Webserver_test.go internal/webserver/authentication/Authentication.go internal/webserver/authentication/csrftoken/CsrfToken.go internal/webserver/authentication/csrftoken/security_binding_test.go internal/webserver/security_bounds_test.go internal/webserver/security_cookie_test.go internal/webserver/security_form_cookie_test.go)
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
run context go test ./internal/webserver ./internal/webserver/authentication/csrftoken ./internal/webserver/authentication/oauth -count=1 -tags=test,noaws -run 'TestSEC023|TestSEC012|TestSEC014CancelledLogin' -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/webserver/authentication/csrftoken ./internal/webserver/authentication/oauth -count=1 -tags=test,noaws
    run handler-race go test -race ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC023|TestSEC012PasswordChange|TestSEC014CancelledLogin'
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
