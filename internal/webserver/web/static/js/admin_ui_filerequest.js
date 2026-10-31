// This file contains JS code for the API view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...

function deleteFileRequest(requestId) {
    document.getElementById("delete-" + requestId).disabled = true;

    apiURequestDelete(requestId)
        .then(data => {
            const mainRow = document.getElementById("row-" + requestId);
            const fileListRow = document.getElementById("filelist-" + requestId);
            mainRow.classList.add("rowDeleting");
            if (fileListRow !== null) {
                fileListRow.classList.add("rowDeleting");
            }

            setTimeout(() => {
                mainRow.remove();
                if (fileListRow !== null) {
                    fileListRow.remove();
                }
                updateFileRequestEmptyState();
            }, 290);
        })
        .catch(error => {
            document.getElementById("delete-" + requestId).disabled = false;
            alert(t("request.deleteFailed") + error);
            console.error('Error:', error);
        });
}

function deleteOrShowModal(requestId, requestName, count) {
    if (count === 0) {
        deleteFileRequest(requestId);
    } else {
        showDeleteFRequestModal(requestId, requestName, count);
    }
}


function deleteFileFr(id, frId) {
    document.getElementById("button-delete-" + id).disabled = true;
    let rowFileEntry = document.getElementById("cell-listupload-"+id);
    apiFilesDelete(id, 10)
        .then(data => {
            changeFileCountFr(frId, -1);
            removeDownloadFileReference(id, frId);
		rowFileEntry.classList.add("rowDeleting");
		setTimeout(() => {
		    rowFileEntry.remove();
		}, 290);
	 showToastFileDeletionFr(id);
        })
        .catch(error => {
            document.getElementById("button-delete-" + id).disabled = false;
            alert(t("files.deleteFailed") + error);
            console.error('Error:', error);
        });
}

async function shareCollectionFile(id, button) {
    if (button.dataset.shared === 'true') return true;
    if (button.sharePromise) return button.sharePromise;
    if (button.disabled) return false;
    const row = document.getElementById(`cell-listupload-${id}`);
    const panel = row && document.getElementById(`collapse-filelist-${row.dataset.requestId}`);
    if (panel) {
        if (panel.shareBatchRunning && !panel.shareBatch.some(file => file.id === id)) return false;
        // A failed member retries within its displayed batch. A different single
        // action starts an explicit new scope; the success cache stays intact.
        if (!panel.shareBatch?.some(file => file.id === id)) {
            panel.shareBatch = [{id, name: row.querySelector('[data-collection-select]').dataset.name}];
            panel.deliveryChoices = new Map();
        }
    }
    if (panel) {
        if (!panel.sharePending) panel.sharePending = new Set();
        panel.sharePending.add(id);
        updateCollectionShareResults(row.dataset.requestId);
    }
    button.sharePromise = createCollectionShare(id, button);
    try {
        return await button.sharePromise;
    } finally {
        button.sharePromise = null;
        if (panel) {
            panel.sharePending.delete(id);
            updateCollectionShareResults(row.dataset.requestId);
        }
    }
}

function collectionShareRunning(panel) {
    return Boolean(panel.shareBatchRunning || panel.shareBatch?.some(file => panel.sharePending?.has(file.id)));
}

async function createCollectionShare(id, button) {
    const error = document.getElementById(`share-error-${id}`);
    const result = document.getElementById(`share-result-${id}`);
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    button.textContent = t('request.sharing');
    error.textContent = '';
    try {
        const data = await apiCollectionFileShare(id);
        const url = data.UrlDownload;
        if (!url) throw new Error(t('request.shareMissingLink'));
        // A successful conversion stays available here without creating another copy.
        button.dataset.shared = 'true';
        document.getElementById(`share-url-${id}`).value = url;
        document.getElementById(`share-copy-${id}`).dataset.clipboardText = url;
        result.hidden = false;
        button.textContent = t('request.shared');
        const row = document.getElementById(`cell-listupload-${id}`);
        if (row) {
            const panel = document.getElementById(`collapse-filelist-${row.dataset.requestId}`);
            if (!panel.shareLinks) panel.shareLinks = new Map();
            panel.shareLinks.set(id, {
                id: typeof data.Id === 'string' ? data.Id : '',
                name: row.querySelector('[data-collection-select]').dataset.name,
                url, e2e: data.IsEndToEndEncrypted !== false
            });
            updateCollectionShareResults(row.dataset.requestId);
        }
        return true;
    } catch (failure) {
        error.textContent = t('request.shareFailed') + shareFailureMessage(failure);
        button.textContent = t('request.toShare');
        return false;
    } finally {
        button.removeAttribute('aria-busy');
        button.disabled = button.dataset.shared === 'true' || button.dataset.unavailable === 'true';
    }
}

function collectionShareSelections(requestId) {
    return [...document.getElementById(`collapse-filelist-${requestId}`).querySelectorAll('[data-collection-select]')];
}

function selectCollectionShares(requestId, checked) {
    for (const field of collectionShareSelections(requestId)) {
        if (!field.disabled) field.checked = checked;
    }
    updateCollectionShareSelection(requestId);
}

function updateCollectionShareSelection(requestId) {
    const fields = collectionShareSelections(requestId).filter(field => !field.disabled);
    const selected = fields.filter(field => field.checked).length;
    const all = document.getElementById(`share-select-all-${requestId}`);
    all.checked = fields.length > 0 && selected === fields.length;
    all.indeterminate = selected > 0 && selected < fields.length;
}

function collectionShareLinks(requestId) {
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    // Keep previous conversions cached to avoid creating duplicate shares, but
    // expose only the current, explicitly selected batch in results and delivery.
    return (panel.shareBatch || []).map(file => panel.shareLinks?.get(file.id)).filter(Boolean);
}

function updateCollectionShareResults(requestId) {
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    const links = collectionShareLinks(requestId);
    const result = document.getElementById(`share-batch-result-${requestId}`);
    result.hidden = links.length === 0;
    document.getElementById(`share-batch-links-${requestId}`).value = formatShareLinks(links);
    renderCollectionDeliveryChoices(requestId);
    if (!panel.shareBatchRunning && collectionShareRunning(panel)) {
        document.getElementById(`share-batch-status-${requestId}`).textContent = t('request.sharing');
    }
    if (panel.shareBatch && !collectionShareRunning(panel)) {
        const failed = panel.shareBatch.filter(file => !panel.shareLinks?.has(file.id));
        const success = panel.shareBatch.length - failed.length;
        const state = failed.length === 0 ? 'allSucceeded' : (success ? 'partlySucceeded' : 'allFailed');
        document.getElementById(`share-batch-status-${requestId}`).textContent =
            t('sharing.' + state, {success, failed: failed.length}) +
            (failed.length ? ' ' + t('sharing.failedFiles', {names: failed.map(file => file.name).join(', ')}) : '');
    }
}

async function shareSelectedCollectionFiles(requestId, button) {
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    if (button.disabled || panel.shareBatchRunning) return;
    const selected = collectionShareSelections(requestId).filter(field => field.checked && !field.disabled);
    if (!selected.length) {
        showToast(3500, t('sharing.noSelection'));
        return;
    }
    const label = button.textContent;
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    button.textContent = t('sharing.creating');
    const status = document.getElementById(`share-batch-status-${requestId}`);
    panel.deliveryChoices = new Map();
    panel.shareBatch = selected.map(field => ({id: field.value, name: field.dataset.name}));
    panel.shareBatchRunning = true;
    updateCollectionShareResults(requestId);
    try {
        for (const [index, field] of selected.entries()) {
            status.textContent = t('sharing.progress', {current: index + 1, total: selected.length});
            const single = document.getElementById(`button-share-${field.value}`);
            if (single) await shareCollectionFile(field.value, single);
        }
    } finally {
        panel.shareBatchRunning = false;
        updateCollectionShareResults(requestId);
        button.textContent = label;
        button.removeAttribute('aria-busy');
        button.disabled = false;
    }
}

// Selection here refers to successful *new* shares, never collection source IDs.
function renderCollectionDeliveryChoices(requestId) {
    const choices = document.getElementById(`share-delivery-choices-${requestId}`);
    if (!choices) return;
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    if (!panel.deliveryChoices) panel.deliveryChoices = new Map();
    choices.replaceChildren();
    for (const source of panel.shareBatch || []) {
        const share = panel.shareLinks?.get(source.id);
        if (!share) continue;
        const label = document.createElement('label');
        label.className = 'd-flex align-items-start gap-2 mb-2';
        const field = document.createElement('input');
        field.type = 'checkbox';
        field.className = 'form-check-input flex-shrink-0';
        field.setAttribute('data-collection-delivery-select', '');
        field.value = source.id;
        const eligible = Boolean(share.id) && !share.e2e;
        field.disabled = !eligible || collectionShareRunning(panel);
        field.checked = eligible && (panel.deliveryChoices.get(source.id) ?? true);
        field.onchange = () => {
            panel.deliveryChoices.set(source.id, field.checked);
            updateCollectionDeliverySelection(requestId);
        };
        const name = document.createElement('span');
        name.className = 'delivery-item-details';
        name.textContent = share.name + (!share.id ? ' — ' + t('delivery.noShareId') : '');
        label.append(field, name);
        choices.appendChild(label);
    }
    updateCollectionDeliverySelection(requestId);
}

function selectedCollectionDeliveryFiles(requestId) {
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    const selected = [...document.getElementById(`share-delivery-choices-${requestId}`)
        .querySelectorAll('[data-collection-delivery-select]')];
    const current = new Set((panel.shareBatch || []).map(file => file.id));
    return selected.filter(field => field.checked && !field.disabled && current.has(field.value))
        .map(field => panel.shareLinks?.get(field.value))
        .filter(share => share?.id && !share.e2e)
        .map(share => ({id: share.id, name: share.name, e2e: false}));
}

function updateCollectionDeliverySelection(requestId) {
    const status = document.getElementById(`share-delivery-status-${requestId}`);
    if (!status) return;
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    const links = collectionShareLinks(requestId);
    const selected = selectedCollectionDeliveryFiles(requestId).length;
    const excluded = links.filter(share => !share.id || share.e2e).length;
    status.textContent = collectionShareRunning(panel) ? t('delivery.collectionRunning') :
        t('delivery.collectionSelection', {selected, success: links.length, failed: (panel.shareBatch || []).length - links.length}) +
        (excluded ? ' ' + t('delivery.collectionExcluded', {count: excluded}) : '') +
        (selected < 2 || selected > 20 ? ' ' + t('delivery.selectionLimit') : '');
    document.getElementById(`share-delivery-create-${requestId}`).disabled =
        collectionShareRunning(panel) || selected < 2 || selected > 20;
}

function createCollectionDelivery(requestId) {
    const panel = document.getElementById(`collapse-filelist-${requestId}`);
    if (collectionShareRunning(panel)) {
        document.getElementById(`share-delivery-status-${requestId}`).textContent = t('delivery.collectionRunning');
        return;
    }
    const files = selectedCollectionDeliveryFiles(requestId);
    // Only IDs/names/type are passed. E2E URL fragments and passwords never
    // reach the delivery API or its shared creation form.
    openDeliveryCreate(files, t('delivery.collectionContext'));
}

function copyCollectionShareLinks(requestId, button) {
    return copyShareLinks(collectionShareLinks(requestId), button);
}

function changeFileCountFr(frId, change) {
	let countCell = document.getElementById("totalFiles-fr-" + frId);
	let currentCount = Number(countCell.innerText) || 0;
	let newCount = currentCount + change;
	countCell.innerText = newCount;
    if (typeof collectionSummarySnapshots !== 'undefined') {
        const snapshot = collectionSummarySnapshots.get(frId);
        if (snapshot) updateCollectionSummary({...snapshot, uploadedfiles: Math.max(0, newCount)});
    }
}

function bindRequestDownload(button, ids, name) {
    button.dataset.fileIds = ids.join(',');
    button.dataset.recordName = name;
    button.disabled = ids.length === 0;
    button.classList.toggle('disabled', button.disabled);
    button.removeAttribute('onclick');
    button.onclick = ids.length === 0 ? null : () => {
        if (ids.length === 1) downloadFileWithPresign(ids[0]);
        else downloadFilesZipWithPresign(ids.join(','), name);
    };
}

function removeDownloadFileReference(fileId, frId) {
    const button = document.getElementById(`download-${frId}`);
    if (!button) return;
    const ids = (button.dataset.fileIds || '').split(',').filter(id => id && id !== fileId);
    bindRequestDownload(button, ids, button.dataset.recordName || '');
}


function showToastFileDeletionFr(id) {
    let notification = document.getElementById("toastnotificationUndo");
    let filename = document.getElementById("cell-name-" + id).innerText;
    let filenameToast = document.getElementById("toastFilename");
    let button = document.getElementById("toastUndoButton");

    filenameToast.innerText = filename;

    button.dataset.fileid = id;
    hideToast();
    notification.classList.add("show");

    clearTimeout(toastId);
    toastId = setTimeout(() => {
        hideFileToast();
    }, 5000);
}


function handleUndoFr(button) {
    hideFileToast();
    apiFilesRestore(button.dataset.fileid)
        .then(data => {
            window.location.reload();
        })
        .catch(error => {
            alert(t("files.restoreFailed") + error);
            console.error('Error:', error);
        });
}


function showDeleteFRequestModal(requestId, requestName, count) {
    document.getElementById("deleteModalBodyName").innerText = requestName;
    document.getElementById("deleteModalBodyCount").innerText = count;
    $('#deleteModal').modal('show');

    document.getElementById("buttonDelete").onclick = function() {
        $('#deleteModal').modal('hide');
        deleteFileRequest(requestId);
    };
}


function newFileRequest() {
    loadFileRequestDefaults();
    document.getElementById("m_urequestlabel").innerText = t("request.new");
    $('#addEditModal').modal('show');

    document.getElementById("b_fr_save").onclick = function() {
        saveFileRequest();
    };
}

function saveFileRequestDefaults() {
    if (document.getElementById("mc_maxfiles").checked) {
        localStorage.setItem("fr_maxfiles", document.getElementById("mi_maxfiles").value);
    } else {
        localStorage.setItem("fr_maxfiles", 0);
    }
    if (document.getElementById("mc_maxsize").checked) {
        localStorage.setItem("fr_maxsize", document.getElementById("mi_maxsize").value);
    } else {
        localStorage.setItem("fr_maxsize", 0);
    }
}

function loadFileRequestDefaults() {
    const defaultMaxFiles = localStorage.getItem("fr_maxfiles");
    const defaultMaxSize = localStorage.getItem("fr_maxsize");
    const defaultExpiry = Math.floor(Date.now() / 1000) + 5 * 24 * 60 * 60;
    setModalValues("", "", defaultMaxFiles, defaultMaxSize, defaultExpiry, "", 1);
}

function setModalValues(id, name, maxFiles, maxSize, expiry, notes, allowedDownloads) {
    const previousError = document.querySelector('#addEditModal .save-error');
    if (previousError) previousError.textContent = '';
    document.getElementById("freqId").value = id;

    if (name === null) {
        document.getElementById("mFriendlyName").value = "";
    } else {
        document.getElementById("mFriendlyName").value = name;
    }


    if (limitMaxFiles != 0) {
        let checkbox = document.getElementById("mc_maxfiles");
        if (maxFiles === null || maxFiles == 0) {
            maxFiles = limitMaxFiles;
        }
        checkbox.checked = true;
        checkbox.disabled = true;
        checkbox.title = t("request.adminUnlimited");
        checkbox.value = "1";
        document.getElementById("mi_maxfiles").setAttribute("max", limitMaxFiles);
    } else {
        let checkbox = document.getElementById("mc_maxfiles");
        checkbox.disabled = false;
        checkbox.title = "";
        document.getElementById("mi_maxfiles").setAttribute("max", "");
    }

    if (limitMaxSize != 0) {
        let checkbox = document.getElementById("mc_maxsize");
        if (maxSize === null || maxSize == 0) {
            maxSize = limitMaxSize;
        }
        checkbox.checked = true;
        checkbox.disabled = true;
        checkbox.title = t("request.adminUnlimited");
        checkbox.value = "1";
        document.getElementById("mi_maxsize").setAttribute("max", limitMaxSize);
    } else {
        let checkbox = document.getElementById("mc_maxsize");
        checkbox.disabled = false;
        checkbox.title = "";
        document.getElementById("mi_maxsize").setAttribute("max", "");
    }

    if (maxFiles === null || maxFiles == 0) {
        document.getElementById("mi_maxfiles").value = "1";
        document.getElementById("mi_maxfiles").disabled = true;
        document.getElementById("mc_maxfiles").checked = false;
    } else {
        document.getElementById("mi_maxfiles").value = maxFiles;
        document.getElementById("mi_maxfiles").disabled = false;
        document.getElementById("mc_maxfiles").checked = true;
    }


    if (maxSize === null || maxSize == 0) {
        document.getElementById("mi_maxsize").value = "10";
        document.getElementById("mi_maxsize").disabled = true;
        document.getElementById("mc_maxsize").checked = false;
    } else {
        document.getElementById("mi_maxsize").value = maxSize;
        document.getElementById("mi_maxsize").disabled = false;
        document.getElementById("mc_maxsize").checked = true;
    }

    if (expiry === null || expiry == 0) {
        const defaultDate = Math.floor(Date.now() / 1000) + 5 * 24 * 60 * 60;
        document.getElementById("mi_expiry").disabled = true;
        document.getElementById("mc_expiry").checked = false;
        document.getElementById("mi_expiry").value = defaultDate;
        createCalendar("mi_expiry", defaultDate);
    } else {
        document.getElementById("mi_expiry").value = expiry;
        document.getElementById("mi_expiry").disabled = false;
        document.getElementById("mc_expiry").checked = true;
        createCalendar("mi_expiry", expiry);
    }
    document.getElementById("mNotes").value = notes;

    const keepLegacyDownloads = !!id && Number(allowedDownloads || 0) === 0;
    document.getElementById("legacyDownloadsOption").hidden = !keepLegacyDownloads;
    document.getElementById("mc_keepdownloads").checked = keepLegacyDownloads;
    document.getElementById("mi_downloads").value = keepLegacyDownloads ? 1 : (allowedDownloads ?? 1);
    handleRequestDownloadPolicyChange();
}

function handleRequestDownloadPolicyChange() {
    document.getElementById("mi_downloads").disabled = document.getElementById("mc_keepdownloads").checked;
}

function editFileRequest(id, name, maxFiles, maxSize, expiry, notes, allowedDownloads) {
    setModalValues(id, name, maxFiles, maxSize, expiry, notes, allowedDownloads);
    document.getElementById("m_urequestlabel").innerText = t("request.editTitle");
    $('#addEditModal').modal('show');

    document.getElementById("b_fr_save").onclick = function() {
        saveFileRequest();
    };
}


async function saveFileRequest() {
    const buttonSave = document.getElementById("b_fr_save");
    const id = document.getElementById("freqId").value;
    const name = document.getElementById("mFriendlyName").value || t("request.unnamed");
    const notes = document.getElementById("mNotes").value;
    let maxFiles = 0;
    let maxSize = 0;
    let expiry = 0;

    if (document.getElementById("mc_maxfiles").checked) {
        maxFiles = document.getElementById("mi_maxfiles").value;
    }
    if (document.getElementById("mc_maxsize").checked) {
        maxSize = document.getElementById("mi_maxsize").value;
    }
    if (document.getElementById("mc_expiry").checked) {
        expiry = document.getElementById("mi_expiry").value;
    }

    const allowedDownloads = document.getElementById("mc_keepdownloads").checked
        ? undefined : Number(document.getElementById("mi_downloads").value);

    for (const fieldId of ['mi_maxfiles','mi_maxsize','mi_downloads']) {
        const field = document.getElementById(fieldId);
        field.required = true;
        if (!field.disabled && !field.reportValidity()) return;
    }
    await runModalSave('addEditModal', buttonSave, async () => {
        if (expiry !== 0 && (!Number.isFinite(Number(expiry)) || Number(expiry) <= Date.now()/1000)) throw new Error(t('settings.invalidExpiry'));
        if (allowedDownloads !== undefined && (!Number.isInteger(allowedDownloads) || allowedDownloads < 1 || allowedDownloads > 10)) throw new Error(t('request.invalidDownloads'));
        const data = await apiURequestSave(id, name, maxFiles, maxSize, expiry, notes, allowedDownloads);
        insertOrReplaceFileRequest(data);
        saveFileRequestDefaults();
        showToast(3500,t('settings.saved'));
    });
}

function checkMaxNumber(element) {
    if (element.value == "") {
        element.value = "1";
        return;
    }
    let maxVal = element.getAttribute("max");
    if (maxVal == "") {
        return;
    }
    if (element.value > maxVal) {
        element.value = maxVal;
    }
}

function insertOrReplaceFileRequest(jsonResult) {
    const tbody = document.getElementById("filerequesttable");
    let row = document.getElementById(`row-${jsonResult.id}`);
    const isNew = !row;

    if (row) {
        updateCollectionSummary(jsonResult);
        updateFileRequestControls(jsonResult);
    } else {
        let tr = createFileRequestRow(jsonResult, userName);
        tr.querySelectorAll('td').forEach((td) => {
            td.classList.add("newFileRequest");
            setTimeout(() => {
                td.classList.remove("newFileRequest");
            }, 700);
        });
        tbody.prepend(tr);
        updateCollectionSummary(jsonResult);
    }
    row = document.getElementById(`row-${jsonResult.id}`);
    if (row) row.dataset.ownerId = String(jsonResult.userid);
    if (typeof ensureCollectionArrivalPanel === 'function') ensureCollectionArrivalPanel(jsonResult.id);
    if (isNew && typeof seedCollectionArrivalBaseline === 'function') seedCollectionArrivalBaseline(jsonResult.id, []);
    updateFileRequestEmptyState();
}

function updateFileRequestControls(input) {
    const {id, name, maxfiles, maxsize, expiry, notes, alloweddownloads, uploadedfiles} = input;
    const summaryRow = document.getElementById(`row-${id}`);
    if (summaryRow && Number.isSafeInteger(input.userid) && input.userid > 0) summaryRow.dataset.ownerId = String(input.userid);
    const nameLink = document.getElementById(`collection-name-${id}`);
    if (nameLink && typeof name === 'string') nameLink.textContent = name;
    const edit = document.getElementById(`edit-${id}`);
    if (edit) edit.onclick = () => editFileRequest(id, name, maxfiles, maxsize, expiry, notes, alloweddownloads);
    const remove = document.getElementById(`delete-${id}`);
    if (remove) remove.onclick = () => deleteOrShowModal(id, name, uploadedfiles);
    const download = document.getElementById(`download-${id}`);
    if (download && Array.isArray(input.fileidlist)) {
        bindRequestDownload(download, input.fileidlist, name);
        const row = document.getElementById(`row-${id}`);
        if (row) prepareFileRequestRow(row);
    }
}


function createFileRequestRow(jsonResult, user) {

    function tdText(text) {
        const td = document.createElement("td");
        td.textContent = text;
        return td;
    }

    function tdLink(text, href) {
        const td = document.createElement("td");
        const a = document.createElement("a");
        a.textContent = text;
        a.href = href;
        a.target = "_blank";
        td.appendChild(a);
        return td;
    }

    function icon(classes) {
        const i = document.createElement("i");
        i.className = `bi ${classes}`;
        return i;
    }

    const publicUrl = `${baseUrl}r/${encodeURIComponent(jsonResult.apikey)}`;

    const tr = document.createElement("tr");
    tr.id = `row-${jsonResult.id}`;
    tr.className = "filerequest-item";

    // Name
    const nameCell = tdLink(jsonResult.name, publicUrl);
    nameCell.querySelector('a').id = `collection-name-${jsonResult.id}`;
    const badge = document.createElement('span');
    badge.id = `collection-status-${jsonResult.id}`;
    badge.className = 'badge collection-status';
    badge.setAttribute('role', 'status');
    nameCell.appendChild(badge);
    const arrivals = document.createElement('div');
    arrivals.id = `collection-arrivals-${jsonResult.id}`;
    arrivals.className = 'collection-arrivals';
    arrivals.hidden = true;
    const arrivalCount = document.createElement('span');
    arrivalCount.id = `collection-arrivals-count-${jsonResult.id}`;
    arrivalCount.setAttribute('role', 'status');
    arrivalCount.setAttribute('aria-live', 'polite');
    const viewArrivals = document.createElement('button');
    viewArrivals.id = `collection-arrivals-view-${jsonResult.id}`;
    viewArrivals.type = 'button';
    viewArrivals.className = 'btn btn-outline-light btn-sm';
    viewArrivals.textContent = t('collection.arrivals.view');
    viewArrivals.onclick = () => viewCollectionArrivals(jsonResult.id);
    const arrivalDetail = document.createElement('span');
    arrivalDetail.id = `collection-arrivals-detail-${jsonResult.id}`;
    arrivalDetail.className = 'small';
    arrivalDetail.setAttribute('role', 'status');
    arrivals.append(arrivalCount, viewArrivals, arrivalDetail);
    nameCell.appendChild(arrivals);
    tr.appendChild(nameCell);
    // Preserve the count hook and the expandable list when editing a request.
    const countCell = document.createElement('td');
    countCell.id = `collection-count-${jsonResult.id}`;
    const received = document.createElement('span');
    received.id = `collection-received-${jsonResult.id}`;
    received.appendChild(document.createTextNode(t('collection.receivedLabel') + ' '));
    const count = document.createElement('span');
    count.id = `totalFiles-fr-${jsonResult.id}`;
    count.textContent = jsonResult.uploadedfiles;
    received.appendChild(count);
    const maximum = document.createElement('span');
    maximum.id = `collection-max-${jsonResult.id}`;
    received.appendChild(maximum);
    countCell.appendChild(received);
    for (const [prefix, key, className] of [['inflight', 'collection.inflight', 'collection-inflight'],
        ['empty', 'collection.noFiles', 'collection-no-files']]) {
        const helper = document.createElement('span');
        helper.id = `collection-${prefix}-${jsonResult.id}`;
        helper.className = `small ${className}`;
        helper.textContent = t(key, {count: jsonResult.reserveduploads});
        countCell.appendChild(helper);
    }
    const existingToggle = document.querySelector(`#row-${jsonResult.id} .collapse-toggle`);
    if (existingToggle) countCell.appendChild(existingToggle);
    tr.appendChild(countCell);
    // Total size
    const sizeCell = tdText(getReadableSize(jsonResult.totalfilesize));
    sizeCell.id = `collection-size-${jsonResult.id}`;
    tr.appendChild(sizeCell);
    // Last upload
    const lastCell = tdText(formatTimestampWithNegative(jsonResult.lastupload, t("common.none")));
    lastCell.id = `cell-lastupdate-${jsonResult.id}`;
    tr.appendChild(lastCell);
    // Expiry
    const expiryCell = tdText(formatFileRequestExpiry(jsonResult.expiry));
    expiryCell.id = `cell-expiry-${jsonResult.id}`;
    tr.appendChild(expiryCell);
    // Optional user column
    if (canViewOtherRequests) {
        let userTd = tdText(user);
        userTd.id = `cell-username-${jsonResult.id}`;
        tr.appendChild(userTd);
    }
    // Buttons
    const td = document.createElement("td");

    const group = document.createElement("div");
    group.className = "btn-group";
    group.role = "group";

    // Download
    const downloadBtn = document.createElement("button");
    downloadBtn.id = `download-${jsonResult.id}`;
    downloadBtn.type = "button";
    downloadBtn.className = "btn btn-outline-light btn-sm";
    downloadBtn.title = t("request.downloadAll");

    bindRequestDownload(downloadBtn, jsonResult.fileidlist || [], jsonResult.name);

    downloadBtn.appendChild(icon("bi-download"));


    // Copy
    const copyBtn = document.createElement("button");
    copyBtn.id = `copy-${jsonResult.id}`;
    copyBtn.type = "button";
    copyBtn.className = "copyurl btn btn-outline-light btn-sm";
    copyBtn.title = t("common.copyURL");
    copyBtn.setAttribute("data-clipboard-text", publicUrl);

    copyBtn.appendChild(icon("bi-copy"));


    // Edit
    const editBtn = document.createElement("button");
    editBtn.id = `edit-${jsonResult.id}`;
    editBtn.type = "button";
    editBtn.className = "btn btn-outline-light btn-sm";
    editBtn.title = t("request.edit");
    editBtn.onclick = () =>
        editFileRequest(jsonResult.id, jsonResult.name, jsonResult.maxfiles, jsonResult.maxsize, jsonResult.expiry, jsonResult.notes, jsonResult.alloweddownloads);

    editBtn.appendChild(icon("bi-pencil"));

    // Delete
    const deleteBtn = document.createElement("button");
    deleteBtn.id = `delete-${jsonResult.id}`;
    deleteBtn.type = "button";
    deleteBtn.className = "btn btn-outline-danger btn-sm";
    deleteBtn.title = t("common.delete");
    deleteBtn.onclick = () =>
        deleteOrShowModal(jsonResult.id, jsonResult.name, jsonResult.uploadedfiles);

    deleteBtn.appendChild(icon("bi-trash3"));

    group.append(downloadBtn, copyBtn, editBtn, deleteBtn);
    td.appendChild(group);
    tr.appendChild(td);
    labelIconButtons(tr);
    prepareFileRequestRow(tr);
    return tr;
}

function prepareFileRequestRow(row) {
    const download = row.querySelector('[id^="download-"]');
    if (download) {
        // Saved responses still include exhausted files until cleanup. Keep
        // their exclusions when replacing a row after any collection edit.
        const ids = (download.dataset.fileIds || '').split(',').filter(id => {
            if (!id) return false;
            const remaining = document.getElementById(`cell-downloadsRemaining-${id}`)?.dataset.remaining;
            return remaining === undefined || Number(remaining) !== 0;
        });
        bindRequestDownload(download, ids, download.dataset.recordName || '');
    }
    const copy = row.querySelector('.copyurl');
    if (copy) { copy.textContent = t('common.copyLink'); copy.setAttribute('aria-label', t('common.copyLink')); }
    const actions = row.querySelector('.btn-group');
    if (actions) actions.classList.add('request-actions');
}

function updateFileRequestEmptyState() {
    const table = document.getElementById('filerequesttable');
    if (!table) return;
    const count = table.querySelectorAll('.filerequest-item').length;
    document.getElementById('requestCount').textContent = String(count);
    document.getElementById('requestEmpty').hidden = count !== 0;
    document.getElementById('requestTableWrap').hidden = count === 0;
}

function expireCollectionFile(id) {
    const row = document.getElementById(`cell-listupload-${id}`);
    if (!row) return;
    row.querySelectorAll('[data-collection-download]').forEach(control => {
        control.disabled = true;
        control.setAttribute('aria-disabled', 'true');
        control.removeAttribute('onclick');
        control.removeAttribute('href');
        control.classList.add('disabled');
    });
    const share = document.getElementById(`button-share-${id}`);
    if (share) {
        share.dataset.unavailable = 'true';
        share.disabled = true;
    }
    const selection = row.querySelector('[data-collection-select]');
    if (selection) {
        selection.checked = false;
        selection.disabled = true;
        updateCollectionShareSelection(row.dataset.requestId);
    }
    const status = document.getElementById(`cell-expired-file-${id}`);
    if (status) status.hidden = false;
    removeDownloadFileReference(id, row.dataset.requestId);
}

function applyCollectionDownloadEvent(data) {
    let event;
    try { event = JSON.parse(data); } catch (_) { return; }
    if (event.event !== 'download' || !Number.isInteger(event.downloads_remaining) || event.downloads_remaining < 0) return;
    const cell = document.getElementById(`cell-downloadsRemaining-${event.file_id}`);
    if (!cell) return;
    // Publications can arrive out of order after concurrent downloads.
    if (Number(cell.dataset.remaining) >= 0 && event.downloads_remaining > Number(cell.dataset.remaining)) return;
    cell.dataset.remaining = String(event.downloads_remaining);
    cell.textContent = String(event.downloads_remaining);
    if (event.downloads_remaining === 0) expireCollectionFile(event.file_id);
}

function registerCollectionChanges() {
    const connectDirect = () => {
        const source = new EventSource('./uploadStatus');
        source.onmessage = event => applyCollectionDownloadEvent(event.data);
        // EventSource reconnects itself after a transient transport failure.
    };
    if (typeof SharedWorker !== 'undefined') {
        try {
            const worker = new SharedWorker('./js/sse-worker.js');
            worker.port.onmessage = event => {
                if (event.data.type === 'message') applyCollectionDownloadEvent(event.data.data);
                else if (event.data.type === 'shutdown') window.location.href = './login';
            };
            worker.onerror = () => {
                worker.onerror = null;
                worker.port.close();
                sseWorkerPort = null;
                connectDirect();
            };
            worker.port.start();
            sseWorkerPort = worker.port;
            return;
        } catch (_) { /* Fall through when SharedWorker is unavailable. */ }
    }
    connectDirect();
}
