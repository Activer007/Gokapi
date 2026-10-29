// This file contains JS code for the User view
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...


function changeRank(userId, newRank, buttonId) {

    let indicator = document.getElementById(buttonId);
    if (indicator.disabled) {
        return;
    }
    const name = document.getElementById('row-' + userId).cells[0].textContent.trim();
    if (!window.confirm(t('users.rankConfirm', {name, role: t(newRank === 'ADMIN' ? 'user.level.1' : 'user.level.2')}))) return;
    indicator.disabled = true;

    apiUserChangeRank(userId, newRank)
        .then(data => {
            location.reload();
        })
        .catch(error => {
            indicator.disabled = false;
            alert(t("users.rankFailed") + error);
            console.error('Error:', error);
        });
}



function showDeleteUserModal(userId, userEmail) {
    let checkboxDelete = document.getElementById("checkboxDelete");
    checkboxDelete.checked = false;
    document.getElementById("deleteModalBody").innerText = userEmail;
    $('#deleteModal').modal('show');

    const error = document.querySelector('#deleteModal .save-error');
    if (error) error.textContent = '';
    document.getElementById("buttonDelete").onclick = async function() {
        const deleteFiles = checkboxDelete.checked;
        await runModalSave('deleteModal', this, async () => {
            await apiUserDelete(userId, deleteFiles);
            document.getElementById('row-' + userId).remove();
        });
    };
}


// Credentials are only held while the originating modal is open. They are not
// attached to user rows, storage, clipboard data attributes or event closures.
const userCredentialState = new WeakMap();
const userCredentialLifecycle = new WeakSet();

function clearUserCredentials(modal) {
    userCredentialState.delete(modal);
    delete modal.dataset.credentialsIssued;
    const panel = modal.querySelector('[data-user-credentials]');
    if (!panel) return;
    panel.hidden = true;
    for (const field of panel.querySelectorAll('input, textarea')) field.value = '';
    panel.querySelector('[data-credential-field="password"]').type = 'password';
    panel.querySelector('[data-credential-fallback]').hidden = true;
    for (const button of panel.querySelectorAll('button')) button.onclick = null;
    const toggle = panel.querySelector('[data-credential-toggle]');
    toggle.textContent = t('users.showPassword');
    toggle.setAttribute('aria-pressed', 'false');
}

function resetUserCredentialModal(modal) {
    clearUserCredentials(modal);
    for (const entry of modal.querySelectorAll('[data-credential-entry], [data-credential-actions]')) entry.hidden = false;
    for (const close of modal.querySelectorAll('[data-credential-close]')) close.hidden = true;
    const error = modal.querySelector('.save-error');
    if (error) error.textContent = '';
    const form = modal.querySelector('form');
    if (form) form.reset();
    modal.querySelector('.modal-title').textContent = t(modal.id === 'newUserModal' ? 'users.create' : 'users.resetPassword');
    if (modal.id === 'resetPasswordModal') {
        modal.querySelector('#forceNewPassword').checked = true;
        modal.querySelector('#generateRandomPassword').checked = false;
        modal.querySelector('#l_userpwreset').textContent = '';
        modal.querySelector('#resetPasswordButton').onclick = null;
    }
}

function prepareUserCredentialModal(id) {
    const modal = document.getElementById(id);
    if (!userCredentialLifecycle.has(modal)) {
        modal.addEventListener('hidden.bs.modal', () => resetUserCredentialModal(modal));
        userCredentialLifecycle.add(modal);
    }
    resetUserCredentialModal(modal);
    return modal;
}

function credentialShareText(credentials) {
    return t('users.accountShare', credentials);
}

async function copyUserCredential(modal, field, trigger) {
    const credentials = userCredentialState.get(modal);
    if (!credentials) return;
    const text = field === 'all' ? credentialShareText(credentials) : credentials[field];
    try {
        await navigator.clipboard.writeText(text);
        if (userCredentialState.get(modal) === credentials) {
            const fallback = modal.querySelector('[data-credential-fallback]');
            fallback.hidden = true;
            fallback.querySelector('textarea').value = '';
            showToast(3500, t('users.infoCopied'));
        }
    } catch (error) {
        // The modal may have closed while a browser permission prompt was open.
        if (userCredentialState.get(modal) !== credentials) return;
        const fallback = modal.querySelector('[data-credential-fallback]');
        const value = fallback.querySelector('textarea');
        fallback.hidden = false;
        value.value = text;
        value.focus();
        value.select();
    }
}

function finishUserCredentialEntry(modal) {
    // Hide explicitly: runModalSave re-enables captured controls in its finally.
    for (const entry of modal.querySelectorAll('[data-credential-entry], [data-credential-actions]')) entry.hidden = true;
    for (const close of modal.querySelectorAll('[data-credential-close]')) close.hidden = false;
    modal.dataset.credentialsIssued = 'true';
}

function showUserCredentials(modal, values) {
    finishUserCredentialEntry(modal);
    if (![values.username, values.password, values.loginUrl].every(value => typeof value === 'string' && value.length > 0)) {
        // Account creation/reset already succeeded. Do not expose its submit
        // button again or accidentally create/regenerate credentials on retry.
        throw new Error(t('users.credentialsUnavailable'));
    }
    const credentials = {username: values.username, password: values.password, loginUrl: values.loginUrl};
    userCredentialState.set(modal, credentials);
    const panel = modal.querySelector('[data-user-credentials]');
    for (const field of ['username', 'password', 'loginUrl']) panel.querySelector('[data-credential-field="'+field+'"]').value = credentials[field];
    panel.hidden = false;
    modal.querySelector('.modal-title').textContent = t(modal.id === 'newUserModal' ? 'users.accountReady' : 'users.accountResetReady');
    for (const button of panel.querySelectorAll('[data-credential-copy]')) {
        button.onclick = () => copyUserCredential(modal, button.dataset.credentialCopy, button);
    }
    const toggle = panel.querySelector('[data-credential-toggle]');
    toggle.onclick = () => {
        const field = panel.querySelector('[data-credential-field="password"]');
        const visible = field.type === 'password';
        field.type = visible ? 'text' : 'password';
        toggle.textContent = t(visible ? 'users.hidePassword' : 'users.showPassword');
        toggle.setAttribute('aria-pressed', String(visible));
    };
}

function showAddUserModal() {
    const modal = prepareUserCredentialModal('newUserModal');
    bootstrap.Modal.getOrCreateInstance(modal).show();
}


function showResetPwModal(userid, name) {
    const modal = prepareUserCredentialModal('resetPasswordModal');
    document.getElementById("l_userpwreset").innerText = name;
    let button = document.getElementById("resetPasswordButton");
    button.onclick = function() {
        resetPw(userid, document.getElementById("generateRandomPassword").checked);
    };
    bootstrap.Modal.getOrCreateInstance(modal).show();
}

async function resetPw(userid, newPw) {
    const modal = document.getElementById('resetPasswordModal');
    if (modal.dataset.credentialsIssued === 'true') return;
    const button = document.getElementById('resetPasswordButton');
    await runModalSave('resetPasswordModal', button, async () => {
        const data = await apiUserResetPassword(userid, newPw);
        if (!newPw) {
            showToast(3500,t('users.resetSuccess'));
            return;
        }
        showUserCredentials(modal, {
            username: data.username, password: data.password, loginUrl: data.loginUrl
        });
    }, undefined, !newPw);
    if (userCredentialState.has(modal)) modal.querySelector('[data-credential-copy="all"]').focus();
}

async function addNewUser() {
    const input = document.getElementById('e_userName');
    input.value = input.value.trim();
    if (!document.getElementById('newUserForm').reportValidity()) return;
    const name = input.value;
    const modal = document.getElementById('newUserModal');
    if (modal.dataset.credentialsIssued === 'true') return;
    let delivered = false;
    const saved = await runModalSave('newUserModal', document.getElementById('mb_addUser'), async () => {
        let data;
        try { data = await apiUserCreate(name); }
        catch (error) { throw new Error(error.message === 'duplicate' ? t('users.duplicate') : error.message); }
        addRowUser(data.id, data.name, data.permissions);
        if (isInternalAuth) {
            showUserCredentials(modal, {username: data.name, password: data.password, loginUrl: data.loginUrl});
            delivered = true;
        } else {
            showToast(3500,t('settings.saved'));
        }
    }, undefined, false);
    if (saved && !delivered) bootstrap.Modal.getOrCreateInstance(modal).hide();
    if (saved && delivered) modal.querySelector('[data-credential-copy="all"]').focus();
}

const PermissionDefinitions = [
    {
        key: "UserPermGuestUploads",
        bit: 1 << 8,
        icon: "bi bi-box-arrow-in-down",
        title: t("users.createRequest"),
        htmlId: userid => `perm_guest_upload_${userid}`,
        apiName: "PERM_GUEST_UPLOAD"
    },
    {
        key: "UserPermReplaceUploads",
        bit: 1 << 0,
        icon: "bi bi-recycle",
        title: t("users.replaceOwn"),
        htmlId: userid => `perm_replace_${userid}`,
        apiName: "PERM_REPLACE"
    },
    {
        key: "UserPermListOtherUploads",
        bit: 1 << 1,
        icon: "bi bi-eye",
        title: t("users.listOther"),
        htmlId: userid => `perm_list_${userid}`,
        apiName: "PERM_LIST"
    },
    {
        key: "UserPermEditOtherUploads",
        bit: 1 << 2,
        icon: "bi bi-pencil",
        title: t("users.editOther"),
        htmlId: userid => `perm_edit_${userid}`,
        apiName: "PERM_EDIT"
    },
    {
        key: "UserPermDeleteOtherUploads",
        bit: 1 << 4,
        icon: "bi bi-trash3",
        title: t("users.deleteOther"),
        htmlId: userid => `perm_delete_${userid}`,
        apiName: "PERM_DELETE"
    },
    {
        key: "UserPermReplaceOtherUploads",
        bit: 1 << 3,
        icon: "bi bi-arrow-left-right",
        title: t("users.replaceOther"),
        htmlId: userid => `perm_replace_other_${userid}`,
        apiName: "PERM_REPLACE_OTHER"
    },
    {
        key: "UserPermManageLogs",
        bit: 1 << 5,
        icon: "bi bi-card-list",
        title: t("users.manageLogs"),
        htmlId: userid => `perm_logs_${userid}`,
        apiName: "PERM_LOGS"
    },
    {
        key: "UserPermManageUsers",
        bit: 1 << 7,
        icon: "bi bi-people",
        title: t("users.manage"),
        htmlId: userid => `perm_users_${userid}`,
        apiName: "PERM_USERS"
    },
    {
        key: "UserPermManageApiKeys",
        bit: 1 << 6,
        icon: "bi bi-sliders2",
        title: t("users.manageAPI"),
        htmlId: userid => `perm_api_${userid}`,
        apiName: "PERM_API"
    }
];

function hasPermission(userPerm, permissionBit) {
    return (userPerm & permissionBit) !== 0;
}


function addRowUser(userid, name, permissions) {

    userid = sanitizeUserId(userid);

    let table = document.getElementById("usertable");
    let row = table.insertRow(0);
    row.id = "row-" + userid;
    let cellName = row.insertCell(0);
    let cellGroup = row.insertCell(1);
    let cellLastOnline = row.insertCell(2);
    let cellUploads = row.insertCell(3);
    let cellPermissions = row.insertCell(4);
    let cellActions = row.insertCell(5);

    cellName.classList.add("newUser");
    cellGroup.classList.add("newUser");
    cellLastOnline.classList.add("newUser");
    cellUploads.classList.add("newUser");
    cellPermissions.classList.add("newUser");
    cellActions.classList.add("newUser");


    cellName.innerText = name;
    cellGroup.innerText = t("user.level.2");
    cellLastOnline.innerText = t("users.neverOnline");
    cellUploads.innerText = "0";

    // Create one button group
    const btnGroup = document.createElement("div");
    btnGroup.className = "btn-group";
    btnGroup.setAttribute("role", "group");

    // Password reset button (optional)
    if (isInternalAuth) {
        const btnResetPw = document.createElement("button");
        btnResetPw.id = `pwchange-${userid}`;
        btnResetPw.type = "button";
        btnResetPw.className = "btn btn-outline-light btn-sm";
        btnResetPw.title = t("users.resetPassword");
        btnResetPw.onclick = () => showResetPwModal(userid, name);
        btnResetPw.innerHTML = `<i class="bi bi-key-fill"></i>`;
        btnGroup.appendChild(btnResetPw);
    }

    // Promote button
    const btnPromote = document.createElement("button");
    btnPromote.id = `changeRank_${userid}`;
    btnPromote.type = "button";
    btnPromote.className = "btn btn-outline-light btn-sm";
    btnPromote.title = t("users.promote");
    if (isAdmin) {
        btnPromote.onclick = () => changeRank(userid, 'ADMIN', `changeRank_${userid}`);
    } else {
        btnPromote.disabled = true;
    }
    btnPromote.innerHTML = `<i class="bi bi-chevron-double-up"></i>`;
    btnGroup.appendChild(btnPromote);

    // Delete button
    const btnDelete = document.createElement("button");
    btnDelete.id = `delete-${userid}`;
    btnDelete.type = "button";
    btnDelete.className = "btn btn-outline-danger btn-sm";
    btnDelete.title = t("common.delete");
    btnDelete.onclick = () => showDeleteUserModal(userid, name);
    btnDelete.innerHTML = `<i class="bi bi-trash3"></i>`;
    btnGroup.appendChild(btnDelete);

    // Insert button group into cellActions
    cellActions.innerHTML = '';
    cellActions.appendChild(btnGroup);

    // Permissions
     cellPermissions.innerHTML = PermissionDefinitions.map(perm => {
        let granted = "perm-notgranted";
        if (hasPermission(permissions, perm.bit)) {
            granted = "perm-granted";
        }
        const id = perm.htmlId(userid);
        let nochangeClass = "";
        if (!hasPermission(userPermissions, perm.bit)) {
            nochangeClass = "perm-nochange";
        }
        
        

        return `
        <i id="${id}"
           class="${perm.icon} ${granted} ${nochangeClass}"
           title="${perm.title}"
           data-permission="${perm.apiName}">
        </i>`;
    }).join("");

    initPermissionEditors("user", row);
    setTimeout(() => {

        cellName.classList.remove("newUser");
        cellGroup.classList.remove("newUser");
        cellLastOnline.classList.remove("newUser");
        cellUploads.classList.remove("newUser");
        cellPermissions.classList.remove("newUser");
        cellActions.classList.remove("newUser");
    }, 700);
}

function sanitizeUserId(id) {
    const numericId = id.toString().trim();
    if (!/^\d+$/.test(numericId)) {
        throw new Error(t("users.invalidID"));
    }
    return numericId;
}
