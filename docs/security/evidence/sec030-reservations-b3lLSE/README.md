# SEC-030 durable reservation foundation evidence

Baseline 357670c16f5a0d78fe32b91efb39ee7bc74ae90c; Go1.26.0 linux/amd64, WSL Ubuntu-24.04.

Run `bash docs/security/tools/run-sec030-reservations.sh`. The disposable git archive overlays Go inputs from the named directories, normalizes CRLF to LF, and hashes the 52 canonical inputs actually compiled. Environment/generation/contracts/awsmock/noaws/integration/related-race all exited0. Those inputs match the final source. Raw logs/commands and earlier RED/failures remain in ignored .secret directories.

The tested contract is internal provider atomicity: independent SQLite/miniredis connections contend10/50 for remaining logical/count/active/staging capacity; stable replay/bind/claim/commit; retained expired capacity and cleanup acknowledgement; bounded receipts/recovery; typed exact integers/corrupt input; SQLite rollback/schema/backup; Redis WATCH invalidation/retry/target-type races/EXEC cleanup and runtime-error checking; offline migration preflight/receipts/data preservation/CLI refusal.

New API-development RED used unimplemented stubs, not a claimed production vulnerability. Intermediate assertion failures and stricter migration-preflight RED are retained in TEST-REPORT.

No HTTP upload admission is wired yet. Cleanup acknowledgement is trusted internal input; it does not prove physical file removal. Actual startup reconciliation, positive metadata mutations, policy/default limits, body streaming/anomalies and1–2GiB correctness/performance/peaks remain M3 gates. Real Redis persistence, old-binary/production restores, full-repository race and govulncheck were NOT_RUN.
