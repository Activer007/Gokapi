#!/usr/bin/env bash
set -euo pipefail
mode=${1:-full}
case "$mode" in full|target|large|red-policy|red-entry|red-storage) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec030-receiver-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec030-receiver-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
if [[ "$mode" == red-* ]]; then base=f4ce5db9f525eaf625071cbd75a5d93fe4c2a647; fi
historical_test_ref=f563d9f5bcb4fea30cfb4c5fc1ccb0b22a284313
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
case "$mode" in
    red-policy) files=(internal/environment/Environment.go internal/environment/security_receive_policy_test.go) ;;
    red-entry) files=(internal/webserver/fileupload/security_receive_test.go) ;;
    red-storage) files=(internal/storage/security_receive_storage_test.go) ;;
    *) mapfile -t files < <(git -C "$repo" ls-files --cached --others --exclude-standard -- internal/environment internal/webserver/fileupload internal/webserver/api internal/storage/uploadreceive internal/storage/FileServing.go internal/storage/security_receive_storage_test.go internal/webserver/Webserver.go internal/webserver/Webserver_test.go internal/webserver/security_receive_logging_test.go internal/webserver/errorHandling/errorcodes | grep -E '\.go$' | sort -u) ;;
esac
for file in "${files[@]}"; do
    mkdir -p "$work/$(dirname "$file")"
    if [ "$mode" = red-entry ]; then
        git -C "$repo" show "${historical_test_ref}:${file}" > "$work/$file"
    else
        sed 's/\r$//' "$repo/$file" > "$work/$file"
    fi
done
(cd "$work" && sha256sum "${files[@]}") > "$out/source.sha256"
if [ "$mode" = red-entry ]; then
    printf '%s\n' "$historical_test_ref" | tee "$out/historical-test-ref.txt"
    cat "$out/source.sha256"
fi
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
environment_code=$code
case "$mode" in
    red-policy) run contract go test ./internal/environment -count=1 -tags=test,noaws -run TestSEC030ReceivePolicy -v ;;
    red-entry) run contract go test ./internal/webserver/fileupload -count=1 -tags=test,noaws -run TestSEC030Receive -v ;;
    red-storage) run contract go test ./internal/storage -count=1 -tags=test,noaws -run TestSEC030ReceiveEncryptedSourceClosed -v ;;
    large) run generate go generate ./...; run large go test ./internal/webserver/api -count=1 -tags=test,noaws,security_large -run TestSEC030ReceiveLargeFiles -timeout 15m -v ;;
    *)
        run generate go generate ./...
        run contract go test ./internal/environment ./internal/storage ./internal/storage/uploadreceive ./internal/webserver/fileupload ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC030Receive|TestSEC031UploadSizeSemantics|TestReceive|TestSpool' -v
        run race go test -race ./internal/environment ./internal/storage ./internal/storage/uploadreceive ./internal/webserver/fileupload ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC030Receive|TestSEC031UploadSizeSemantics|TestReceive|TestSpool'
        if [ "$mode" = full ]; then
            run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
            run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
            run integration go test ./... -count=1 -tags=test,noaws,integration
        fi ;;
esac
if [ "$mode" = red-entry ]; then
    if [ "$environment_code" -eq 0 ] && [ "$code" -eq 1 ] &&
        grep -qF -- '--- FAIL: TestSEC030ReceiveBodyAdmission' "$out/contract.log" &&
        grep -qF -- 'receive admission: body reads=10 rejected=0; want 4/6' "$out/contract.log" &&
        grep -qF -- 'receive admission: body reads=50 rejected=0; want 4/46' "$out/contract.log" &&
        grep -qF -- '--- PASS: TestSEC030ReceiveKnownLengthOrders' "$out/contract.log" &&
        ! grep -qE -- '(^|[[:space:]])panic:|\[build failed\]|setup failed|\[no test files\]' "$out/contract.log"; then
        printf 'ASSERTION_RED\n' | tee "$out/red-classification.txt"
        failed=1
    else
        printf 'BLOCKED_OR_NOT_REPRODUCED\n' | tee "$out/red-classification.txt"
        failed=2
    fi
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
