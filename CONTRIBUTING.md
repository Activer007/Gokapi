# Contributing to SupaShare

First off, thanks for taking the time to contribute! SupaShare is built to be a lightweight, secure way to share files, and community help is what keeps this project running.

## Security First
If you find a security vulnerability, please **do not** open a public issue. Instead, report it at https://github.com/Activer007/SupaShare/security to allow for a responsible disclosure.

## Getting Started
1. **Fork** the repository and clone it locally.
2. Ensure you have the minimum Go version specified in `go.mod` (**1.26.10** for this reviewed checkout) installed.
3. Install dependencies: `go mod download`.
4. Create a new branch for your fix or feature: `git checkout -b feat/new-pr-name`.

## Coding Standards
- **Style:** Run `go fmt ./...` before committing. We follow standard Go idioms.
- **Tests:** If you add a feature, please add a corresponding test in the relevant `_test.go` file.
- **Documentation:** Update the `README.md` or `/docs` if you change application behaviours or procedures.

## Submitting a PR
- Ensure your PR description follows our [PR Template](.github/PULL_REQUEST_TEMPLATE.md).
- Keep PRs focused. If you have two unrelated fixes, please open two PRs.
- Describe the problem, minimal change, checks actually run and compatibility impact.
- GitHub Actions in this repository are currently disabled by the maintainer. Use local checks and code review; do not enable or dispatch CI as part of a PR.

## Choose checks by risk

- **S — small bugs or documentation:** make the focused change, keep a useful regression for changed behavior, and run relevant checks. For documentation, check links and rendering rather than the application test suite.
- **M — normal features:** verify the core flow and realistic failure boundaries.
- **H — security, consistency, encryption, concurrency or migrations:** verify the specific invariants and failure paths at risk; use race or recovery checks where those risks require them.

Reuse existing helpers and valid results for unchanged code. The broader `build/test-ui-local.sh` workflow is available when the change warrants it; full suites, browser matrices, RED/GREEN setups and separate reports are not universal PR requirements. Stop when the defined requirements pass.

## Community & Communication
If you're unsure about an implementation detail, feel free to open a "Draft PR" or start a GitHub Discussion to get feedback early!
