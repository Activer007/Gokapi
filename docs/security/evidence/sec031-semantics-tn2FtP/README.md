# SEC-031 final semantic characterization

Baseline: e38b03f44a183db93c7df929f87e614e21244137. Go 1.26.0 linux/amd64, WSL Ubuntu-24.04.

Run: `bash docs/security/tools/run-sec031-semantics.sh` in a disposable source archive. The runner overlays the one input listed in source.sha256. Generation, targeted SQLite/miniredis API characterization and all three full suites exited zero. Raw logs remain ignored in the matching .secret directory.

The target proves current per-file MaxSize behavior (two distinct 768 KiB files at MaxSize=1 MiB), exact completed payloads, sequential count refusal, expiry, known disk-floor refusal and current body-overhead/unknown-length refusals. It does not prove atomic quota admission, real Redis, cleanup/restart recovery or 1–2 GiB streaming performance. No runtime/schema/data semantics changed.

Earlier fixture and fixed-port failures are preserved in TEST-REPORT.md. Relevant race and govulncheck were NOT_RUN for this test/document-only PR. M3 remains active.
