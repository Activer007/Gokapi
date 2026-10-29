// Shared presentation for server-rendered and live file rows. Existing action
// nodes are moved, preserving E2E IDs, permission flags and event listeners.
function prepareFileActions(row) {
    const toolbar = row.querySelector('.btn-toolbar');
    if (!toolbar || toolbar.dataset.prepared) return;
    const primary = toolbar.querySelector('[id^="url-button-"]');
    const more = toolbar.querySelector('[id^="shareDropdown-"]');
    if (!primary || !more) return;
    const actions = [...toolbar.querySelectorAll('button, a')].filter(node =>
        node !== primary && !node.hasAttribute('data-bs-toggle'));
    primary.textContent = t('common.copyLink');
    primary.className = 'copyurl btn btn-outline-light btn-sm';
    primary.setAttribute('aria-label', t('common.copyLink'));
    more.className = 'btn btn-outline-light btn-sm dropdown-toggle';
    more.textContent = t('common.more');
    more.setAttribute('aria-label', t('common.more'));
    const menu = document.createElement('ul');
    menu.className = 'dropdown-menu dropdown-menu-end';
    menu.dataset.bsTheme = 'dark';
    const sourceLink = row.querySelector('[id^="url-href-"]');
    if (sourceLink) {
        const open = document.createElement('button');
        open.type = 'button';
        open.title = t('files.openPage');
        open.onclick = () => window.open(sourceLink.href, '_blank', 'noopener');
        actions.unshift(open);
    }
    for (const action of actions) {
        const disabled = action.disabled || action.classList.contains('disabled');
        const destructive = action.classList.contains('btn-outline-danger');
        const copy = action.classList.contains('copyurl');
        action.className = 'dropdown-item' + (disabled ? ' disabled' : '') +
            (destructive ? ' text-danger' : '') + (copy ? ' copyurl' : '');
        action.textContent = action.title || action.textContent;
        if (action.tagName === 'BUTTON') action.disabled = disabled;
        // Legacy QR/hotlink anchors have no href; expose keyboard semantics.
        if (action.tagName === 'A' && !action.hasAttribute('href')) {
            action.setAttribute('role', 'button');
            action.tabIndex = disabled ? -1 : 0;
            action.addEventListener('keydown', event => {
                if (!disabled && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault();
                    action.click();
                }
            });
        }
        if (disabled) action.setAttribute('aria-disabled', 'true');
        const item = document.createElement('li');
        item.appendChild(action);
        menu.appendChild(item);
    }
    const dropdown = document.createElement('div');
    dropdown.className = 'dropdown';
    dropdown.append(more, menu);
    toolbar.replaceChildren(primary, dropdown);
    toolbar.className = 'file-actions';
    toolbar.dataset.prepared = 'true';
    const protectedFile = row.querySelector('td:nth-of-type(6) .bi-key');
    if (protectedFile) {
        protectedFile.setAttribute('role', 'img');
        protectedFile.setAttribute('aria-label', protectedFile.title);
        toolbar.prepend(protectedFile);
    }
}

function initUploadSummary() {
    for (const id of ['allowedDownloads', 'expiryDays', 'password']) {
        document.getElementById(id).addEventListener('input', updateUploadSummary);
    }
    updateUploadSummary();
}

function updateUploadSummary() {
    const summary = document.getElementById('uploadSummary');
    if (!summary) return;
    const downloads = document.getElementById('allowedDownloads');
    const expiry = document.getElementById('expiryDays');
    const password = document.getElementById('password');
    const valid = [downloads, expiry].every(field => field.disabled || (field.value !== '' && field.checkValidity())) &&
        (password.disabled || password.value !== '');
    summary.textContent = valid ? t('files.summary', {
        downloads: downloads.disabled ? t('files.summaryUnlimitedDownloads') : t('files.summaryDownloads', {count: downloads.value}),
        expiry: expiry.disabled ? t('files.summaryUnlimitedTime') : t('files.summaryDays', {count: expiry.value}),
        password: password.disabled ? t('files.summaryNoPassword') : t('files.summaryPassword')
    }) : t('files.summaryInvalid');
    summary.classList.toggle('text-warning', !valid);
}
