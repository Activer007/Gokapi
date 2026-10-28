# SEC-062 reviewed compiler evidence

Tested code: `58d2cb54acb64ce8f5b9f3bda41d75cb38e620d5`.
[PR CI](https://github.com/Activer007/Gokapi/actions/runs/40763628844),
[push CI](https://github.com/Activer007/Gokapi/actions/runs/40763619586) and
[extended manual CI](https://github.com/Activer007/Gokapi/actions/runs/40763621734)
all succeeded. Manual job: 123081807272.

Current compiler Go 1.26.10 linux/amd64; govulncheck v1.9.0;
DB updated 2026-10-27 21:16:18 UTC. Default/noaws/WASM source scans and
minifier/server/CLI binary scans returned 0. Generation, four full suites,
related race and bounded Range compatibility passed. Source scans still report
module-only OpenPGP and legacy AWS S3 Crypto advisories without reachable affected
calls. This does not mean the dependencies contain no advisories or prove absence
of reflection/dynamic behavior that static analysis cannot see.

Historical cb5868c with actual Go 1.26.0 returned scanner exit 3 and included
published GO-2026-6609. The wrapper expects RED, not safety of the old source.
No high-resource exploit load was used.

Three temporary runner-local Docker images built successfully. Builder go version
and server/CLI BuildInfo confirmed 1.26.10. No image was pushed or mounted user
data. Docker binaries received BuildInfo checks; binary vulnerability scans used
the separate CGO_ENABLED=0 build artifacts. Image OS scans, immutable image/action
pinning and full cross builds remain open.

Real HTTP file-first 1 GiB plaintext and 2 GiB server-encrypted uploads passed
read-back SHA256 under the patched compiler. Metrics describe this runner/path;
sampled disk/heap/RSS peaks do not establish universal bounds.

44 actual compiled Go hashes and 11 current input/harness hashes were checked
against this commit's Git blobs. Seven historical input hashes match cb5868c;
the historical harness is current. Generated artifact hashes are recorded from
the runner, not compared with Git or locally rebuilt binaries. Documentation-only
follow-ups preserve Go/runner/workflow bytes and get independent PR CI. Previous
failures/cancellations remain recorded. Local Go remains NOT_RUN/BLOCKED;
M3 remains open.
