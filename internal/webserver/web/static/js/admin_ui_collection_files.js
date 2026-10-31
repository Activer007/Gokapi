// Fetch and append only explicitly requested arrivals. Existing collection DOM
// owns M6/M7 state and must never be rebuilt when a new file is viewed.
function ensureCollectionArrivalPanel(requestId) {
    let panel = document.getElementById(`collapse-filelist-${requestId}`);
    if (panel) return panel;
    const row = document.getElementById(`row-${requestId}`);
    const skeleton = document.getElementById('collection-file-panel-template');
    if (!row || !skeleton?.content) return null;
    const fileListRow = skeleton.content.cloneNode(true).firstElementChild;
    fileListRow.id = `filelist-${requestId}`;
    fileListRow.querySelector('td').colSpan = canViewOtherRequests ? 7 : 6;
    for (const field of fileListRow.querySelectorAll('[data-panel-id]')) {
        field.id = `${field.dataset.panelId}-${requestId}`;
    }
    for (const label of fileListRow.querySelectorAll('[data-panel-for]')) {
        label.htmlFor = `${label.dataset.panelFor}-${requestId}`;
    }
    panel = fileListRow.querySelector('.collection-files');
    panel.querySelector('[data-panel-id="share-select-all"]').onchange = event => selectCollectionShares(requestId, event.target.checked);
    panel.querySelector('[data-panel-id="share-selected"]').onclick = event => shareSelectedCollectionFiles(requestId, event.currentTarget);
    panel.querySelector('[data-panel-id="share-batch-copy"]').onclick = event => copyCollectionShareLinks(requestId, event.currentTarget);
    panel.querySelector('[data-panel-id="share-delivery-create"]').onclick = () => createCollectionDelivery(requestId);
    row.after(fileListRow);
    const snapshot = collectionSummarySnapshots.get(requestId);
    if (snapshot) ensureCollectionToggle(snapshot);
    return panel;
}

function collectionArrivalFileValid(file, id, requestId) {
    return file && file.Id === id && file.IsFileRequest === true && file.FileRequestId === requestId &&
        typeof file.Name === 'string' && typeof file.Size === 'string' &&
        Number.isSafeInteger(file.UploadDate) && file.UploadDate >= 0 &&
        Number.isSafeInteger(file.DownloadsRemaining) && file.DownloadsRemaining >= 0 &&
        typeof file.UnlimitedDownloads === 'boolean' && typeof file.IsPendingDeletion === 'boolean';
}

function createCollectionArrivalFileRow(requestId, file) {
    const skeleton = document.getElementById('collection-file-row-template');
    if (!skeleton?.content) throw new Error('collection file template unavailable');
    const row = skeleton.content.cloneNode(true).firstElementChild;
    row.id = `cell-listupload-${file.Id}`;
    row.dataset.requestId = requestId;
    const field = name => row.querySelector(`[data-file-field="${name}"]`);
    const ids = {select: null, name:'cell-name', error:'share-error', result:'share-result', url:'share-url',
        copy:'share-copy', date:'cell-date-file', remaining:'cell-downloadsRemaining', expired:'cell-expired-file',
        share:'button-share', delete:'button-delete'};
    for (const [name, prefix] of Object.entries(ids)) {
        if (prefix) field(name).id = `${prefix}-${file.Id}`;
    }
    const selection = field('select');
    selection.value = file.Id;
    selection.dataset.name = file.Name;
    selection.checked = false;
    selection.setAttribute('aria-labelledby', `cell-name-${file.Id}`);
    selection.onchange = () => updateCollectionShareSelection(requestId);
    field('name').textContent = file.Name;
    field('name').onclick = () => { downloadFileWithPresign(file.Id); return false; };
    field('size').textContent = file.Size;
    field('date').textContent = formatUnixTimestamp(file.UploadDate);
    field('share-label').htmlFor = `share-url-${file.Id}`;
    const remaining = file.UnlimitedDownloads ? -1 : file.DownloadsRemaining;
    field('remaining').dataset.remaining = String(remaining);
    field('remaining').textContent = remaining < 0 ? t('common.unlimited') : String(remaining);
    field('share').onclick = event => shareCollectionFile(file.Id, event.currentTarget);
    field('download').onclick = () => downloadFileWithPresign(file.Id);
    const request = document.getElementById(`row-${requestId}`);
    const ownerID = Number(request?.dataset.ownerId);
    // FileApiOutput omits the collection file's uploader. Never treat its
    // UploaderId=0 as ownership or as a grant to delete someone else's file.
    field('delete').disabled = !(Number.isSafeInteger(ownerID) && ownerID > 0 &&
        (ownerID === currentUserId || canDeleteOtherCollectionFiles));
    field('delete').onclick = () => deleteFileFr(file.Id, requestId);
    return row;
}

function collectionArrivalAccessAllowed(requestId) {
    if (typeof collectionSummaryAuthBlocked !== 'undefined' && collectionSummaryAuthBlocked) {
        throw Object.assign(new Error('collection access unavailable'), {status:403});
    }
    // A request disappearing from a successful authorized list only revokes
    // that scope; it must not masquerade as a session-wide authentication error.
    return typeof collectionArrivalCanView !== 'function' || collectionArrivalCanView(requestId);
}

// Authentication failures discard the entire staged batch. Other failures are
// returned per item, so the arrival controller can keep them pending for retry.
async function loadCollectionArrivalFiles(requestId, ids) {
    if (typeof requestId !== 'string' || !Array.isArray(ids)) throw new Error('invalid collection file request');
    const candidates = [...new Set(ids)];
    const scopeUnavailable = () => ({shown:[], unavailable:[], failed:candidates});
    if (!collectionArrivalAccessAllowed(requestId)) return scopeUnavailable();
    const shown = [], unavailable = [], failed = [], staged = [];
    for (const [index, id] of candidates.entries()) {
        if (typeof id !== 'string' || !/^[A-Za-z0-9._-]{1,128}$/.test(id) || index >= 50) {
            failed.push(id);
            continue;
        }
        const existing = document.getElementById(`cell-listupload-${id}`);
        if (existing) {
            if (existing.dataset.requestId === requestId) shown.push(id);
            else failed.push(id);
            continue;
        }
        try {
            const file = await apiFilesListById(id);
            if (!collectionArrivalFileValid(file, id, requestId)) {
                failed.push(id);
            } else if (file.IsPendingDeletion) {
                unavailable.push(id);
            } else {
                staged.push({id, file});
            }
        } catch (failure) {
            if (failure.status === 401 || failure.status === 403) {
                if (typeof storedTokens !== 'undefined') storedTokens.delete('PERM_VIEW');
                throw failure;
            }
            (failure.status === 404 ? unavailable : failed).push(id);
        }
    }
    if (!collectionArrivalAccessAllowed(requestId)) return scopeUnavailable();
    const request = document.getElementById(`row-${requestId}`);
    if (!request) return {shown:[], unavailable, failed:[...failed, ...staged.map(item => item.id)]};
    // Construct every new row before touching the live panel. Invalid templates
    // cannot leave a partly hydrated file row in a previously working batch.
    for (const item of staged) item.row = createCollectionArrivalFileRow(requestId, item.file);
    const panel = ensureCollectionArrivalPanel(requestId);
    const list = panel?.querySelector('[data-collection-files]');
    if (!list) throw new Error('collection file list unavailable');
    if (!collectionArrivalAccessAllowed(requestId)) return scopeUnavailable();
    for (const {id, file, row} of staged) {
        const existing = document.getElementById(`cell-listupload-${id}`);
        if (!existing) {
            list.appendChild(row);
            if (!file.UnlimitedDownloads && file.DownloadsRemaining === 0) expireCollectionFile(id);
        }
        shown.push(id);
    }
    const snapshot = collectionSummarySnapshots.get(requestId);
    if (snapshot) ensureCollectionToggle(snapshot);
    prepareFileRequestRow(request);
    updateCollectionShareSelection(requestId);
    return {shown, unavailable, failed};
}
