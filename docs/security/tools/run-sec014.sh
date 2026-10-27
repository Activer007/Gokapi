#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in ""|--red|--awsmock) ;; *) echo 'Usage: run-sec014.sh [--red|--awsmock]' >&2; exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec014-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec014-XXXXXX)
git -C "$repo" archive HEAD | tar -x -C "$work"
files=(internal/webserver/ratelimiter/RateLimiter.go internal/webserver/ratelimiter/security_bounds_test.go
  internal/webserver/Webserver.go internal/webserver/security_bounds_test.go internal/webserver/api/Api.go internal/webserver/api/security_bounds_test.go
  internal/configuration/setup/Setup.go internal/configuration/setup/Setup_test.go internal/configuration/setup/security_bounds_test.go)
for file in "${files[@]}"; do cp "$repo/$file" "$work/$file"; done
(cd "$repo" && sha256sum "${files[@]}") > "$out/overlay.sha256"
git -C "$repo" rev-parse HEAD > "$out/head.txt"
sha256sum "$0" > "$out/runner.sha256"
printf '%s\n' "$work" > "$out/workspace.txt"
cd "$work"
failed=0
run() {
    name=$1; shift
    printf '%s\n' "$*" > "$out/$name.command.txt"
    set +e
    env -i HOME="$HOME" PATH="$PATH" GOTOOLCHAIN=auto \
      HTTPS_PROXY="${HTTPS_PROXY:-${https_proxy:-}}" HTTP_PROXY="${HTTP_PROXY:-${http_proxy:-}}" \
      NO_PROXY=localhost,127.0.0.1,::1 timeout 900 "$@" > "$out/$name.log" 2>&1
    code=$?
    set -e
    printf '%s\t%s\n' "$name" "$code" | tee -a "$out/results.tsv"
    if [ "$code" -ne 0 ]; then failed=1; fi
}
run environment go version
run generate go generate ./...
if [ "${1:-}" = "--awsmock" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
else
    run sec014 go test ./internal/webserver/ratelimiter ./internal/webserver ./internal/webserver/api ./internal/configuration/setup -count=1 -tags=test,noaws -run 'TestSEC014|TestIntegration' -v
    if [ "${1:-}" != "--red" ]; then
        run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
        run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
        run integration go test ./... -count=1 -tags=test,noaws,integration
        run targeted-race go test -race ./internal/webserver/ratelimiter ./internal/webserver ./internal/webserver/api ./internal/configuration/setup -count=1 -tags=test,noaws -run TestSEC014
    fi
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
