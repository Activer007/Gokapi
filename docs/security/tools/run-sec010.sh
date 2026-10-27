#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in ""|--red) ;; *) echo 'Usage: run-sec010.sh [--red]' >&2; exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec010-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec010-XXXXXX)
git -C "$repo" archive HEAD | tar -x -C "$work"
cp "$repo"/internal/configuration/setup/*.go "$work/internal/configuration/setup/"
cp -r "$repo"/internal/configuration/setup/templates/. "$work/internal/configuration/setup/templates/"
(cd "$repo" && sha256sum internal/configuration/setup/*.go internal/configuration/setup/templates/*) > "$out/overlay.sha256"
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
run sec010 go test ./internal/configuration/setup -count=1 -tags=test,noaws,security_red -run TestSEC010 -v
if [ "${1:-}" != "--red" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run setup-race go test -race ./internal/configuration/setup -count=1 -tags=test,noaws -run TestSEC010
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
