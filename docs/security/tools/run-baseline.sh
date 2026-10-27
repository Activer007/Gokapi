#!/usr/bin/env bash
# Run only on a disposable source copy. Never load a deployment's environment.
set -euo pipefail
case "${1:-}" in ""|--fixtures|--red|--s3-race) ;; *) echo 'Usage: run-baseline.sh [--fixtures|--red|--s3-race]' >&2; exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
mkdir -p "$repo/docs/security/evidence"
out=$(mktemp -d "$repo/docs/security/evidence/run-XXXXXX")
work=$(mktemp -d /tmp/gokapi-m0-XXXXXX)
git -C "$repo" archive HEAD | tar -x -C "$work"
if [ -n "${1:-}" ]; then
    mkdir -p "$work/internal/test/securityfixture"
    cp "$repo"/internal/test/securityfixture/*.go "$work/internal/test/securityfixture/"
    cp "$repo/internal/configuration/setup/security_red_test.go" "$work/internal/configuration/setup/"
    cp "$repo/internal/webserver/authentication/sessionmanager/security_red_test.go" "$work/internal/webserver/authentication/sessionmanager/"
    (cd "$repo" && sha256sum internal/test/securityfixture/*.go internal/configuration/setup/security_red_test.go internal/webserver/authentication/sessionmanager/security_red_test.go) > "$out/overlay.sha256"
fi
# Do not delete the copy ourselves; OS/WSL lifecycle may still clear /tmp.
printf '%s\n' "$work" > "$out/workspace.txt"
git -C "$repo" rev-parse HEAD > "$out/head.txt"
sha256sum "$repo/docs/security/tools/run-baseline.sh" > "$out/runner.sha256"
cd "$work"
failed=0
run() {
    name=$1
    shift
    printf '%s\n' "$*" > "$out/$name.command.txt"
    set +e
    env -i HOME="$HOME" PATH="$PATH" GOTOOLCHAIN=auto \
        HTTPS_PROXY="${HTTPS_PROXY:-${https_proxy:-}}" HTTP_PROXY="${HTTP_PROXY:-${http_proxy:-}}" \
        NO_PROXY=localhost,127.0.0.1,::1 \
        timeout 900 "$@" > "$out/$name.log" 2>&1
    code=$?
    set -e
    printf '%s\t%s\n' "$name" "$code" | tee -a "$out/results.tsv"
    if [ "$code" -ne 0 ]; then failed=1; fi
}
run environment go version
run platform go env GOOS GOARCH GOTOOLCHAIN CGO_ENABLED
run generate go generate ./...
if [ "${1:-}" = "--s3-race" ]; then
    run s3-driver-race go test -race ./internal/test/securityfixture -count=1 -tags=test -run TestM0S3
    printf 'Evidence: %s\n' "$out"
    exit "$failed"
fi
if [ "${1:-}" = "--red" ]; then
    run red go test ./internal/configuration/setup ./internal/webserver/authentication/sessionmanager ./internal/test/securityfixture -count=1 -tags=test,security_red -run TestSEC
    run build go build -o ./gokapi-m0 ./cmd/gokapi
    if [ -f ./gokapi-m0 ]; then
        run binary-version ./gokapi-m0 -v
        run build-info go version -m ./gokapi-m0
        sha256sum gokapi-m0 > "$out/binary.sha256"
    fi
    run docker docker version
    printf 'Evidence: %s\n' "$out"
    exit "$failed"
fi
run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
run integration go test ./... -count=1 -tags=test,noaws,integration
run vet go vet -tags=test,noaws ./...
if [ "${1:-}" = "--fixtures" ]; then
    run fixtures-race go test -race ./internal/test/securityfixture -count=1 -tags=test,awsmock
    run s3-driver go test ./internal/test/securityfixture -count=1 -tags=test -run TestM0S3
    run red go test ./internal/configuration/setup ./internal/webserver/authentication/sessionmanager ./internal/test/securityfixture -count=1 -tags=test,security_red -run TestSEC
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
