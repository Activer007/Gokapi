Delivery lists
==============

Select 2–20 of your existing ordinary files in the file workspace and choose
**Create delivery list**. Supply a title, an optional note and an optional expiry.
The list preserves the selected order and stores file IDs, without creating file
metadata, copying stored content or consuming another upload quota allowance.
End-to-end encrypted files and files held in a collection cannot be added. Turn
collected files into ordinary shares first using the existing collection action.

The delivery section lets the creator view, copy and revoke previously created
lists. A public link grants access to the list without a management login; keep
it private. If clipboard access fails, select and copy the displayed URL manually.
The MVP follows the existing desktop browser policy.

Recipients see each file's current availability and follow its existing download
page. Passwords, expiry, download allowances and server-side encryption continue
to follow the individual share's rules. Opening a list does not spend a download.
A removed, expired, exhausted or pending-deletion file is unavailable without
blocking the other items. Removed or reassigned entries use a generic unavailable
label instead of exposing another owner's metadata.

Revoking or expiring a list closes that list's entry point. It does not delete
files or revoke ordinary file links that recipients have already obtained.
To disable an individual share, use the existing file management controls.

Persistence and upgrades
------------------------

SQLite schema 19 adds the ``DeliveryLists`` table and an owner index. Redis
schema 11 adds a delivery version marker and a records hash under the configured
prefix. Records include revoked lists and remain across normal restarts. Existing
file records and stored content are unchanged. Offline database migration copies
delivery records, including their order, expiry and revocation state.

Stop the server and back up configuration, the database and stored content before
upgrading, following the existing backup procedure. Verify existing files and a
new delivery link after upgrading. A rollback must restore the matching pre-upgrade
database/configuration backup with the older binary; do not give the newer schema
to an older version. Redis durability still depends on the operator's Redis
persistence and backup configuration.

This release adds no ZIP bundles, folders, notifications, mobile access, end-to-end
encrypted lists or collection-result shortcuts.
