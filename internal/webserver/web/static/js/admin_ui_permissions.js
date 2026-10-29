// Permission changes are drafts until Save. The existing endpoints are
// idempotent GRANT/REVOKE operations, so failed batches can retry the remainder.
function initPermissionEditors(kind, root = document) {
    const selector = kind === 'user' ? '#usertable tr' : '#apitable tr';
    const rows = root.tagName === 'TR' ? [root] : root.querySelectorAll(selector);
    for (const row of rows) {
        labelIconButtons(row);
        const sources = [...row.querySelectorAll('[data-permission]')];
        if (!sources.length) continue;
        const cell = sources[0].parentElement;
        if (cell.dataset.permissionReady) continue;
        cell.dataset.permissionReady = 'true';
        cell.classList.add('permission-cell');
        const permissions = sources.map(source => ({
            key: source.dataset.permission, label: source.title,
            granted: source.classList.contains('perm-granted'),
            locked: source.classList.contains('perm-nochange'), source
        }));
        for (const source of sources) source.hidden = true;
        const summary = document.createElement('p');
        summary.className = 'permission-summary';
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'btn btn-outline-light btn-sm';
        button.textContent = t('permissions.review');
        const refresh = () => {
            const names = permissions.filter(p => p.granted).map(p => p.label);
            summary.textContent = names.length ? names.join(' · ') : t('permissions.none');
        };
        button.onclick = () => openPermissionEditor(kind, row.id.slice(4), row.cells[0].textContent.trim(), permissions, refresh, button);
        cell.append(summary, button);
        refresh();
    }
}

function permissionChangeOrder(permissions) {
    // Revoke dependent access before its prerequisite; grant the prerequisite
    // before dependent access. Other permissions retain their displayed order.
    const weight = p => p.key === 'PERM_REPLACE_OTHER' && !p.checked ? -1 :
        p.key === 'PERM_REPLACE_OTHER' && p.checked ? 1 : 0;
    return permissions.filter(p => !p.locked && p.checked !== p.granted).sort((a,b) => weight(a)-weight(b));
}

async function applyPermissionChanges(kind, id, permissions, onSaved) {
    for (const permission of permissionChangeOrder(permissions)) {
        await (kind === 'user' ? apiUserModify : apiAuthModify)(id, permission.key, permission.checked ? 'GRANT' : 'REVOKE');
        permission.granted = permission.checked;
        onSaved(permission);
    }
}

function openPermissionEditor(kind, id, name, permissions, refresh, trigger) {
    const element = document.getElementById('permissionModal');
    const fields = document.getElementById('permissionFields');
    const save = document.getElementById('permissionSave');
    const error = document.getElementById('permissionError');
    const changes = document.getElementById('permissionChanges');
    document.getElementById('permissionSubject').textContent = name;
    fields.replaceChildren();
    error.textContent = '';
    const draft = permissions.map(permission => ({...permission, checked: permission.granted}));
    const own = draft.find(p => p.key === 'PERM_REPLACE');
    const other = draft.find(p => p.key === 'PERM_REPLACE_OTHER');
    if (kind === 'user' && other && own && own.locked && !own.granted) other.locked = true;
    if (kind === 'user' && other && own && other.locked && other.granted) own.locked = true;
    const update = () => {
        const count = permissionChangeOrder(draft).length;
        changes.textContent = count ? t('permissions.changes',{count}) : t('permissions.noChanges');
        save.disabled = !count;
    };
    for (const permission of draft) {
        const label = document.createElement('label');
        label.className = 'permission-option';
        const input = document.createElement('input');
        input.type = 'checkbox';
        input.className = 'form-check-input';
        input.checked = permission.checked;
        input.disabled = permission.locked;
        permission.input = input;
        const text = document.createElement('span');
        text.textContent = permission.label;
        label.append(input,text);
        if (permission.locked) {
            const reason = document.createElement('small');
            reason.className = 'text-muted';
            reason.textContent = t('permissions.locked');
            text.append(document.createElement('br'),reason);
        }
        input.onchange = () => {
            permission.checked = input.checked;
            if (kind === 'user' && own && other) {
                if (permission === other && other.checked && !own.locked) own.checked = own.input.checked = true;
                if (permission === own && !own.checked && !other.locked) other.checked = other.input.checked = false;
            }
            update();
        };
        fields.appendChild(label);
    }
    if (kind === 'user') {
        const help = document.createElement('p');
        help.className = 'text-muted small mt-2';
        help.textContent = t('permissions.dependency');
        fields.appendChild(help);
    }
    update();
    element.addEventListener('hidden.bs.modal', () => trigger.isConnected && trigger.focus(), {once:true});
    save.onclick = async () => {
        let saved = 0;
        await runModalSave('permissionModal', save, async () => {
            try {
                await applyPermissionChanges(kind, id, draft, permission => {
                    const original = permissions.find(p => p.key === permission.key);
                    original.granted = permission.granted;
                    original.source.classList.toggle('perm-granted', permission.granted);
                    original.source.classList.toggle('perm-notgranted', !permission.granted);
                    saved++;
                    refresh();
                });
            } catch (failure) {
                throw new Error((saved ? t('permissions.partial',{count:saved}) : t('permissions.failed')) + ' ' + failure.message);
            }
            showToast(3500,t('settings.saved'));
        }, error);
        update();
    };
    bootstrap.Modal.getOrCreateInstance(element).show();
}
