File collection policies
========================

New collections default to a deadline five days from creation and an allowance
of one download for each collected file. The allowance can be set to an integer
from 1 through 10. The deadline can be changed or disabled. Reopening the creation
dialog always starts with these defaults; editing preserves the saved policy.

The collection deadline stops new uploads, including files that finish after the
deadline. It does not expire files already collected. File lifetime and storage
cleanup remain separate. The maximum file count describes files currently held,
not the cumulative number ever submitted; cleanup releases a file's slot.

The allowance is copied into each file when its upload is completed. Changing a
collection affects subsequent uploads, not existing files. Existing collections
and files retain their previous policy, including legacy unlimited downloads.
The edit dialog can preserve that legacy setting or opt into the 1--10 range.

Management downloads
--------------------

The actual downloader's current server-side role determines counting, regardless
of who created the collection or issued a temporary download link:

* Ordinary users spend one allowance per collected file. A ZIP spends one for
  each collection file included. Ordinary management files keep their existing
  counting behavior.
* Administrators and super administrators do not spend an allowance or increase
  the file's counted downloads. Downloads are still logged, and normal file
  permissions and validity checks apply.
* Pages, link generation and HEAD probes do not spend an allowance. Invalid
  ranges and detectable local read/decryption errors are rejected before charging.
  Concurrent transfers cannot each claim the same last allowance.

An allowance is charged when an authorized transfer starts, rather than when the
browser saves it. Management collection downloads permit a two-minute retry
window in the same browser and signed-in account. Single-range continuation of
unencrypted local files and full retries of ZIP/decrypted transfers use the same
charged allowance. The window is fixed and cannot be extended by retries. Current
account, issuer permissions, file identity, expiry and deletion state are checked
again. A different browser, missing cookie or server restart requires a new grant.
The bounded retry cache can disable retries under capacity pressure. Direct API
downloads enforce the same counting policy but do not create browser retry grants.

At zero remaining downloads the file is unavailable for new downloads and enters
the existing cleanup process. Active transfers and their bounded retry window
protect the content from expiry cleanup. Administrator exemption does not grant
access to exhausted or deleted files. Recovering an already cleaned file is not
supported.

API compatibility
-----------------

``POST /api/uploadrequest/save`` accepts the optional ``allowedDownloads`` header
(1--10). Omission means 1 on creation and preserves the policy on update. An
explicit zero is rejected. The response field is ``alloweddownloads``; zero in
an old response means legacy unlimited. Explicit download-policy overrides for
individual collection files also use the 1--10 range. SQLite upgrades to schema
18 with a zero default for existing rows; missing Redis fields likewise read as
legacy unlimited. Offline database migration preserves both old and new policies.
