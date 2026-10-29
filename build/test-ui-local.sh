#!/usr/bin/env bash
# Run from a native Linux checkout (including WSL's Linux filesystem).
set -euo pipefail
cd "$(dirname "$0")/.."
case "$PWD" in
    /mnt/[a-z]/*)
        echo "Copy this source snapshot to WSL's Linux filesystem before running; Windows mounts have different permission semantics." >&2
        exit 1
        ;;
esac

go generate ./...
node --test build/tests/*.test.cjs
go test ./... -tags=test,noaws -count=1
go test ./... -tags=test,awsmock -count=1
go test -race ./internal/i18n ./internal/models ./internal/configuration/setup \
    ./internal/webserver ./internal/webserver/api ./internal/webserver/browseraccess \
    -tags=test,noaws -run 'TestUI|TestPreference|TestSelect|TestDictionaries|TestDesktopPolicy|TestReconfigurationPreservesUI' -count=1

buildDir=$(mktemp -d)
trap 'rm -f "$buildDir/gokapi" "$buildDir/gokapi-noaws"; rmdir "$buildDir"' EXIT
go build -buildvcs=false -o "$buildDir/gokapi" ./cmd/gokapi
go build -buildvcs=false -tags=noaws -o "$buildDir/gokapi-noaws" ./cmd/gokapi
