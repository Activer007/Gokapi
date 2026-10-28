#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in ""|--target|--red) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec030-reservations-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec030-reservations-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
mapfile -t files < <(git -C "$repo" ls-files --cached --others --exclude-standard -- internal/uploadquota internal/configuration/database internal/test/securityfixture | grep -E '\.go$' | sort -u)
for file in "${files[@]}"; do
    mkdir -p "$work/$(dirname "$file")"
    # Hash the canonical bytes actually compiled, independent of Windows checkout EOLs.
    sed 's/\r$//' "$repo/$file" > "$work/$file"
done
(cd "$work" && sha256sum "${files[@]}") > "$out/source.sha256"
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
if [ "${1:-}" != "--red" ]; then run generate go generate ./...; fi
run contract go test ./internal/uploadquota ./internal/test/securityfixture ./internal/configuration/database ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws -run 'TestSEC030(Ledger|Durable|SQLite|Redis|Schema|Migration)' -v
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
    run race go test -race ./internal/uploadquota ./internal/test/securityfixture ./internal/configuration/database ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws -run 'TestSEC030(Ledger|Durable|SQLite|Redis|Schema|Migration)'
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
