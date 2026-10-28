#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in ""|--target|--red|--occupied) ;; *) exit 2 ;; esac
repo=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mktemp -d "$repo/docs/security/evidence/sec062-setup-listener-XXXXXX.secret")
work=$(mktemp -d /tmp/gokapi-sec062-setup-listener-XXXXXX)
base=$(git -C "$repo" rev-parse HEAD)
if [ "${1:-}" = "--red" ]; then
    base=4ad12558ed9096fc74ebf1afdda273d5a56fc76b
fi
if ! git -C "$repo" cat-file -e "$base^{commit}"; then
    printf 'BLOCKED: baseline commit is unavailable locally: %s\n' "$base" >&2
    exit 2
fi
git -C "$repo" -c core.autocrlf=false archive "$base" | tar -x -C "$work"
files=(internal/configuration/setup/Setup.go internal/configuration/setup/Setup_test.go internal/configuration/setup/bootstrap_test.go)
if [ "${1:-}" != "--red" ]; then
    for file in "${files[@]}"; do sed 's/\r$//' "$repo/$file" > "$work/$file"; done
fi
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
cat > "$work/occupied-port.py" <<'PY'
import os
import socket
import subprocess
import sys

# Own an OS-assigned port for the entire real setup test process, and configure
# it as the product port. The blocker itself must not depend on a fixed free port.
# A blocker startup error is a harness failure (exit 2), never a product RED.
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as blocker:
    try:
        blocker.bind(("127.0.0.1", 0))
        blocker.listen()
    except OSError as exc:
        print(f"BLOCKED: could not own configured test port: {exc}", flush=True)
        sys.exit(2)
    port = blocker.getsockname()[1]
    print(f"Harness owns configured port 127.0.0.1:{port}; running setup entry points", flush=True)
    child_env = dict(os.environ, GOKAPI_PORT=str(port))
    try:
        result = subprocess.run(
            [sys.argv[1], "-test.run=^(TestInitialSetup|TestRunConfigModification|TestIntegration|TestIsErrorAddressAlreadyInUse|TestSEC062.*)$", "-test.v", "-test.timeout=120s"],
            cwd=os.path.join(os.getcwd(), "internal/configuration/setup"),
            env=child_env,
            timeout=130,
            check=False,
        )
    except subprocess.TimeoutExpired:
        print("BLOCKED: setup test process exceeded harness deadline", flush=True)
        sys.exit(2)
    sys.exit(result.returncode)
PY
run environment go version
run compile go test -c -tags=test,noaws -o "$work/setup.test" ./internal/configuration/setup
if [ -x "$work/setup.test" ]; then
    run occupied-port python3 "$work/occupied-port.py" "$work/setup.test"
fi
if [ "${1:-}" != "--red" ] && [ "${1:-}" != "--occupied" ]; then
    run generate go generate ./...
    run setup go test ./internal/configuration/setup -count=1 -tags=test,noaws
    run race go test -race ./internal/configuration/setup -count=1 -tags=test,noaws -run '^(TestInitialSetup|TestRunConfigModification|TestIntegration|TestIsErrorAddressAlreadyInUse|TestSEC062.*)$'
fi
if [ "${1:-}" = "" ]; then
    run awsmock go test ./... -parallel 8 -count=1 -tags=test,awsmock
    run noaws go test ./... -parallel 8 -count=1 -tags=test,noaws
    run integration go test ./... -count=1 -tags=test,noaws,integration
fi
printf 'Evidence: %s\n' "$out"
exit "$failed"
