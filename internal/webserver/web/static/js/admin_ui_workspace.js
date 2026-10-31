// Shared presentation for server-rendered and live file rows. Existing action
// nodes are moved, preserving E2E IDs, permission flags and event listeners.
function prepareFileActions(row) {
    const filename = row.querySelector('[id^="cell-name-"]');
    if (filename) decorateFileName(filename);
    prepareFileSelection(row);
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

// Keep one set of rule inputs: opening the disclosure never resets the values.
function validateUploadRules(reveal = false) {
    const fields = ['allowedDownloads', 'expiryDays', 'password'].map(id => document.getElementById(id));
    const invalid = fields.find(field => !field.disabled &&
        (field.value === '' || (field.type === 'number' && !field.checkValidity())));
    updateUploadSummary();
    if (invalid && reveal) {
        document.getElementById('uploadSettings').open = true;
        invalid.focus();
        invalid.reportValidity();
    }
    return !invalid;
}

function chooseUploadFiles() {
    const trigger = document.getElementById('selectUploadFiles');
    if (!dropzoneObject || dropzoneObject.disabled || trigger.disabled) return;
    if (validateUploadRules(true)) dropzoneObject.hiddenFileInput.click();
}

function initFileTable(table) {
    const wrapper = table.table().container();
    const filter = wrapper.querySelector('.dataTables_filter');
    const target = document.getElementById('fileSearch');
    if (filter && target) {
        target.appendChild(filter);
        const input = filter.querySelector('input');
        input.placeholder = t('files.searchPlaceholder');
        input.setAttribute('aria-label', t('files.searchPlaceholder'));
        input.setAttribute('data-allow-regular-paste', '');
    }
    updateFileCount(table);
}

function updateFileCount(table) {
    const count = document.getElementById('fileCount');
    if (count) {
        const total = table.rows().count();
        const matched = table.rows({search: 'applied'}).count();
        count.textContent = table.search() ? matched + ' / ' + total : String(total);
    }
    const clear = document.getElementById('clearFileSearch');
    if (clear) clear.hidden = !table.search();
    updateFileSelection(table);
}

function currentFileShareLink(row) {
    const copy = row.querySelector('[id^="url-button-"]');
    const url = copy?.dataset.clipboardText || row.querySelector('[id^="url-href-"]')?.getAttribute('href') || '';
    // An E2E link is usable only after the existing decryption flow supplies its key.
    return row.dataset.shareE2e === 'true' && !url.includes('#') ? '' : url;
}

function prepareFileSelection(row) {
    const cell = row.querySelector('[id^="cell-name-"]');
    if (!cell) return;
    let checkbox = cell.querySelector('[data-file-select]');
    if (!checkbox) {
        checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'form-check-input me-2';
        checkbox.setAttribute('data-file-select', '');
        checkbox.onchange = () => {
            row.dataset.shareSelected = String(checkbox.checked);
            updateFileSelection($('#maintable').DataTable());
        };
        cell.prepend(checkbox);
    }
    checkbox.setAttribute('aria-label', t('sharing.selectFile', {name: cell.querySelector('.file-name')?.textContent || cell.textContent}));
    checkbox.checked = row.dataset.shareSelected === 'true';
    checkbox.disabled = !currentFileShareLink(row) || row.classList.contains('rowDeleting');
    return checkbox;
}

function visibleShareRows(table) {
    return Array.from(table.rows({search: 'applied'}).nodes()).filter(row => !row.classList.contains('rowDeleting'));
}

function updateFileSelection(table) {
    const all = document.getElementById('selectAllFiles');
    const count = document.getElementById('fileSelectionCount');
    if (!all || !count) return;
    const boxes = visibleShareRows(table).map(prepareFileSelection).filter(box => box && !box.disabled);
    const selected = boxes.filter(box => box.checked).length;
    all.disabled = boxes.length === 0;
    all.checked = boxes.length > 0 && selected === boxes.length;
    all.indeterminate = selected > 0 && selected < boxes.length;
    count.textContent = t('sharing.selected', {count: selected});
}

function toggleFileSelection(checked) {
    const table = $('#maintable').DataTable();
    for (const row of visibleShareRows(table)) {
        const checkbox = prepareFileSelection(row);
        if (checkbox && !checkbox.disabled) row.dataset.shareSelected = String(checked);
    }
    updateFileSelection(table);
}

async function copySelectedFileLinks(trigger) {
    const table = $('#maintable').DataTable();
    const entries = [];
    for (const row of visibleShareRows(table)) {
        const checkbox = prepareFileSelection(row);
        if (!checkbox || checkbox.disabled || !checkbox.checked) continue;
        entries.push({name: row.querySelector('.file-name').textContent, url: currentFileShareLink(row)});
    }
    await copyShareLinks(entries, trigger);
}

function clearFileSearch() {
    const table = $('#maintable').DataTable();
    table.search('').draw();
    const input = document.querySelector('#fileSearch input');
    if (input) { input.value = ''; input.focus(); }
}

function decorateFileName(cell) {
    if (cell.querySelector('.file-name')) return;
    const name = cell.textContent;
    const extension = name.split('.').pop().toLowerCase();
    const icon = document.createElement('i');
    const types = {pdf:'file-earmark-pdf', png:'file-earmark-image', jpg:'file-earmark-image', jpeg:'file-earmark-image',
        gif:'file-earmark-image', webp:'file-earmark-image', svg:'file-earmark-image', zip:'file-earmark-zip',
        gz:'file-earmark-zip', mp4:'file-earmark-play', mp3:'file-earmark-music', md:'file-earmark-text', txt:'file-earmark-text'};
    icon.className = 'bi ' + 'bi-' + (types[extension] || 'file-earmark') + ' file-type-icon';
    icon.setAttribute('aria-hidden', 'true');
    const label = document.createElement('span');
    label.className = 'file-name';
    label.textContent = name;
    label.title = name;
    cell.replaceChildren(icon, label);
}
