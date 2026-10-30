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


## Collections batch — merged

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

Delivered as PR #46: https://github.com/Activer007/SupaShare/pull/46. Head `f8563e1` passed GitHub CI. After separate explicit user authorization, merged as `ad82481`.

## Administration batch — merged

Branch: `codex/ui-upgrade-management`, based on B2 `f8563e1`.

- User and API pages adopt the shared heading, primary action and wide table. Existing permissions, key masking, credential delivery and DOM hooks remain intact. API empty state follows the actual table rows.
- Credential dialogs use a scrollable body and reachable footer at 1280×720. Credentials remain confined to the existing dialog lifecycle and are cleared on close.
- Status uses six resource summaries on wide desktops, three per row on smaller desktops, labelled log controls and a readable resizable log viewer.
- Log deletion and traffic reset now use explicit Bootstrap confirmation with the shared pending/error handling and focus return. Cancelling clears the selection without sending the destructive request.

Validation on 2026-10-30:

- Native Linux generation, all 35 Node tests, and `go test ./internal/i18n ./internal/webserver -tags=test,noaws -count=1` passed. The only subsequent template change removes trailing whitespace. Generated assets were imported from this final snapshot.
- Browser: create a synthetic user, copy delivery text, close and inspect that credential field lengths are zero; duplicate username rejection retains input; permission save and cancel; create/copy/rename API key and cancel deletion.
- Browser: filter authentication logs; cancel both new status dialogs and verify no destructive request result. An earlier browser test of the old native confirm unexpectedly deleted only synthetic local logs; this prompted replacement by explicit page dialogs and regression tests. No production data was involved.
- Chinese users/API/status compared with the selected concept at CSS 1586×992, DPR 1. English status and credential delivery inspected at 1280×720; credential Close bounds 631–669px remain inside the viewport. Status full-page captures include normal vertical scrolling. No warning/error captured in the final status tab.
- Screenshots: `docs/images/ui-upgrade/users-zh.jpg`, `api-zh.jpg`, `status-zh.jpg`; additional synthetic evidence in `local-ui.secret/ui-upgrade/management/`.

Limitations: the generated-password browser login attempt was inconclusive and is not accepted as first-login coverage; B4 will use a known disposable forced-reset fixture to inspect that gate. API empty-state browser coverage and full-site regression remain pending. Version remains 2.11.0.

Delivered as PR #47: https://github.com/Activer007/SupaShare/pull/47. Head `96418bd` passed CI, was retargeted to master after #46, and was merged with explicit user authorization as `ef690e8`.

## Public pages and encryption — merged

- Login, password change/recovery, download/password challenge, errors and encryption setup share a bounded charcoal panel, labelled fields, clear actions and the common public header/footer. Collection pages reuse the same shell and retain their 960px content width. Old public-only overrides were removed.
- Password challenge now has the submit-button ID its script accesses. Mismatched password submission displays feedback and focuses confirmation. Existing POST fields, CSRF, password/session rules, redirects and custom-resource ordering remain.
- Normal downloads use native file links. WASM initialization awaits asynchronous failures; encrypted transfer failures handle empty rejection reasons, abort output and clear completion feedback. Download errors explain recovery through the original share link. The existing external HTTP helper address is unchanged.

Validation on 2026-10-30:

- Native Linux generation, 40 Node tests and i18n/webserver Go tests passed. The final native-link template refinement passed all 40 Node tests plus `go test ./internal/webserver -tags=test,noaws -run 'TestUI|TestSEC012PasswordChangeRevokesSessions' -count=1`. The final collection-width-only refinement was regenerated and visually verified.
- Browser: incorrect login, preserved username, known forced-reset fixture redirected to password change; incorrect/correct download password; missing-key error; existing E2E key unlock; real E2E paste upload and decrypted dynamic filename/link. Password mutation itself is covered by Go/Node tests, not claimed as a browser submission.
- Normal downloaded file: all 3,430 bytes match the original. Chrome WASM + local service-worker download: all 221,184 bytes match, SHA-256 `e6d7e87d2542d0864a03b5a06bbd2768b3aa5739175f3b76ce8ee0ae1a1d548d`. Wrong cipher shows authentication failure, and reopening the original link recovers an identical file. Actual browser E2E upload/share/download round trip matches all 54 original UTF-8 bytes.
- The in-app browser blocked ordinary scripted download navigation and aborted stream saving; normal native-link saving and Chrome were used for final file checks. Chrome is an allowed Product Design fallback when the in-app browser is blocked. The local-worker test uses loopback's secure context and a disposable fixture with an HTTPS public URL loaded into configuration; it is not an actual TLS/deployment test. Earlier previews that changed the URL without reloading still selected the external helper and are not counted as local-worker checks.
- Actual full comparison captures: Chinese login/download/encryption unlock and public collection shell at CSS 1586×992, DPR 1; English first-login page at 1280×720. Missing-key page was inspected at 1280×720. Chrome completion DOM and files passed; its viewport screenshot scaling limitation is not counted as a pixel-matched comparison. Final successful encrypted download had no captured warning/error.
- Screenshots: `docs/images/ui-upgrade/login-zh.jpg`, `download-zh.jpg`, `encryption-unlock-zh.jpg`. Detailed synthetic evidence and byte-verification scripts remain under `local-ui.secret/ui-upgrade/`.

Limits: no production HTTPS/ServerURL/Docker changes, no external-helper migration, no version increment. External helper availability, initial key-generation backup flow, API empty state and remaining global checks continue in B5–B6. A Chrome clipboard probe did not observe the page's write through the automation clipboard; only the success feedback and correct decrypted link binding are accepted from that check.

Delivered as PR #49: https://github.com/Activer007/SupaShare/pull/49. Head `b248135` passed CI on retry and was merged after explicit user authorization as `4baeade`. The first CI attempt failed the Redis branch of `TestSEC030WholeReservedGCRechecksCommittedReference`; the unchanged test passed 30 consecutive native Linux repetitions, and the full CI retry passed. No test assertion or cleanup behavior was weakened.


## Installation wizard — verified, preparing PR

- The separately embedded setup page now uses charcoal surfaces, blue actions, persistent field labels, a scrollable card and a reachable footer. The existing local step list remains; no application-wide sidebar or new framework was introduced. Language controls remain visible across steps.
- Keyboard activation works for previously visited steps. Step changes focus the heading; invalid submissions focus the first failed field. Validation uses Bootstrap's existing lifecycle but renders text directly below its field, fixing clipped right-side popovers at 1280×720. Recovery clears the field's error association.
- All 56 serialized field names and ordering, authentication/storage/encryption branches, initial defaults and reconfiguration values remain intact. The final Continue control is a real button and uses textContent for its waiting state. Help points to SupaShare's setup documentation.

Validation on 2026-10-30:

- Synchronized master `4baeade`, including independent deletion/reclamation and WASM cancellation fixes from the security work. That mainline includes a bounded retry for definite Redis cleanup WATCH aborts related to the earlier CI failure; this UI batch does not modify cleanup code.
- Native Linux snapshot ran `bash build/test-ui-local.sh` successfully: generation, 40 Node tests, both complete Go tag suites, targeted race tests and default/noaws production builds. Two subsequent presentation-only refinements (heading level and duplicate help-label removal) passed all 40 Node tests and setup/i18n UI/reconfiguration tests.
- Both setup translation dictionaries resolve every template key. The production embed includes the standalone stylesheet. The full setup integration tests exercise authenticated initial installation and reconfiguration; no credential mutation was submitted through the browser.
- Browser: Chinese/English initial pages, SQLite/Redis, invalid URL and recovery, persistent credential labels, keyboard return to a visited step, Redis field/footer reachability. Reconfiguration retains its site/database/authentication values and reaches the final Submit step; S3 fields and Test Configuration are reachable without invoking external services.
- Actual viewports: 1280×720 (Next bottom 633px; footer bottom 651px), 1440×900 (English long form), 1920×1080 (reconfiguration finish), and 1586×992 at DPR 1 for comparison with the selected concept. No captured warning/error in the inspected final setup tab. Browser zoom percentage is still pending; changing the effective viewport is not claimed as browser zoom.
- Browser authentication support and process-lifecycle issues interrupted the first preview attempts. Final visual checks use actual authenticated Go-rendered initial/reconfiguration HTML served by an ignored, read-only loopback preview with the real setup CSS/JS. It cannot submit configuration. This verifies rendering and client interaction, while the native authenticated integration tests verify commits; it is not a claim of browser-driven installation.
- Tracked screenshot: `docs/images/ui-upgrade/setup-zh.jpg`. Additional error/long-form/finish evidence is under `local-ui.secret/ui-upgrade/setup/`.

Remaining: global zoom/customization checks, API empty state, initial E2E backup flow, latest-mainline encrypted download check, final documentation/version and B6 acceptance. Version remains 2.11.0.

Iteration policy (user request, 2026-10-30): reuse passed checks for unchanged code; run focused checks only for affected behavior or new failures. B5's completed full suite is retained without another local full run before its PR. The final review corrected only an OIDC documentation link and prose punctuation. Consolidate remaining cross-page checks into B6, then run the complete local suite once against the final delivery snapshot. Repository-required CI remains enabled.
