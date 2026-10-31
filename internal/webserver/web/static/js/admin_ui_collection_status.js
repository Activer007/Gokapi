// Presentation snapshots only. The upload provider still decides admission.
const collectionSummarySnapshots = new Map();
let collectionClockOffset = 0;
let collectionStatusTimer;
let collectionSummaryRefreshPending = false;
let collectionSummaryRefreshPromise = null;
let collectionSummaryRefreshManual = false;
let collectionSummaryAuthBlocked = false;

function collectionServerNow() {
    return Math.floor(Date.now() / 1000 + collectionClockOffset);
}

function setCollectionServerTime(serverTime) {
    if (Number.isFinite(serverTime) && serverTime > 0) collectionClockOffset = serverTime - Date.now() / 1000;
}

function collectionSummarySnapshot(input) {
    if (!input || typeof input.id !== 'string' || !input.id) return null;
    const snapshot = {id: input.id};
    for (const key of ['uploadedfiles', 'reserveduploads', 'maxfiles', 'totalfilesize', 'lastupload', 'expiry']) {
        if (input[key] === undefined || input[key] === '') return null;
        const value = Number(input[key]);
        if (!Number.isSafeInteger(value) || value < 0) return null;
        snapshot[key] = value;
    }
    const ids = input.fileidlist ?? (typeof input.fileids === 'string' ? input.fileids.split(',').filter(Boolean) : null);
    if (!Array.isArray(ids) || ids.some(id => typeof id !== 'string' || !id)) return null;
    snapshot.fileidlist = Object.freeze([...ids]);
    return Object.freeze(snapshot);
}

function deriveCollectionStatus(snapshot, now) {
    if (snapshot.expiry > 0 && now > snapshot.expiry) return 'expired';
    if (snapshot.maxfiles > 0 && snapshot.uploadedfiles >= snapshot.maxfiles) return 'full';
    if (snapshot.maxfiles > 0 && snapshot.uploadedfiles + snapshot.reserveduploads >= snapshot.maxfiles) return 'reserved';
    return 'accepting';
}

function renderCollectionStatus(snapshot) {
    const row = document.getElementById(`row-${snapshot.id}`);
    const state = row?.dataset.collectionAccess === 'unavailable' ? 'unavailable' : deriveCollectionStatus(snapshot, collectionServerNow());
    const badge = document.getElementById(`collection-status-${snapshot.id}`);
    if (badge) {
        const label = t(`collection.status.${state}`);
        const className = `badge collection-status collection-status-${state}`;
        if (badge.textContent !== label) badge.textContent = label;
        if (badge.className !== className) badge.className = className;
    }
    const expiry = document.getElementById(`cell-expiry-${snapshot.id}`);
    if (expiry) {
        const label = snapshot.expiry === 0 ? t('common.never') :
            (collectionServerNow() > snapshot.expiry ? t('common.expired') : formatUnixTimestamp(snapshot.expiry));
        if (expiry.textContent !== label) expiry.textContent = label;
    }
}

function ensureCollectionToggle(snapshot) {
    const cell = document.getElementById(`collection-count-${snapshot.id}`);
    const panel = document.getElementById(`collapse-filelist-${snapshot.id}`);
    if (!cell || !panel || snapshot.uploadedfiles === 0 || cell.querySelector('.collapse-toggle')) return;
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'btn btn-sm btn-link text-light p-0 collapse-toggle';
    toggle.setAttribute('data-bs-toggle', 'collapse');
    toggle.setAttribute('data-bs-target', `#collapse-filelist-${snapshot.id}`);
    toggle.setAttribute('aria-expanded', String(panel.classList.contains('show')));
    toggle.setAttribute('aria-controls', `collapse-filelist-${snapshot.id}`);
    toggle.setAttribute('aria-label', t('request.expand'));
    const icon = document.createElement('i');
    icon.className = 'bi bi-chevron-down';
    icon.setAttribute('aria-hidden', 'true');
    toggle.appendChild(icon);
    cell.appendChild(toggle);
}

// Never replace the row or its collapse panel: M6/M7 keep live batch results,
// choices and pending conversions on those nodes.
function updateCollectionSummary(input) {
    const snapshot = collectionSummarySnapshot(input);
    const row = snapshot && document.getElementById(`row-${snapshot.id}`);
    if (!row) return false;
    row.setAttribute('data-collection-summary', '');
    Object.assign(row.dataset, {
        id: snapshot.id, uploadedfiles: String(snapshot.uploadedfiles), reserveduploads: String(snapshot.reserveduploads),
        maxfiles: String(snapshot.maxfiles), totalfilesize: String(snapshot.totalfilesize), lastupload: String(snapshot.lastupload),
        expiry: String(snapshot.expiry), fileids: snapshot.fileidlist.join(',')
    });
    collectionSummarySnapshots.set(snapshot.id, snapshot);
    const count = document.getElementById(`totalFiles-fr-${snapshot.id}`);
    if (count) count.textContent = String(snapshot.uploadedfiles);
    const maximum = document.getElementById(`collection-max-${snapshot.id}`);
    if (maximum) maximum.textContent = snapshot.maxfiles ? ` / ${snapshot.maxfiles}` : '';
    const inflight = document.getElementById(`collection-inflight-${snapshot.id}`);
    if (inflight) {
        inflight.hidden = snapshot.reserveduploads === 0;
        inflight.textContent = t('collection.inflight', {count: snapshot.reserveduploads});
    }
    const empty = document.getElementById(`collection-empty-${snapshot.id}`);
    if (empty) empty.hidden = snapshot.uploadedfiles !== 0;
    const size = document.getElementById(`collection-size-${snapshot.id}`);
    if (size) size.textContent = getReadableSize(snapshot.totalfilesize);
    const last = document.getElementById(`cell-lastupdate-${snapshot.id}`);
    if (last) last.textContent = formatTimestampWithNegative(snapshot.lastupload, t('common.none'));
    renderCollectionStatus(snapshot);
    ensureCollectionToggle(snapshot);
    return true;
}

function recordCollectionUpdatedTime() {
    const updated = document.getElementById('collectionUpdatedAt');
    if (!updated) return;
    const date = new Date(collectionServerNow() * 1000);
    updated.dateTime = date.toISOString();
    updated.textContent = t('collection.updatedAt', {time: date.toLocaleString()});
}

function initCollectionStatus(serverTime) {
    setCollectionServerTime(Number(serverTime));
    document.querySelectorAll('[data-collection-summary]').forEach(row => updateCollectionSummary(row.dataset));
    recordCollectionUpdatedTime();
    clearInterval(collectionStatusTimer);
    collectionStatusTimer = setInterval(() => {
        for (const snapshot of collectionSummarySnapshots.values()) {
            if (document.getElementById(`row-${snapshot.id}`)) renderCollectionStatus(snapshot);
            else collectionSummarySnapshots.delete(snapshot.id);
        }
    }, 1000);
}

function blockCollectionSummaryAccess() {
    collectionSummaryAuthBlocked = true;
    if (typeof storedTokens !== 'undefined') {
        storedTokens.delete('PERM_MANAGE_FILE_REQUESTS');
        storedTokens.delete('PERM_VIEW');
    }
    const button = document.getElementById('collectionRefresh');
    if (button) button.disabled = true;
    const status = document.getElementById('collectionRefreshStatus');
    if (status) status.textContent = t('collection.authRequired');
    if (typeof stopCollectionArrivalPolling === 'function') stopCollectionArrivalPolling();
    if (typeof collectionArrivalStates !== 'undefined') {
        for (const [id, state] of collectionArrivalStates) {
            state.active = false;
            setCollectionArrivalAccess(id, false);
            renderCollectionArrivalNotice(id);
        }
    }
}

function refreshCollectionSummaries({silent = false} = {}) {
    if (collectionSummaryAuthBlocked) return Promise.resolve(false);
    if (collectionSummaryRefreshPromise) {
        if (!silent) {
            collectionSummaryRefreshManual = true;
            const status = document.getElementById('collectionRefreshStatus');
            if (status && status.textContent !== t('common.loading')) status.textContent = t('common.loading');
        }
        return collectionSummaryRefreshPromise;
    }
    collectionSummaryRefreshManual = !silent;
    const pending = performCollectionSummaryRefresh();
    collectionSummaryRefreshPromise = pending;
    const clear = () => { if (collectionSummaryRefreshPromise === pending) collectionSummaryRefreshPromise = null; };
    pending.then(clear, clear);
    return pending;
}

async function performCollectionSummaryRefresh() {
    collectionSummaryRefreshPending = true;
    const button = document.getElementById('collectionRefresh');
    const status = document.getElementById('collectionRefreshStatus');
    if (button) { button.disabled = true; button.setAttribute('aria-busy', 'true'); }
    if (status && collectionSummaryRefreshManual) status.textContent = t('common.loading');
    try {
        const result = await apiURequestList();
        if (collectionSummaryAuthBlocked) return false;
        if (!Array.isArray(result.requests)) throw new Error('invalid collection summary');
        const snapshots = result.requests.map(collectionSummarySnapshot);
        if (snapshots.some(snapshot => !snapshot) || new Set(snapshots.map(snapshot => snapshot.id)).size !== snapshots.length) {
            throw new Error('invalid collection summary');
        }
        setCollectionServerTime(result.serverTime);
        for (const [index, snapshot] of snapshots.entries()) {
            if (updateCollectionSummary(snapshot) && typeof updateFileRequestControls === 'function') {
                updateFileRequestControls(result.requests[index]);
            }
        }
        if (typeof processCollectionArrivalSnapshots === 'function') processCollectionArrivalSnapshots(snapshots);
        recordCollectionUpdatedTime();
        if (status && (collectionSummaryRefreshManual || status.textContent === t('collection.refreshFailed'))) {
            status.textContent = t('collection.refreshed');
        }
        return true;
    } catch (failure) {
        if (failure.status === 401 || failure.status === 403) blockCollectionSummaryAccess();
        else if (status && status.textContent !== t('collection.refreshFailed')) status.textContent = t('collection.refreshFailed');
        return false;
    } finally {
        collectionSummaryRefreshPending = false;
        collectionSummaryRefreshManual = false;
        if (button) { button.disabled = collectionSummaryAuthBlocked; button.removeAttribute('aria-busy'); }
    }
}
