# SEC-030 final receiver code evidence

Tested code: `ff7497beb4f173f81cbf90695e7b6544d393f53b` (2026-10-29).
[PR CI 40751200410](https://github.com/Activer007/Gokapi/actions/runs/40751200410)
and [manual CI 40751191903](https://github.com/Activer007/Gokapi/actions/runs/40751191903)
succeeded on the own repository. All four suites, generation, occupied-port
contract and related race passed. The manual run also passed real HTTP
file-first 1 GiB plaintext and 2 GiB server-encrypted uploads plus read-back SHA256.
The runner used Go 1.26.0 linux/amd64 on GitHub ubuntu-latest.

`source.sha256` contains 43 actual runner hashes, each checked against the tested
commit's Git blobs. `large-metrics.txt` contains measured numeric peaks,
throughput and plaintext SHA256, with no keys or sharing URLs. Apparent/allocated
peaks sample test-owned disk files every 20 ms; heap/RSS are sampled process peaks,
not proof of a universal maximum. Throughput describes this runner and path only.

This final code includes the rejected-body HTTP/1 regression, sticky uncertain
Close and quoted-printable compatibility contracts. The earlier f563d9f evidence
remains a historical checkpoint. A following documentation-only commit does not
change the Go, runner or workflow bytes; its PR CI is checked separately.

Local WSL's read-only failure and subsequent E_UNEXPECTED remain BLOCKED/NOT_RUN
in TEST-REPORT. These remote passes do not replace that failed local attempt.
Durable upload-ID admission, cumulative user/request quotas, persistent chunks,
async completion, real S3 and full M3 recovery are still open. The Go toolchain's
published advisory gate is separate; these tests do not establish a safe release.
