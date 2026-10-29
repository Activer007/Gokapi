# SupaShare UI upgrade execution record

This report tracks implementation of the selected compact, top-navigation design. The existing local design plan and concept images predate this implementation and are preserved separately.

## Foundation batch — merged

Baseline: `c434e0997e61d368dd9c4705d3ba4e10a0a01882` (2.11.0). Branch: `codex/ui-upgrade-foundation`.

Implemented:

- Scoped desktop workspace tokens, top navigation and account dropdown.
- Local decorative folder asset with generation provenance.
- Compact upload area and a disclosure containing the original rule inputs; the summary never exposes passwords.
- Upload admission validation shared by Dropzone selection, drop and paste, retaining encryption readiness restrictions.
- Search counts, clear-search action and text-safe filename icons.
- Table-managed redraw after addition/removal, plain replacement labels including filtered rows, and decrypted-name search cache refresh.

Compatibility: preserve seven table columns, hidden link nodes, file action IDs, permission branches, real data, custom resource loading order and configuration. No new framework, deployment changes or version increment.

Validation so far:

- Native Linux `go generate ./...`: passed.
- `node --test build/tests/*.test.cjs`: 29 passed, including upload rules, table redraws, replacement names and encrypted link/name handling.
- Native Linux `go test ./internal/i18n ./internal/webserver -tags=test,noaws -count=1`: passed after JS refinements.
- Mainline PR #44 was merged into the branch as `dfcb2b2`. On that native Linux source snapshot, `bash build/test-ui-local.sh` passed: generation, all 29 Node tests, both Go tag suites, targeted race checks and both production builds. Later CSS refinements, newline normalization and the immediate-restore fix were regenerated, covered by all 29 Node tests, built into the preview and browser-verified separately.
- Browser: real Chinese long-name upload; invalid enabled rule disclosure/focus; no-result search and clear; plain replacement labels; file rule save.
- Browser: restricted-user paste upload, deletion and restore, then real download with exact original Chinese/English text; keyboard disclosure and menu actions. A replacement list regression from treating DataTables nodes as an iterable was found in the browser and fixed with a regression test.
- After merging mainline, immediate Undo exposed a stale deletion-timer race. The table record is now removed before the fade; only the old DOM node is removed afterwards. Immediate restore retains exactly one file row and the downloaded file contains the original fixture text. The download event tool timed out once, but the actual saved file was inspected separately.
- Minification now normalizes CRLF to LF, matching the existing fingerprint normalization. This prevents equal fingerprints from producing different bytes inside JavaScript template literals on Windows and Linux.
- Inspected Chinese 1440×900, English 1280×720 and Chinese 1920×1080 screenshots with synthetic data. Same-size design comparison: 1586×992 at DPR 1, `docs/images/ui-upgrade/foundation-zh.jpg`. Local evidence: `local-ui.secret/ui-upgrade/foundation/`. See `design-qa.md` for corrections and measured contrast.

Limitations: 720×450 tests effective narrow desktop layout; actual browser zoom percentages were not changed. Full E2E file-content verification, exhaustive failure states and full-site acceptance remain in B4–B6. No production deployment, image publication or release occurred. Version remains 2.11.0.

Delivered as PR #45: https://github.com/Activer007/SupaShare/pull/45. Final head `41c4ef3` passed GitHub CI. After explicit user confirmation, merged into master as `b7a5f85`. CI annotations concern runner/action runtime migrations, not test failures. The full B0–B6 upgrade is not complete.


## Collections batch — verified, preparing PR

Branch: `codex/ui-upgrade-collections`, based on merged B1 `b7a5f85`.

- Collection management now uses the shared page heading, primary creation action, count, empty state and full-width table. Existing permission-dependent columns and expandable received-file lists remain.
- Public upload now separates collection notes, upload limits, branded file selection, queue and submission. Remaining capacity uses an explicit bilingual label and updates after each successful upload.
- Edited collections retain their received-file toggle and working single/ZIP downloads. Download handlers retain quoted names as data rather than parsing inline JavaScript. Failed deletion requests re-enable their buttons.
- Added accessible queue removal labels and a close action to the capacity-reached success dialog. Existing quota, reservation, upload and recovery APIs are unchanged.

Validation:

- Native Linux `go generate ./...`, all 32 Node tests, and `go test ./internal/i18n ./internal/webserver -tags=test,noaws -count=1`: passed on final B2 sources. Generated bundles/fingerprints were imported through the normal generator.
- Browser: empty state → create with two-file/1 MB limits → public paste queue → reject three files → remove excess → upload two → capacity reached → server refuses further access.
- Browser: edit a received collection to a name containing quotes and angle brackets, preserve expansion and ZIP download; inspect both ZIP entries and compare exact original UTF-8 text. Both match.
- Browser: destructive collection deletion cancelled; final preview received-file deletion followed by immediate Restore returns the server-rendered count to 1/3 and restores its download control. The first slower restore attempt in an older preview was inconclusive and is not counted as a pass.
- Browser: Chinese/English, keyboard expansion, final remaining-capacity text (3 → 2), 1280×720 English management, 1440×900 creation, 1586×992 visual comparison, 1920×1080 public completion. No captured warning/error in the inspected final public tab.
- Tracked screenshots: `docs/images/ui-upgrade/collections-zh.jpg`, `docs/images/ui-upgrade/public-upload-zh.jpg`. Additional synthetic evidence: `local-ui.secret/ui-upgrade/collections/`.

Scope limits: collection expiry/active-reservation concurrency and exhaustive network failure behavior retain existing automated coverage and remain part of B6 cross-page regression. Public error-page styling belongs to B4. No full-site acceptance or version increment is claimed.
