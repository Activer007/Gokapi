// Delivery lists organize existing ordinary shares. Download authorization stays
// on each file's original share page.
let deliveryCreating = false;
let deliveryCreateUncertain = false;
let deliveryPendingFiles = null;

async function deliveryRequest(action, payload) {
    const permissions = {list: 'PERM_VIEW', create: 'PERM_UPLOAD', revoke: 'PERM_DELETE'};
    const token = await getToken(permissions[action], false);
    const options = {method: action === 'list' ? 'GET' : 'POST',
        headers: {'Content-Type': 'application/json', apikey: token}};
    if (payload !== undefined) options.body = JSON.stringify(payload);
    const response = await fetch(`./api/deliveries/${action}`, options);
    if (!response.ok) throw await apiFailure(response);
    return response.json();
}

function selectedDeliveryFiles() {
    const rows = visibleShareRows($('#maintable').DataTable());
    return rows.filter(row => {
        const checkbox = prepareFileSelection(row);
        return checkbox && !checkbox.disabled && checkbox.checked;
    }).map(row => ({id: row.id.slice('row-'.length),
        name: row.querySelector('.file-name')?.textContent || row.id.slice('row-'.length),
        e2e: row.dataset.shareE2e === 'true'}));
}

function showDeliveryCreate() {
    return openDeliveryCreate(selectedDeliveryFiles());
}

// Both entry points pass authoritative ordinary-share IDs. Keep a separate
// snapshot so filtering a table or retrying a conversion cannot change a form
// that the user is already reviewing.
function openDeliveryCreate(files, contextLabel = '') {
    const status = document.getElementById('deliveryStatus');
    const panel = document.getElementById('deliveryCreatePanel');
    if (deliveryCreating || deliveryCreateUncertain) {
        panel.hidden = false;
        status.textContent = t(deliveryCreating ? 'delivery.creating' : 'delivery.createUncertain');
        return false;
    }
    if (!Array.isArray(files) || files.some(file => !file || typeof file.id !== 'string' || !file.id) ||
        new Set(files.map(file => file.id)).size !== files.length || files.length < 2 || files.length > 20) {
        status.textContent = t('delivery.selectionLimit');
        return false;
    }
    if (files.some(file => file.e2e)) {
        status.textContent = t('delivery.noE2E');
        return false;
    }
    deliveryPendingFiles = Object.freeze(files.map(file => Object.freeze({
        id: file.id, name: String(file.name || file.id), e2e: Boolean(file.e2e)
    })));
    const confirmation = document.getElementById('deliverySelectedFiles');
    if (confirmation) {
        confirmation.replaceChildren();
        for (const file of deliveryPendingFiles) {
            const item = document.createElement('li');
            item.textContent = file.name;
            confirmation.appendChild(item);
        }
    }
    const context = document.getElementById('deliveryCreateContext');
    if (context) context.textContent = contextLabel;
    document.getElementById('deliveryCreated').hidden = true;
    document.getElementById('deliveryCreatedURL').value = '';
    panel.hidden = false;
    document.getElementById('deliveryTitle').focus();
    panel.scrollIntoView?.({block: 'nearest'});
    status.textContent = t('delivery.selected', {count: files.length});
    return true;
}

function deliveryFailure(failure) {
    if (failure.status === 401 || failure.status === 403) return t('delivery.errorPermission');
    if (failure.status === 400 || failure.status === 404 || failure.status === 409) return t('delivery.errorInvalid');
    return t('delivery.errorGeneric');
}

async function createDelivery(event) {
    event.preventDefault();
    if (deliveryCreating || deliveryCreateUncertain) return;
    const files = deliveryPendingFiles || selectedDeliveryFiles();
    const status = document.getElementById('deliveryStatus');
    if (files.some(file => file.e2e)) { status.textContent = t('delivery.noE2E'); return; }
    if (files.length < 2 || files.length > 20) { status.textContent = t('delivery.selectionLimit'); return; }
    const form = document.getElementById('deliveryCreateForm');
    if (!form.reportValidity()) return;
    const expiry = document.getElementById('deliveryExpiry').value;
    const expiresAt = expiry ? Math.floor(new Date(expiry).getTime() / 1000) : 0;
    if (expiry && (!Number.isFinite(expiresAt) || expiresAt <= Date.now() / 1000)) {
        status.textContent = t('delivery.futureExpiry');
        return;
    }
    const button = document.getElementById('deliveryCreateSubmit');
    const fields = ['deliveryTitle', 'deliveryNote', 'deliveryExpiry'].map(id => document.getElementById(id));
    const disabled = fields.map(field => field.disabled);
    deliveryCreating = true;
    fields.forEach(field => { field.disabled = true; });
    document.getElementById('deliveryCreated').hidden = true;
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    status.textContent = t('delivery.creating');
    try {
        const result = await deliveryRequest('create', {
            title: document.getElementById('deliveryTitle').value.trim(),
            note: document.getElementById('deliveryNote').value,
            fileIds: files.map(file => file.id), expiresAt
        });
        if (!result?.url) throw new Error('missing delivery result');
        document.getElementById('deliveryCreatedURL').value = result.url;
        const open = document.getElementById('deliveryCreatedOpen');
        if (open) open.href = result.url;
        document.getElementById('deliveryCreated').hidden = false;
        document.getElementById('deliveryCreatePanel').hidden = true;
        form.reset();
        deliveryPendingFiles = null;
        status.textContent = t('delivery.created');
        await refreshDeliveries();
    } catch (failure) {
        // A lost response may follow a committed write. Require a successful
        // list query and inspection before allowing another creation attempt.
        deliveryCreateUncertain = !failure.status || failure.status >= 500;
        status.textContent = deliveryCreateUncertain ? t('delivery.createUncertain') : deliveryFailure(failure);
    } finally {
        deliveryCreating = false;
        fields.forEach((field, index) => { field.disabled = disabled[index]; });
        button.disabled = deliveryCreateUncertain;
        button.removeAttribute('aria-busy');
    }
}

function deliveryState(item) {
    if (item.revokedAt) return t('delivery.revoked');
    if (item.expiresAt && item.expiresAt <= Date.now() / 1000) return t('delivery.expired');
    return t('delivery.active');
}

function renderDeliveries(items) {
    const list = document.getElementById('deliveryList');
    list.replaceChildren();
    if (!items.length) {
        const empty = document.createElement('p');
        empty.className = 'text-muted mb-0';
        empty.textContent = t('delivery.empty');
        list.appendChild(empty);
        return;
    }
    for (const item of items) {
        const row = document.createElement('li');
        row.className = 'delivery-management-row';
        const details = document.createElement('div');
        details.className = 'delivery-item-details';
        const title = document.createElement('strong');
        title.textContent = item.title;
        const summary = document.createElement('p');
        summary.className = 'small text-muted mb-0';
        summary.textContent = `${deliveryState(item)} · ${t('delivery.fileCount', {count: item.fileIds.length})}`;
        details.append(title, summary);
        if (item.expiresAt) {
            const expiry = document.createElement('p');
            expiry.className = 'small text-muted mb-0';
            expiry.textContent = `${t('common.expiry')}: ${new Date(item.expiresAt * 1000).toLocaleString()}`;
            details.appendChild(expiry);
        }
        const actions = document.createElement('div');
        actions.className = 'delivery-item-actions';
        const open = document.createElement('a');
        open.className = 'btn btn-outline-light btn-sm';
        open.textContent = t('delivery.view');
        open.href = item.url;
        open.target = '_blank';
        open.rel = 'noopener noreferrer';
        const copy = document.createElement('button');
        copy.type = 'button';
        copy.className = 'btn btn-outline-light btn-sm';
        copy.textContent = t('common.copyLink');
        copy.onclick = () => copySensitiveText(item.url, copy, t('files.copyToast'));
        const revoke = document.createElement('button');
        revoke.type = 'button';
        revoke.className = 'btn btn-outline-danger btn-sm';
        revoke.textContent = t('delivery.revoke');
        revoke.disabled = Boolean(item.revokedAt);
        revoke.onclick = () => revokeDelivery(item.token, revoke);
        actions.append(open, copy, revoke);
        row.append(details, actions);
        list.appendChild(row);
    }
}

async function refreshDeliveries() {
    const button = document.getElementById('deliveryRefresh');
    if (button.disabled) return;
    button.disabled = true;
    const status = document.getElementById('deliveryListStatus');
    // A query already in flight before an uncertain write cannot confirm it.
    const checkingUncertain = deliveryCreateUncertain;
    status.textContent = t('common.loading');
    try {
        const items = await deliveryRequest('list');
        if (!Array.isArray(items)) throw new Error('invalid delivery list');
        renderDeliveries(items);
        status.textContent = '';
        if (checkingUncertain && deliveryCreateUncertain) {
            deliveryCreateUncertain = false;
            document.getElementById('deliveryCreateSubmit').disabled = deliveryCreating;
            document.getElementById('deliveryStatus').textContent = t('delivery.inspectBeforeRetry');
        }
    } catch (failure) {
        status.textContent = t('delivery.listFailed');
    } finally {
        button.disabled = false;
    }
}

async function revokeDelivery(token, button) {
    if (button.disabled || !confirm(t('delivery.revokeConfirm'))) return;
    button.disabled = true;
    const status = document.getElementById('deliveryListStatus');
    try {
        await deliveryRequest('revoke', {token});
        await refreshDeliveries();
    } catch (failure) {
        status.textContent = t('delivery.revokeFailed');
        button.disabled = false;
    }
}
