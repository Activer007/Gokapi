# SupaShare UI upgrade execution record

This report tracks implementation of the selected compact, top-navigation design. The existing local design plan and concept images predate this implementation and are preserved separately.

## Foundation batch — in progress

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
- Native Linux `go test ./internal/i18n ./internal/webserver -tags=test,noaws -count=1`: passed before the final JS refinements; JS refinements were regenerated and tested separately.
- Browser: real Chinese long-name upload; invalid enabled rule disclosure/focus; no-result search and clear; plain replacement labels; file rule save.
- Inspected Chinese 1440×900 and English 1280×720 screenshots with synthetic data. Local evidence: `local-ui.secret/ui-upgrade/foundation/`.

Pending: remaining viewport/keyboard/permissions/action checks, encrypted content verification, generated-resource synchronization and PR review. The full B0–B6 upgrade is not complete. No PR has been created or merged for this batch yet.
