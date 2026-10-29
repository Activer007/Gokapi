// This file contains JS code for the API view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...


function changeApiPermission(userId, permission, buttonId) {

    var indicator = document.getElementById(buttonId);
    if (indicator.classList.contains("perm-processing") || indicator.classList.contains("perm-nochange")) {
        return;
    }
    var wasGranted = indicator.classList.contains("perm-granted");
    indicator.classList.add("perm-processing");
    indicator.classList.remove("perm-granted");
    indicator.classList.remove("perm-notgranted");

    var modifier = "GRANT";
    if (wasGranted) {
        modifier = "REVOKE";
    }


    apiAuthModify(userId, permission, modifier)
        .then(data => {
            if (wasGranted) {
                indicator.classList.add("perm-notgranted");
                indicator.classList.add("perm-nownotgranted");
            } else {
                indicator.classList.add("perm-granted");
                indicator.classList.add("perm-nowgranted");
            }
            indicator.classList.remove("perm-processing");
            setTimeout(() => {
                 indicator.classList.remove("perm-nowgranted");
                 indicator.classList.remove("perm-nownotgranted");
            }, 1000);
        })
        .catch(error => {
            if (wasGranted) {
                indicator.classList.add("perm-granted");
            } else {
                indicator.classList.add("perm-notgranted");
            }
            indicator.classList.remove("perm-processing");
            alert(t("users.permissionFailed") + error);
            console.error('Error:', error);
        });
}

function deleteApiKey(apiKey) {
    const element = document.getElementById('deleteApiModal');
    const confirm = document.getElementById('confirmDeleteApi');
    const cancel = document.getElementById('cancelDeleteApi');
    const errorMessage = document.getElementById('deleteApiError');
    const trigger = document.getElementById('delete-' + apiKey);
    const modal = bootstrap.Modal.getOrCreateInstance(element);
    document.getElementById('deleteApiName').textContent = document.getElementById('friendlyname-' + apiKey).textContent;
    errorMessage.textContent = '';
    let busy = false;
    const preventWhileBusy = event => { if (busy) event.preventDefault(); };
    element.addEventListener('hide.bs.modal', preventWhileBusy);
    element.addEventListener('hidden.bs.modal', () => {
        element.removeEventListener('hide.bs.modal', preventWhileBusy);
        confirm.onclick = null;
        if (trigger.isConnected) trigger.focus();
        else document.getElementById('button-newapi').focus();
    }, {once: true});
    confirm.onclick = async () => {
        if (busy) return;
        busy = true;
        confirm.disabled = cancel.disabled = true;
        confirm.textContent = t('common.deleting');
        errorMessage.textContent = '';
        try {
            await apiAuthDelete(apiKey);
            document.getElementById('row-' + apiKey).remove();
            busy = false;
            modal.hide();
            showToast(3500, t('api.deleted'));
        } catch (error) {
            errorMessage.textContent = t('api.deleteFailed') + error.message;
        } finally {
            busy = false;
            confirm.disabled = cancel.disabled = false;
            confirm.textContent = t('common.delete');
        }
    };
    modal.show();
}



function newApiKey() {
    document.getElementById("button-newapi").disabled = true;
    apiAuthCreate()
        .then(data => {
            addRowApi(data.Id, data.PublicId);
            document.getElementById("button-newapi").disabled = false;
        })
        .catch(error => {
            alert(t("api.createFailed") + error);
            console.error('Error:', error);
        });
}




function addFriendlyNameChange(apiKey) {
    let cell = document.getElementById("friendlyname-" + apiKey);
    if (cell.classList.contains("isBeingEdited"))
        return;
    cell.classList.add("isBeingEdited");
    let currentName = cell.innerText;
    let input = document.createElement("input");
    input.size = 5;
    input.value = currentName;
    let allowEdit = true;

    let submitEntry = function() {
        if (!allowEdit)
            return;
        allowEdit = false;
        let newName = input.value;
        if (newName == "") {
            newName = t("api.unnamed");
        }
        cell.innerText = newName;

        cell.classList.remove("isBeingEdited");

        apiAuthFriendlyName(apiKey, newName)
            .catch(error => {
                alert(t("api.nameFailed") + error);
                console.error('Error:', error);
            });
    };

    input.onblur = submitEntry;
    input.addEventListener("keyup", function(event) {
        // Enter
        if (event.keyCode === 13) {
            event.preventDefault();
            submitEntry();
        }
    });
    cell.innerText = "";
    cell.appendChild(input);
    input.focus();
}




function addRowApi(apiKey, publicId) {

    let table = document.getElementById("apitable");
    let row = table.insertRow(0);

    row.id = "row-" + publicId;
    let cellCount = 0;
    let cellFriendlyName = row.insertCell(cellCount++);
    let cellId = row.insertCell(cellCount++);
    let cellLastUsed = row.insertCell(cellCount++);
    let cellPermissions = row.insertCell(cellCount++);
    let cellUserName;
    if (canViewOtherApiKeys) {
        cellUserName = row.insertCell(cellCount++);
    }
    let cellButtons = row.insertCell(cellCount++);

    if (canViewOtherApiKeys) {
        cellUserName.classList.add("newApiKey");
        cellUserName.innerText = userName;
    }

    cellFriendlyName.classList.add("newApiKey");
    cellId.classList.add("newApiKey");
    cellLastUsed.classList.add("newApiKey");
    cellPermissions.classList.add("newApiKey");
    cellPermissions.classList.add("prevent-select");
    cellButtons.classList.add("newApiKey");


    cellFriendlyName.innerText = t("api.unnamed");
    cellFriendlyName.id = "friendlyname-" + publicId;
    cellFriendlyName.onclick = function() {
        addFriendlyNameChange(publicId);
    };
    cellId.innerText = apiKey;
    cellId.classList.add("font-monospace");
    cellLastUsed.innerText = t("common.neverUsed");


    const btnGroup = document.createElement("div");
    btnGroup.className = "btn-group";
    btnGroup.setAttribute("role", "group");


    // === Buttons Cell ===
    const copyButton = document.createElement('button');
    copyButton.type = 'button';
    copyButton.dataset.clipboardText = apiKey;
    copyButton.title = t("api.copy");
    copyButton.className = 'copyurl btn btn-outline-light btn-sm';

    const copyIcon = document.createElement('i');
    copyIcon.className = 'bi bi-copy';
    copyButton.appendChild(copyIcon);

    const deleteButton = document.createElement('button');
    deleteButton.type = 'button';
    deleteButton.id = `delete-${publicId}`;
    deleteButton.title = t("common.delete");
    deleteButton.className = 'btn btn-outline-danger btn-sm';
    deleteButton.setAttribute('onclick', `deleteApiKey('${publicId}')`);

    const deleteIcon = document.createElement('i');
    deleteIcon.className = 'bi bi-trash3';
    deleteButton.appendChild(deleteIcon);

    btnGroup.appendChild(copyButton);
    btnGroup.appendChild(deleteButton);
    cellButtons.appendChild(btnGroup);

    // === Permissions Cell ===
    const perms = [{
            perm: 'PERM_VIEW',
            icon: 'bi-eye',
            granted: true,
            title: t("api.list")
        },
        {
            perm: 'PERM_UPLOAD',
            icon: 'bi-file-earmark-plus',
            granted: true,
            title: t("nav.upload")
        },
        {
            perm: 'PERM_EDIT',
            icon: 'bi-pencil',
            granted: true,
            title: t("api.edit")
        },
        {
            perm: 'PERM_DELETE',
            icon: 'bi-trash3',
            granted: true,
            title: t("api.delete")
        },
        {
            perm: 'PERM_REPLACE',
            icon: 'bi-recycle',
            granted: false,
            title: t("api.replace")
        },
        {
            perm: 'PERM_DOWNLOAD',
            icon: 'bi-box-arrow-in-down',
            granted: false,
            title: t("api.download")
        },
        {
            perm: 'PERM_MANAGE_FILE_REQUESTS',
            icon: 'bi-file-earmark-arrow-up',
            granted: false,
            title: t("api.requests")
        },
        {
            perm: 'PERM_MANAGE_USERS',
            icon: 'bi-people',
            granted: false,
            title: t("users.manage")
        },
        {
            perm: 'PERM_MANAGE_LOGS',
            icon: 'bi-card-list',
            granted: false,
            title: t("users.manageLogs")
        },
        {
            perm: 'PERM_API_MOD',
            icon: 'bi-sliders2',
            granted: false,
            title: t("api.manage")
        }
    ];

    perms.forEach(({
        perm,
        icon,
        granted,
        title
    }) => {
        const i = document.createElement('i');
        const id = `${perm.toLowerCase()}_${publicId}`;
        i.id = id;
        i.className = `bi ${icon} ${granted ? 'perm-granted' : 'perm-notgranted'}`;
        i.title = title;
        i.setAttribute('onclick', `changeApiPermission("${publicId}","${perm}", "${id}");`);
        cellPermissions.appendChild(i);
        cellPermissions.appendChild(document.createTextNode(' '));
    });


    if (!canReplaceFiles) {
        let cell = document.getElementById("perm_replace_" + publicId);
        cell.classList.add("perm-unavailable");
        cell.classList.add("perm-nochange");
    }
    if (!canManageUsers) {
        let cell = document.getElementById("perm_manage_users_" + publicId);
        cell.classList.add("perm-unavailable");
        cell.classList.add("perm-nochange");
    }
    if (!canViewSystemLog) {
        let cell = document.getElementById("perm_manage_logs_" + publicId);
        cell.classList.add("perm-unavailable");
        cell.classList.add("perm-nochange");
    }
    if (!canCreateFileRequest) {
        let cell = document.getElementById("perm_manage_file_requests_" + publicId);
        cell.classList.add("perm-unavailable");
        cell.classList.add("perm-nochange");
    }

    setTimeout(() => {
        cellFriendlyName.classList.remove("newApiKey");
        cellId.classList.remove("newApiKey");
        cellLastUsed.classList.remove("newApiKey");
        cellPermissions.classList.remove("newApiKey");
        cellButtons.classList.remove("newApiKey");
    }, 700);

}
