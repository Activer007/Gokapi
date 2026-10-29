// A small shared pending/error lifecycle for modal saves. Callers capture and
// validate values before entering; failures keep those values for correction.
async function runModalSave(id, button, action, errorElement, closeOnSuccess = true) {
    const modal = document.getElementById(id);
    if (modal.dataset.saving === 'true') return false;
    let error = errorElement || modal.querySelector('.save-error');
    if (!error) {
        error = document.createElement('p');
        error.className = 'save-error text-danger mt-3 mb-0';
        error.setAttribute('role','alert');
        modal.querySelector('.modal-body').appendChild(error);
    }
    error.textContent = '';
    const label = button.textContent;
    const enabled = [...modal.querySelectorAll('button, input, textarea, select')].filter(control => !control.disabled);
    const preventHide = event => { if (modal.dataset.saving === 'true') event.preventDefault(); };
    modal.addEventListener('hide.bs.modal',preventHide);
    modal.dataset.saving = 'true';
    modal.setAttribute('aria-busy','true');
    window.gokapiFormBusy = true;
    enabled.forEach(control => control.disabled = true);
    button.textContent = t('settings.saving');
    try {
        await action();
        modal.dataset.saving = 'false';
        if (closeOnSuccess) bootstrap.Modal.getOrCreateInstance(modal).hide();
        return true;
    } catch (failure) {
        error.textContent = failure.message || String(failure);
        return false;
    } finally {
        modal.dataset.saving = 'false';
        modal.removeAttribute('aria-busy');
        modal.removeEventListener('hide.bs.modal',preventHide);
        window.gokapiFormBusy = false;
        enabled.forEach(control => control.disabled = false);
        button.textContent = label;
    }
}
