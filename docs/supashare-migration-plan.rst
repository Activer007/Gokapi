SupaShare migration and desktop layout
=====================================

Scope
-----

1. Rename the maintained GitHub repository and software to SupaShare, keeping
   application version 2.10.0 as the independent version-line baseline.
2. Reduce the shared footer from three rows to two when deployment text and
   help links are configured: deployment text first, links and software/version
   credit second. Omit empty rows.
3. Place the management navigation in the top header row, aligned to the right,
   with the site name on the left and compact language/account controls.

Delivery
--------

* Identity PR: module/import paths, command/build outputs, visible defaults,
  repository references and migration guidance. Rename the GitHub repository
  after validating the prepared source, then update the local remote URL.
* Layout PR: shared footer and top-right navigation, with bilingual desktop
  browser verification and existing interaction checks.

Compatibility
-------------

Keep existing data/configuration formats and database schema versions. Preserve
legacy GOKAPI_* deployment variables, authentication/session and encryption
protocol identifiers, existing CLI configuration lookup and established data
volume names. Replace the old built-in site name while retaining custom names.
Historical raw evidence and upstream attribution remain identifiable. The
previously approved forceu.github.io HTTP-download helper remains deployed at
its existing URL.

Acceptance
----------

* GitHub reports Activer007/SupaShare and git fetch/push use the new remote.
* The server, CLI/build metadata, defaults, footer and current documentation
  identify the maintained product as SupaShare.
* Native Linux local validation covers Go builds/tests, generated assets and
  existing frontend behaviors. Generated resource URLs resolve.
* Browser screenshots prove a two-row configured footer and top-right first-row
  navigation in Chinese and English, with representative navigation, language
  switching and file-action interactions working.
* Preserve the desktop-only access policy. Do not redesign for mobile devices.
