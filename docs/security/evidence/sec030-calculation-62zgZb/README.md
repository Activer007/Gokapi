# SEC-030 pure calculation final evidence

Baseline bfd4998fcdffbeaa7641da989cb70ac6a79ab04f (same tree as SEC-031 merge 8019c61). Go1.26.0 linux/amd64, WSL Ubuntu-24.04.

Run: `bash docs/security/tools/run-sec030-calculation.sh`. Disposable archive overlays only the two Go inputs listed in source.sha256. Environment/generation/calculation and all three full suites exited zero. Target coverage was 100% of statements.

Earlier API-development tests against unimplemented stubs exited1; minimal checked measurement/capacity functions made them pass. This RED is new API development, not production vulnerability evidence. Original red/green command logs and hashes remain ignored under sec030-calculation-agent.secret.

Each metadata row counts toward current owner and original request regardless of shared hash, display/encryption, pending deletion or expiry. Tests cover invalid metadata, no partial/aliased results, ownership changes, exact byte/file limits, negative inputs and int64 overflow. 1–2 GiB cases are arithmetic only.

No production callers, schema, quota enforcement, durable atomicity or physical-capacity behavior changes. Race/govulncheck NOT_RUN; no shared concurrent state or dependency change. Provider transactions, endpoint integration, concurrency/recovery and large streaming performance remain M3 gates.
