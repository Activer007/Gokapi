#!/usr/bin/env bash
set -euo pipefail
mode=${1:-scan}
case "$mode" in scan|containers|historical) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec062-toolchain-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec062-toolchain-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
if [ "$mode" = historical ]; then base=cb5868c6d45d1a3f4fc163c74976c90c756ae8a8; fi
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
printf '%s\n' "$base" > "$out/head.txt"
(cd "$work" && sha256sum go.mod go.sum build/go.mod build/go.sum Dockerfile Dockerfile.cli-tool build/Dockerfile .github/workflows/test-code.yml) | tee "$out/inputs.sha256"
if [ -f "$work/internal/webserver/security_toolchain_range_test.go" ]; then
    (cd "$work" && sha256sum internal/webserver/security_toolchain_range_test.go) | tee -a "$out/inputs.sha256"
fi
(cd "$repo" && sha256sum docs/security/tools/check-sec062-toolchain.py docs/security/tools/run-sec062-toolchain.sh .github/workflows/test-code.yml) | tee "$out/harness.sha256"
export GOTOOLCHAIN=local GOBIN="$out/tools" GOFLAGS=-mod=readonly
mkdir -p "$GOBIN"
cd "$work"
failed=0
last_code=0
run() {
    local name=$1
    local code
    shift
    printf '%s\n' "$*" > "$out/$name.command.txt"
    set +e
    timeout 1200 "$@" > "$out/$name.log" 2>&1
    code=$?; last_code=$code; set -e
    cat "$out/$name.log"
    printf 'SEC062_RESULT %s %s\n' "$name" "$code" | tee -a "$out/results.tsv"
    if [ "$code" -ne 0 ]; then failed=1; fi
}
run environment go version
if [ "$mode" != historical ]; then
    run policy python3 "$repo/docs/security/tools/check-sec062-toolchain.py" --root "$work" --actual "$(go env GOVERSION)"
    if [ "$failed" -ne 0 ]; then exit 1; fi
fi
if [ "$mode" = containers ]; then
    # Only temporary runner-local images/containers; no push, host mounts or data volumes.
    tag=$(basename "$work")
    run docker-server docker build -t "$tag-server" -f Dockerfile .
    run docker-cli docker build -t "$tag-cli" -f Dockerfile.cli-tool .
    run docker-builder docker build -t "$tag-builder" -f build/Dockerfile build
    run builder-version docker run --rm --entrypoint go "$tag-builder" version
    for target in server cli; do
        name=gokapi; if [ "$target" = cli ]; then name=gokapi-cli; fi
        container=$(docker create "$tag-$target")
        docker cp "$container:/app/$name" "$out/$name"
        docker rm "$container" > /dev/null
        run "$target-buildinfo" go version -m "$out/$name"
        if ! head -n 1 "$out/$target-buildinfo.log" | grep -Eq 'go1\.26\.10([[:space:]]|$)'; then failed=1; fi
    done
    if ! grep -Fq 'go version go1.26.10 ' "$out/builder-version.log"; then failed=1; fi
else
    run scanner-install go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
    if [ "$failed" -ne 0 ]; then exit 1; fi
    run scanner-version "$GOBIN/govulncheck" -version
    run scanner-buildinfo go version -m "$GOBIN/govulncheck"
    if [ "$mode" = historical ]; then
        run historical-environment env GOTOOLCHAIN=go1.26.0 go version
        run historical-generate env GOTOOLCHAIN=go1.26.0 go generate ./...
        if [ "$failed" -ne 0 ]; then exit 1; fi
        # Preserve the scanner's real nonzero result; only the harness expects this RED.
        run historical-scan env GOTOOLCHAIN=go1.26.0 "$GOBIN/govulncheck" -show verbose ./...
        if [ "$last_code" -ne 3 ] || ! grep -Fq 'GO-2026-6609' "$out/historical-scan.log"; then exit 1; fi
        printf 'PASS: historical source scan RED includes published GO-2026-6609\n'
        failed=0
    else
        run generate go generate ./...
        if [ "$failed" -ne 0 ]; then exit 1; fi
        run formatting bash -c 'files=$(gofmt -l internal/webserver/security_toolchain_range_test.go); if [ -n "$files" ]; then gofmt -d internal/webserver/security_toolchain_range_test.go; exit 1; fi'
        run source-default "$GOBIN/govulncheck" -show verbose ./...
        run source-noaws "$GOBIN/govulncheck" -show verbose -tags=noaws ./...
        run wasm-source env GOOS=js GOARCH=wasm "$GOBIN/govulncheck" -show verbose ./cmd/wasmdownloader ./cmd/wasme2e
        # govulncheck source mode accepts package patterns, not named Go files.
        # The generator directory has multiple standalone mains, so scan its
        # actual compiled executable rather than inventing a combined package.
        run generator-build go build -tags=tools -o "$out/minify-generator" ./build/go-generate/minifyStaticContent.go
        run generator-buildinfo go version -m "$out/minify-generator"
        run generator-binary-scan "$GOBIN/govulncheck" -show verbose -mode=binary "$out/minify-generator"
        run server-build env CGO_ENABLED=0 go build -o "$out/gokapi" ./cmd/gokapi
        run cli-build env CGO_ENABLED=0 go build -o "$out/gokapi-cli" ./cmd/cli-uploader
        for name in gokapi gokapi-cli; do
            run "$name-buildinfo" go version -m "$out/$name"
            if ! head -n 1 "$out/$name-buildinfo.log" | grep -Eq 'go1\.26\.10([[:space:]]|$)'; then failed=1; fi
            run "$name-binary-scan" "$GOBIN/govulncheck" -show verbose -mode=binary "$out/$name"
        done
        sha256sum "$out/gokapi" "$out/gokapi-cli" "$out/minify-generator" internal/webserver/web/main.wasm internal/webserver/web/e2e.wasm internal/webserver/web/static/js/wasm_exec.js | tee "$out/artifacts.sha256"
    fi
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
