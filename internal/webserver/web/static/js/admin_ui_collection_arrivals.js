// Current-page arrival notices. Only an authorized metadata snapshot can add an
// ID to this baseline; upload-progress events are not receipt confirmations.
const collectionArrivalStates = new Map();
let collectionArrivalPollTimer;
let collectionArrivalsStarted = false;

function seedCollectionArrivalBaseline(requestId, fileIDs = []) {
    collectionArrivalStates.set(requestId, {
        seenIDs: new Set(fileIDs), pendingIDs: new Set(), active: true, viewing: false, detailKey: ''
    });
    setCollectionArrivalAccess(requestId, true);
    renderCollectionArrivalNotice(requestId);
}

function collectionArrivalCanView(requestId) {
    return !collectionSummaryAuthBlocked && collectionArrivalStates.get(requestId)?.active === true;
}

function setCollectionArrivalAccess(requestId, active) {
    const row = document.getElementById(`row-${requestId}`);
    if (!row) return;
    row.dataset.collectionAccess = active ? 'available' : 'unavailable';
    const actions = row.querySelector('.request-actions');
    if (actions) actions.inert = !active;
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    if (panel) panel.inert = !active;
    const snapshot = collectionSummarySnapshots.get(requestId);
    if (snapshot) renderCollectionStatus(snapshot);
}

function renderCollectionArrivalNotice(requestId) {
    const state = collectionArrivalStates.get(requestId);
    const notice = document.getElementById(`collection-arrivals-${requestId}`);
    const count = document.getElementById(`collection-arrivals-count-${requestId}`);
    const button = document.getElementById(`collection-arrivals-view-${requestId}`);
    if (!state || !notice || !count || !button) return false;
    notice.hidden = collectionSummaryAuthBlocked || !state.active || state.pendingIDs.size === 0;
    const name = document.getElementById(`collection-name-${requestId}`)?.textContent || '';
    const label = t('collection.arrivals.count', {name, count: state.pendingIDs.size});
    if (count.textContent !== label) count.textContent = label;
    button.disabled = !collectionArrivalCanView(requestId) || state.viewing;
    const buttonLabel = t(state.viewing ? 'common.loading' : 'collection.arrivals.view');
    if (button.textContent !== buttonLabel) button.textContent = buttonLabel;
    const detail = document.getElementById(`collection-arrivals-detail-${requestId}`);
    if (detail) {
        const text = state.detailKey ? t(state.detailKey) : '';
        if (detail.textContent !== text) detail.textContent = text;
    }
    return true;
}

function processCollectionArrivalSnapshots(snapshots) {
    const visibleIDs = new Set(snapshots.map(snapshot => snapshot.id));
    for (const [requestId, state] of collectionArrivalStates) {
        if (!visibleIDs.has(requestId)) {
            state.active = false;
            setCollectionArrivalAccess(requestId, false);
            renderCollectionArrivalNotice(requestId);
        }
    }
    for (const snapshot of snapshots) {
        const requestId = snapshot.id;
        let state = collectionArrivalStates.get(requestId);
        if (!state) {
            // A newly visible scope may contain old history. Do not report it
            // as newly uploaded to the page that did not observe its baseline.
            seedCollectionArrivalBaseline(requestId, snapshot.fileidlist);
            continue;
        }
        if (!document.getElementById(`row-${requestId}`) || !state.active) {
            // Returning authorization starts a fresh visible scope. Keep seen
            // history, but do not revive old pending notices from before loss.
            state.pendingIDs.clear();
            state.detailKey = '';
            for (const id of snapshot.fileidlist) state.seenIDs.add(id);
            state.active = true;
            setCollectionArrivalAccess(requestId, true);
            renderCollectionArrivalNotice(requestId);
            continue;
        }
        const arrivals = snapshot.fileidlist.filter(id => !state.seenIDs.has(id));
        for (const id of arrivals) state.pendingIDs.add(id);
        setCollectionArrivalAccess(requestId, true);
        // If the page has not rendered the notice hook, retry on the next
        // snapshot rather than consuming arrivals invisibly.
        if (renderCollectionArrivalNotice(requestId)) {
            for (const id of arrivals) state.seenIDs.add(id);
        } else {
            for (const id of arrivals) state.pendingIDs.delete(id);
        }
    }
}

async function viewCollectionArrivals(requestId) {
    const state = collectionArrivalStates.get(requestId);
    if (!state || !collectionArrivalCanView(requestId) || state.viewing || !state.pendingIDs.size) return false;
    state.viewing = true;
    state.detailKey = '';
    renderCollectionArrivalNotice(requestId);
    const requestedIDs = [...state.pendingIDs];
    try {
        const result = await loadCollectionArrivalFiles(requestId, requestedIDs);
        if (!collectionArrivalCanView(requestId)) return false;
        if (!Array.isArray(result.shown)) throw new Error('invalid arrival detail result');
        const requested = new Set(requestedIDs);
        for (const id of result.shown) if (requested.has(id)) state.pendingIDs.delete(id);
        if (result.shown.some(id => requested.has(id))) {
            const panel = document.getElementById(`collapse-filelist-${requestId}`);
            if (panel) {
                if (typeof bootstrap !== 'undefined' && bootstrap.Collapse) {
                    bootstrap.Collapse.getOrCreateInstance(panel, {toggle: false}).show();
                } else panel.classList.add('show');
                const toggle = document.getElementById(`collection-count-${requestId}`)?.querySelector('.collapse-toggle');
                if (toggle) toggle.setAttribute('aria-expanded', 'true');
                panel.scrollIntoView?.({block: 'nearest'});
            }
        }
        state.detailKey = state.pendingIDs.size ? 'collection.arrivals.retry' : '';
        return state.pendingIDs.size === 0;
    } catch (failure) {
        if (failure.status === 401 || failure.status === 403) blockCollectionSummaryAccess();
        else state.detailKey = 'collection.arrivals.retry';
        return false;
    } finally {
        state.viewing = false;
        renderCollectionArrivalNotice(requestId);
    }
}

function stopCollectionArrivalPolling() {
    clearInterval(collectionArrivalPollTimer);
    collectionArrivalPollTimer = undefined;
}

function handleCollectionArrivalVisibility() {
    if (document.visibilityState === 'visible' && !collectionSummaryAuthBlocked) refreshCollectionSummaries({silent: true});
}

function initCollectionArrivals() {
    if (collectionArrivalsStarted) return;
    collectionArrivalsStarted = true;
    for (const snapshot of collectionSummarySnapshots.values()) seedCollectionArrivalBaseline(snapshot.id, snapshot.fileidlist);
    document.addEventListener('visibilitychange', handleCollectionArrivalVisibility);
    collectionArrivalPollTimer = setInterval(() => {
        if (document.visibilityState === 'visible' && !collectionSummaryAuthBlocked) refreshCollectionSummaries({silent: true});
    }, 60000);
}
