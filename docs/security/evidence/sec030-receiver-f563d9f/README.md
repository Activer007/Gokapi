# SEC-030 receiver checkpoint

Exact tested code: `f563d9f5bcb4fea30cfb4c5fc1ccb0b22a284313`.
PR CI 40746205711 and manual CI 40746246616 both succeeded on the own repository.
The manual run included all four suites, receiver race, and real HTTP file-first
uploads/read-back of 1 GiB plaintext and 2 GiB server-encrypted data.

`source.sha256` was extracted from the actual runner log and every digest was
verified against that commit's Git blobs. `large-metrics.txt` contains only
numeric measurements and plaintext SHA256 checksums; no keys or sharing URLs.
The runner had about 84 GiB of available disk before the large test.

This is a checkpoint, not a claim about later untested edits. A subsequent
rejected-body transport guard and its real HTTP regression require their own
current PR checks. Refer to PR #19 for those gates. The data streaming algorithm
and benchmark fixtures from this checkpoint are preserved.

Local WSL had already become read-only and then failed to start; that failed
2 GiB attempt remains recorded in TEST-REPORT. It is not replaced by a fictitious
local pass. Receiver limits do not prove durable chunk/cumulative quotas,
asynchronous completion or all M3 recovery requirements.
