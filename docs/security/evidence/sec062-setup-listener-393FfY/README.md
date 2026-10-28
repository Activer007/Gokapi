# SEC-062 setup listener validation

Baseline: `4ad12558ed9096fc74ebf1afdda273d5a56fc76b`.
Environment: Windows workspace, WSL Ubuntu-24.04, Go 1.26.0 linux/amd64.

Run `bash docs/security/tools/run-sec062-setup-listener.sh`. The runner archives
the baseline and overlays only the three Go inputs in `source.sha256`, hashing
the canonical LF bytes actually compiled. All nine recorded commands passed;
the three hashes were also checked against the staged Git blobs.

The occupied-port contract owns an OS-assigned listener throughout the child
process and passes its port as `GOKAPI_PORT`. Setup tests use separately held
dynamic listeners. Existing authentication, consumed-proof replay, configuration
and slow-header assertions remain. `TestSEC062LateListenerCleanup` also verifies
that cancellation before listener allocation closes the late listener and exits
Serve. The related race run passed.

`--occupied` is the CI success contract. Explicit `--red` archives the historical
baseline above, which must be available locally: environment/compile pass and
the original setup bind fails with exit 1. Raw RED and failure logs remain local.
An earlier blocker startup failure was retained as BLOCKED (exit 2), never as
product RED or PASS. The final blocker no longer needs a fixed free port.

This is test isolation, not runtime upload admission. The CI socket owner that
caused the original fixed-port failures is unknown. Real Redis restart,
production restore, full-repository race, native Windows Go, local awstest,
govulncheck (no dependency changes), and actual 1–2 GiB resource benchmarks were
not run by this runner. No deployment, schema change, or upload-limit activation.
