#!/usr/bin/env bash
# Parse the complete run before executing it, so a later worktree edit cannot
# change commands midway through a long test run.
run_suite() {
set -euo pipefail
case "${1:-}" in ""|--red|--review) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec014-budget-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec014-budget-XXXXXX)
git -C "$repo" -c core.autocrlf=false archive HEAD | tar -x -C "$work"
files=()
for file in internal/webserver/Webserver.go internal/webserver/ManagementBudget.go internal/webserver/security_bounds_test.go internal/webserver/security_budget_test.go internal/webserver/api/routing.go internal/webserver/api/security_budget_test.go internal/webserver/authentication/oauth/Oauth.go internal/webserver/authentication/oauth/security_budget_test.go; do
    if [ -f "$repo/$file" ]; then files+=("$file"); cp "$repo/$file" "$work/$file"; fi
done
if [ -d "$repo/internal/webserver/requestbudget" ]; then
    mkdir -p "$work/internal/webserver/requestbudget"
    for file in "$repo"/internal/webserver/requestbudget/*.go; do
        relative=${file#"$repo/"}; files+=("$relative"); cp "$file" "$work/$relative"
    done
fi
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
packages=(./internal/webserver ./internal/webserver/api ./internal/webserver/authentication/oauth)
if [ -d internal/webserver/requestbudget ]; then packages+=(./internal/webserver/requestbudget); fi
run budget go test "${packages[@]}" -count=1 -tags=test,noaws -run 'TestSEC014|TestSEC012Callback' -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
fi
if [ "${1:-}" != "--red" ]; then
    run targeted-race go test -race ./internal/webserver ./internal/webserver/api ./internal/webserver/authentication/oauth ./internal/webserver/requestbudget -count=1 -tags=test,noaws -run 'TestSEC014|TestSEC012Callback'
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
}
run_suite "$@"
