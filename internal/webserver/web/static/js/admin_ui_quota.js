// A personal logical-usage readout, never an upload-admission decision.
let quotaSummaryLast = null;
let quotaSummaryReadPromise = null;
let quotaSummaryRefreshNeeded = false;
let quotaSummaryAuthBlocked = false;

function quotaSummarySnapshot(input) {
    if (!input || input.available !== true) return null;
    const result = {available: true};
    for (const field of ['usedBytes', 'usedFiles', 'limitBytes', 'limitFiles', 'asOf']) {
        const value = input[field];
        if (!Number.isSafeInteger(value) || value < 0 ||
            (['limitBytes', 'limitFiles', 'asOf'].includes(field) && value === 0)) return null;
        result[field] = value;
    }
    if (!Number.isFinite(new Date(result.asOf * 1000).getTime())) return null;
    return Object.freeze(result);
}

function formatQuotaBytes(bytes) {
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
    const unit = bytes === 0 ? 0 : Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    const value = new Intl.NumberFormat(document.documentElement.lang,
        {maximumFractionDigits: unit === 0 ? 0 : 2}).format(bytes / 1024 ** unit);
    return `${value} ${units[unit]}`;
}

function renderQuotaSummary(snapshot) {
    const language = document.documentElement.lang;
    const format = value => new Intl.NumberFormat(language).format(value);
    document.getElementById('quotaFiles').textContent = `${format(snapshot.usedFiles)} / ${format(snapshot.limitFiles)}`;
    const bytes = document.getElementById('quotaBytes');
    bytes.textContent = `${formatQuotaBytes(snapshot.usedBytes)} / ${formatQuotaBytes(snapshot.limitBytes)}`;
    bytes.title = t('quota.exactBytes', {used: format(snapshot.usedBytes), limit: format(snapshot.limitBytes)});
    const date = new Date(snapshot.asOf * 1000);
    const updated = document.getElementById('quotaUpdatedAt');
    updated.dateTime = date.toISOString();
    updated.textContent = t('quota.updatedAt', {time: date.toLocaleString(language)});
    document.getElementById('quotaSummary').dataset.state = 'ready';
}

function renderQuotaFailure() {
    document.getElementById('quotaSummary').dataset.state = quotaSummaryLast ? 'stale' : 'unavailable';
    const message = quotaSummaryAuthBlocked ? (quotaSummaryLast ? 'quota.staleAuth' : 'quota.authRequired') :
        (quotaSummaryLast ? 'quota.stale' : 'quota.unavailable');
    document.getElementById('quotaStatus').textContent = t(message);
}

function refreshQuotaSummary() {
    if (!document.getElementById('quotaSummary') || quotaSummaryAuthBlocked) return Promise.resolve(false);
    if (quotaSummaryReadPromise) return quotaSummaryReadPromise;
    const pending = readQuotaSummary();
    quotaSummaryReadPromise = pending;
    const clear = () => { if (quotaSummaryReadPromise === pending) quotaSummaryReadPromise = null; };
    pending.then(clear, clear);
    return pending;
}

function refreshQuotaAfterMutation() {
    if (!document.getElementById('quotaSummary') || quotaSummaryAuthBlocked) return Promise.resolve(false);
    // Writes confirmed during an older read need one follow-up read.
    if (quotaSummaryReadPromise) quotaSummaryRefreshNeeded = true;
    return refreshQuotaSummary();
}

async function readQuotaSummary() {
    const button = document.getElementById('quotaRefresh');
    const status = document.getElementById('quotaStatus');
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    status.textContent = t('common.loading');
    let available = false;
    try {
        do {
            quotaSummaryRefreshNeeded = false;
            try {
                const snapshot = quotaSummarySnapshot(await apiQuotaSummary());
                if (!snapshot) throw new Error('quota summary unavailable');
                quotaSummaryLast = snapshot;
                renderQuotaSummary(snapshot);
                status.textContent = t('quota.updated');
                available = true;
            } catch (failure) {
                if (failure.status === 401 || failure.status === 403) {
                    quotaSummaryAuthBlocked = true;
                    if (typeof storedTokens !== 'undefined') storedTokens.delete('PERM_VIEW');
                }
                renderQuotaFailure();
                available = false;
            }
        } while (quotaSummaryRefreshNeeded && !quotaSummaryAuthBlocked);
        return available;
    } finally {
        button.disabled = quotaSummaryAuthBlocked;
        button.removeAttribute('aria-busy');
    }
}

function initQuotaSummary() {
    return refreshQuotaSummary();
}
