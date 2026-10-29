:orphan:

User onboarding credentials
===========================

Development baseline: ``0a6fcf989caa0a16cffac8f1f82e2e037322d217`` from
``Activer007/SupaShare`` (pulled before development, 2026-10-29).
Feature branch: ``codex/user-onboarding-credentials``.

Goal
----

An administrator creates an internal-login user and immediately receives the
username, generated temporary password and correct login URL, ready to copy
individually or together. The user signs in with that temporary password and
must choose a new password in the existing first-login flow. Regenerating a
password uses the same credential handoff.

Implementation tasks
--------------------

1. Generate a cryptographically random temporary password during internal-login
   account creation. Persist only its hash and the required-password-change flag
   together in the initial user save. Preserve external identity provisioning.
2. Extend only authorized creation and password-generation responses with the
   temporary password and the login URL derived from the configured public URL.
   Preserve existing response fields, authorization and generic user serialization.
3. Keep the successful creation dialog open and show a reusable credential
   handoff with copy-all, individual copy, password visibility and first-login
   guidance. Reuse it for generated password resets.
4. Support clipboard refusal with selectable multiline text. Preserve input and
   retry controls on creation errors. Clear temporary credentials when the
   dialog closes; do not retain them in browser storage or logs.
5. Update English/Chinese text, API reference and user instructions. Regenerate
   the minified/versioned assets through the normal Go generation pipeline.
6. Run backend contracts, Node behavior tests, native Linux noaws/awsmock tests,
   focused race checks and builds. Exercise the real browser create/copy/reset
   and first-login password-change workflow against isolated SQLite/data.

Acceptance evidence
-------------------

* New internal users can authenticate with the returned password. The database
  contains a hash and the reset flag from the initial insert, never plaintext.
* Duplicate, invalid and unauthorized requests do not disclose credentials.
  Ordinary user listings and external-auth provisioning remain compatible.
* Creation and regeneration return the normalized username and configured
  public login URL, including a deployment subpath. Secret responses are not
  cached. Force-change-only reset preserves its existing password behavior.
* Both languages present the complete delivery message with a first-login
  reminder. Copy-all and individual copy work, and clipboard refusal leaves a
  usable manual selection without repeating account creation.
* Closing and reopening the dialogs removes the previous plaintext password
  and fallback text. No plaintext is saved to localStorage/sessionStorage.
* A real browser signs in as the new user, reaches the change-password page,
  changes the password, signs in with the new password, and cannot reuse the
  old temporary password. Regenerated credentials also support this flow.
* Generated assets are in sync; test/build/QA evidence identifies the exact
  source, environment and any limitations. Test data is independent of the
  server deployment.

Compatibility
-------------

The shared user-creation helper also provisions OIDC/header users. Its behavior
is retained; temporary passwords apply to administrator-created accounts under
internal authentication. Existing credentials and account records are unchanged
until an administrator explicitly regenerates a password. No database schema
or stored password format migration is needed.

The existing first-login restriction redirects browser UI navigation to the
password-change screen; this feature does not redefine API authorization.
Only the credential-returning response and its current dialog contain the
plaintext password. Once that dialog is closed, an administrator who loses it
must generate another password.

Status
------

All implementation tasks and acceptance checks above completed on 2026-10-29.

* Backend: dedicated internal-account creation, one-save password hash/reset
  flag, credential-only response fields, cache controls, configured login URL,
  single-process duplicate creation protection and reset sharing are implemented.
* Interface: shared creation/reset delivery panel, hidden password, copy-all and
  individual copy, selectable multiline fallback, retry and close cleanup are
  implemented in English and Chinese. Rendered QA also corrected helper-text
  contrast on the existing dark modal.
* Go 1.26.10 on Ubuntu 24.04, ordinary user and a native Linux snapshot:
  generation, all packages with ``test,noaws``, all packages with
  ``test,awsmock``, focused users/API/webserver/i18n/model race checks, and
  default/noaws production builds passed. The 24 Node behavior tests passed.
* After the template-only contrast correction, generation, focused web UI tests
  and the production build passed again. Generated frontend/API resources were
  synchronized back to the branch.
* Real Chrome through Playwright, local isolated SQLite/config/data, at
  ``1440x1100`` and ``420x900`` with a desktop User-Agent: creation failure and
  retry, successful credential handoff, all copy buttons, denied clipboard and
  manual selection, successful copy retry, visibility toggle, close/reopen
  cleanup, bilingual panels and narrow-window layout passed.
* The real new user authenticated with the temporary password, was redirected
  to change their password, signed in with the new password, and could not reuse
  the temporary password. Administrator regeneration revoked the active user
  session and required another password change. The created test user was
  removed through the normal API.
* No unexplained browser runtime/console errors were observed. The deliberately
  injected creation failure produced expected error logging and was recorded
  separately. Clipboard success/refusal used a controlled browser API test double
  to avoid changing the user's operating-system clipboard.

Browser plugin availability was classified as absent; the installed Playwright
runtime and Chrome were used without adding browser dependencies. Evidence and
screenshots are kept outside the repository in the task's artifact workspace.
This delivery is a tested development branch; server publication is separate.
