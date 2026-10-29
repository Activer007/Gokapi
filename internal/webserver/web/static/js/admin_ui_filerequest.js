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

function changeFileCountFr(frId, change) {
	let countCell = document.getElementById("totalFiles-fr-" + frId);
	let currentCount = Number(countCell.innerText) || 0;
	let newCount = currentCount + change;
	countCell.innerText = newCount;
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
    if (document.getElementById("mc_expiry").checked) {
        let diff = document.getElementById("mi_expiry").value - Math.round(Date.now() / 1000);
        localStorage.setItem("fr_expiry", diff);
    } else {
        localStorage.setItem("fr_expiry", 0);
    }
}

function loadFileRequestDefaults() {
    const defaultMaxFiles = localStorage.getItem("fr_maxfiles");
    const defaultMaxSize = localStorage.getItem("fr_maxsize");
    let defaultExpiry = localStorage.getItem("fr_expiry");

    if (defaultExpiry !== "0" && defaultExpiry !== null) {
        let defaultDate = new Date(Date.now() + Number((defaultExpiry) * 1000));
        defaultDate.setHours(12, 0, 0, 0);
        defaultExpiry = Math.floor(defaultDate.getTime() / 1000);
    }

    setModalValues("", "", defaultMaxFiles, defaultMaxSize, defaultExpiry, "");
}

function setModalValues(id, name, maxFiles, maxSize, expiry, notes) {
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
        const defaultDate = Math.floor(new Date(Date.now() + (14 * 24 * 60 * 60 * 1000)).getTime() / 1000);
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
}

function editFileRequest(id, name, maxFiles, maxSize, expiry, notes) {
    setModalValues(id, name, maxFiles, maxSize, expiry, notes);
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

    for (const fieldId of ['mi_maxfiles','mi_maxsize']) {
        const field = document.getElementById(fieldId);
        field.required = true;
        if (!field.disabled && !field.reportValidity()) return;
    }
    await runModalSave('addEditModal', buttonSave, async () => {
        if (expiry !== 0 && (!Number.isFinite(Number(expiry)) || Number(expiry) <= Date.now()/1000)) throw new Error(t('settings.invalidExpiry'));
        const data = await apiURequestSave(id, name, maxFiles, maxSize, expiry, notes);
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

    if (row) {
        const user = document.getElementById(`cell-username-${jsonResult.id}`)?.innerText || userName;
        row.replaceWith(createFileRequestRow(jsonResult, user));
    } else {
        let tr = createFileRequestRow(jsonResult, userName);
        tr.querySelectorAll('td').forEach((td) => {
            td.classList.add("newFileRequest");
            setTimeout(() => {
                td.classList.remove("newFileRequest");
            }, 700);
        });
        tbody.prepend(tr);
    }
    updateFileRequestEmptyState();
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

    const publicUrl = `${baseUrl}publicUpload?id=${jsonResult.id}&key=${jsonResult.apikey}`;

    const tr = document.createElement("tr");
    tr.id = `row-${jsonResult.id}`;
    tr.className = "filerequest-item";

    // Name
    tr.appendChild(tdLink(jsonResult.name, publicUrl));
    // Preserve the count hook and the expandable list when editing a request.
    const countCell = document.createElement('td');
    const count = document.createElement('span');
    count.id = `totalFiles-fr-${jsonResult.id}`;
    count.textContent = jsonResult.uploadedfiles;
    countCell.appendChild(count);
    if (jsonResult.reserveduploads) countCell.appendChild(document.createTextNode(` +${jsonResult.reserveduploads}`));
    if (jsonResult.maxfiles) countCell.appendChild(document.createTextNode(` / ${jsonResult.maxfiles}`));
    const existingToggle = document.querySelector(`#row-${jsonResult.id} .collapse-toggle`);
    if (existingToggle) countCell.appendChild(existingToggle);
    tr.appendChild(countCell);
    // Total size
    tr.appendChild(tdText(getReadableSize(jsonResult.totalfilesize)));
    // Last upload
    tr.appendChild(tdText(formatTimestampWithNegative(jsonResult.lastupload, t("common.none"))));
    // Expiry
    tr.appendChild(tdText(formatFileRequestExpiry(jsonResult.expiry)));
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
        editFileRequest(jsonResult.id, jsonResult.name, jsonResult.maxfiles, jsonResult.maxsize, jsonResult.expiry, jsonResult.notes);

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
    if (download) bindRequestDownload(download, (download.dataset.fileIds || '').split(',').filter(Boolean), download.dataset.recordName || '');
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
