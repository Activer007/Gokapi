#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec011-go-XXXXXX")
work=$(mktemp -d /tmp/gokapi-sec011-go-XXXXXX)
git -C "$repo" archive HEAD | tar -x -C "$work"
git -C "$repo" rev-parse HEAD > "$out/head.txt"
sha256sum "$0" > "$out/runner.sha256"
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
run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
run integration go test ./... -count=1 -tags=test,noaws,integration
printf 'Evidence: %s\n' "$out"
exit "$failed"
