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
  ranges and source-length or wrapped-key errors detected during preflight are
  rejected before charging.
  Concurrent transfers cannot each claim the same last allowance.

An allowance is charged when an authorized transfer starts, rather than when the
browser saves it. If decryption, reads or writes fail after transfer starts, or
the client cancels, the download is interrupted; a charged attempt is not refunded.
Eligible retries still use the same charged allowance. Management collection
downloads permit a two-minute retry
window in the same browser and signed-in account. Single-range continuation of
unencrypted local files and full retries of ZIP/decrypted transfers use the same
charged allowance. The window is fixed and cannot be extended by retries. Current
account, issuer permissions, file identity, expiry and deletion state are checked
again. A different browser, missing cookie or server restart requires a new grant.
The bounded retry cache can disable retries under capacity pressure. Direct API
downloads enforce the same counting policy but do not create browser retry grants.

Explicit ZIP requests retain archive format even with one member. End-to-end
encrypted files require client-side decryption and can be downloaded individually
as ciphertext; server-side ZIP requests containing them are rejected before any
allowances are charged.

At zero remaining downloads the file is unavailable for new downloads and enters
the existing cleanup process. Active transfers and their bounded retry window
protect the content from expiry cleanup. Administrator exemption does not grant
access to exhausted or deleted files. Recovering an already cleaned file is not
supported.

Turn a collected file into a share
---------------------------------

Choose **Create share** beside a received file to create an ordinary share
and copy its download link. The original collection file and its download count
stay unchanged. The new file appears in the ordinary file list, where its rules
can be edited. It initially inherits the source's remaining downloads, expiry
and password, and retains the same encryption. A protected source keeps its
password; send it to the recipient separately.

Conversion requires permission to read the source and create an upload. Deleted,
pending-deletion or exhausted files cannot be converted. Each new share counts
toward the user's and instance's logical quota even though it reuses stored
content. Deleting the original collection does not delete an independent share.
If creation fails, the page retains the source and offers a retry.

Select multiple received files, or select all within the collection, to create
shares in sequence. Successful links remain available if another file fails;
retry the failed file or run the selection again without recreating successful
shares on the current page. **Copy all successful links** includes filenames
and links, with selectable multiline text as a fallback. The ordinary file list
also supports selecting existing files and copying their current links together;
this does not create new files.

Sharing feedback distinguishes complete, partial and failed operations. Error
messages identify unavailable files, permission, network or quota failures
without exposing service diagnostics. Quota errors explain the logical charge
for each new share; the page does not estimate remaining quota from visible files.

API compatibility
-----------------

``POST /api/uploadrequest/save`` accepts the optional ``allowedDownloads`` header
(1--10). Omission means 1 on creation and preserves the policy on update. An
explicit zero is rejected. The response field is ``alloweddownloads``; zero in
an old response means legacy unlimited. Explicit download-policy overrides for
individual collection files also use the 1--10 range. SQLite upgrades to schema
18 with a zero default for existing rows; missing Redis fields likewise read as
legacy unlimited. Offline database migration preserves both old and new policies.

Collection links and download details
------------------------------------

New collection links use ``/r/<token>``. The token contains 128 random bits in
22 URL-safe characters and directly identifies the collection's upload-only
credential. The internal collection ID is omitted from the URL. Ordinary API
credentials retain their existing length. Both forms resolve live credentials,
owner permissions, collection deadlines and upload capacity on every request.

Previously issued ``/publicUpload?id=...&key=...`` links continue to work. Existing
30-character collection keys can also use the short route without regeneration.
The public upload page resolves assets and API requests against the configured
deployment path, including reverse-proxy subdirectories.

Public download pages show file size and the recorded upload time on one metadata
row, wrapping when the window is narrow. Times use the browser's local timezone
with an explicit UTC offset; missing timestamps display an unknown-time label.
This applies to both ordinary and encrypted download pages. Collection files
continue to be retrieved through the authenticated management page.

Collection intake status
------------------------

The collection list distinguishes files already received from uploads still in
progress. An expired collection shows **Expired**; a collection whose received
count reaches its limit shows **Received limit reached**. If received files plus
in-progress uploads occupy all slots, it shows **Slots temporarily full** instead.
Otherwise it shows **Accepting files**. A collection with no received files also
says so explicitly. Reaching a limit does not mean all business documents have
been submitted or approved.

These labels describe the latest available snapshot. Upload admission remains a
server-side decision and can also depend on quota and other limits. Use the
refresh control to update collection summaries without losing conversion results,
file selections or an unfinished delivery form. A failed refresh retains the
previous snapshot and its successful update time. Collection expiry stops intake
without deleting files already received.

New arrivals in the open management page
----------------------------------------

While the collection management page is visible, it checks authorized collection
summaries about once per minute. Returning to the tab checks again; the refresh
button uses the same check. The initial file set is a baseline, so opening the page
does not announce historical files as new arrivals. Notifications are local to the
current page, without cross-device unread state or offline delivery.

An arrival notice identifies the collection and lets you load its newly received
files. Only confirmed file IDs from the management API count as arrivals; upload
reservations and processing events do not. Repeated checks do not repeat the same
notice. File details are loaded on demand and appended without rebuilding existing
rows. New files start unselected and do not enter a pending conversion batch or
delivery list automatically. Existing share results and unfinished delivery forms
remain intact.

A failed check retains the previous snapshot and update time. A failed file-detail
read can be retried; an unavailable file does not prevent other new files from
being displayed. If authentication or permission is lost, checking and new-file
loading stop until the page is reopened with valid access. Newly visible
collections establish a silent baseline rather than reporting their older files
as new arrivals.
