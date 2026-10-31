// This file contains shared JS code for all admin views
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...


try {
    var clipboard = new ClipboardJS('.copyurl');
    clipboard.on('success', event => {
        event.clearSelection();
        showToast(3500);
    });
    clipboard.on('error', event => showClipboardFallback(event.trigger));
} catch (ignored) {
    if (typeof document.addEventListener === 'function') {
        document.addEventListener('click', event => {
            const trigger = event.target.closest('.copyurl');
            if (trigger) showClipboardFallback(trigger);
        });
    }
}

function showClipboardFallback(trigger, text) {
    // Passwords can be copied from inside another modal. Keep its focus trap
    // and context instead of stacking Bootstrap modals.
    const parentModal = trigger && trigger.closest('.modal.show');
    if (parentModal) {
        let fallback = parentModal.querySelector('.clipboard-fallback-inline');
        if (!fallback) {
            fallback = document.createElement('div');
            fallback.className = 'clipboard-fallback-inline mt-3 text-start';
            const label = document.createElement('label');
            label.className = 'form-label';
            label.textContent = t('clipboard.failed');
            const field = document.createElement('input');
            field.className = 'form-control';
            field.readOnly = true;
            label.appendChild(field);
            fallback.appendChild(label);
            parentModal.querySelector('.modal-body').appendChild(fallback);
            parentModal.addEventListener('hidden.bs.modal', () => fallback.remove(), {once: true});
        }
        const field = fallback.querySelector('input');
        field.value = text === undefined ? trigger.dataset.clipboardText : text;
        field.focus();
        field.select();
        return;
    }
    const element = document.getElementById('clipboardFallback');
    const input = document.getElementById('clipboardFallbackValue');
    input.value = text === undefined ? trigger.dataset.clipboardText : text;
    element.addEventListener('shown.bs.modal', () => {
        input.focus();
        input.select();
    }, {once: true});
    element.addEventListener('hidden.bs.modal', () => {
        input.value = '';
        if (trigger && trigger.isConnected) trigger.focus();
    }, {once: true});
    bootstrap.Modal.getOrCreateInstance(element).show();
}

async function copySensitiveText(text, trigger, successMessage) {
    try {
        await navigator.clipboard.writeText(text);
        showToast(3500, successMessage);
    } catch (error) {
        showClipboardFallback(trigger, text);
    }
}

function formatShareLinks(entries) {
    return entries.map(entry => `${entry.name}\n${entry.url}`).join('\n\n');
}

async function copyShareLinks(entries, trigger) {
    if (trigger.disabled) return;
    if (!entries.length) {
        showToast(3500, t('sharing.noSelection'));
        return;
    }
    trigger.disabled = true;
    try {
        await copySensitiveText(formatShareLinks(entries), trigger, t('sharing.linksCopied', {count: entries.length}));
    } finally {
        trigger.disabled = false;
    }
}

var toastId;

function labelIconButtons(root = document) {
    for (const button of root.querySelectorAll('button[title]')) {
        if (!button.textContent.trim() && !button.hasAttribute('aria-label')) button.setAttribute('aria-label', button.title);
    }
}
if (typeof document.addEventListener === 'function') document.addEventListener('DOMContentLoaded', () => labelIconButtons());

function showToast(timeout, text) {
    let notification = document.getElementById("toastnotification");
    if (!notification) return;
    if (typeof text !== 'undefined')
        notification.innerText = text;
    else
        notification.innerText = notification.dataset.default;
    notification.classList.add("show");

    clearTimeout(toastId);
    toastId = setTimeout(() => {
        hideToast();
    }, timeout);
}

function hideToast() {
    document.getElementById("toastnotification").classList.remove("show");
}


var calendarInstance = null;

function tableLanguage() {
    return {
        search: t('table.search'), zeroRecords: t('table.noMatches'),
        lengthMenu: t('table.length'), infoFiltered: t('table.filtered'),
        processing: t('table.processing'), loadingRecords: t('common.loading'),
        paginate: {first: t('table.first'), previous: t('table.previous'), next: t('table.next'), last: t('table.last')},
        aria: {sortAscending: t('table.sortAscending'), sortDescending: t('table.sortDescending')}
    };
}

function calendarLanguage() {
    if (document.documentElement.lang !== 'zh-CN') return 'default';
    return {
        weekdays: {shorthand: t('calendar.weekdaysShort').split('|'), longhand: t('calendar.weekdaysLong').split('|')},
        months: {shorthand: t('calendar.months').split('|'), longhand: t('calendar.months').split('|')},
        firstDayOfWeek: 1, rangeSeparator: t('calendar.range'),
        scrollTitle: t('calendar.scroll'), toggleTitle: t('calendar.toggle'),
        yearAriaLabel: t('calendar.year'), monthAriaLabel: t('calendar.month'),
        hourAriaLabel: t('calendar.hour'), minuteAriaLabel: t('calendar.minute')
    };
}

function createCalendar(element, timestamp) {
    const expiryDate = new Date(timestamp * 1000);

    calendarInstance = flatpickr(document.getElementById(element), {
        locale: calendarLanguage(),
        enableTime: true,
        dateFormat: 'U', // Unix timestamp
        altInput: true,
        altFormat: 'Y-m-d H:i',
        allowInput: true,
        time_24hr: true,
        defaultDate: expiryDate,
        minDate: 'today',
    });
    // Flatpickr replaces the visible input; preserve its accessible name/help.
    const original = document.getElementById(element);
    for (const attribute of ['aria-label', 'aria-describedby']) {
        if (original.hasAttribute(attribute)) {
            calendarInstance.altInput.setAttribute(attribute, original.getAttribute(attribute));
        }
    }
}


function handleEditCheckboxChange(checkbox) {
    var targetElement = document.getElementById(checkbox.getAttribute("data-toggle-target"));
    var timestamp = checkbox.getAttribute("data-timestamp");

    if (checkbox.checked) {
        targetElement.classList.remove("disabled");
        targetElement.removeAttribute("disabled");
        if (timestamp != null) {
            calendarInstance._input.disabled = false;
        }
    } else {
        if (timestamp != null) {
            calendarInstance._input.disabled = true;
        }
        targetElement.classList.add("disabled");
        targetElement.setAttribute("disabled", true);
    }
}

function downloadFileWithPresign(id) {
    apiFilesListDownloadSingle(id)
        .then(data => {
            if (!data.hasOwnProperty("downloadUrl")) {
                throw new Error(t("presign.failed"));
            }
            const a = document.createElement('a');
            a.href = data.downloadUrl;
            a.style.display = 'none';

            document.body.appendChild(a);
            a.click();
            a.remove();
        })
        .catch(error => {
            alert(t("download.failed") + error);
            console.error('Error:', error);
        });
}

function downloadFilesZipWithPresign(ids, filename) {
    apiFilesListDownloadZip(ids, filename)
        .then(data => {
            if (!data.hasOwnProperty("downloadUrl")) {
                throw new Error(t("presign.failed"));
            }
            const a = document.createElement('a');
            a.href = data.downloadUrl;
            a.style.display = 'none';

            document.body.appendChild(a);
            a.click();
            a.remove();
        })
        .catch(error => {
            alert(t("download.failed") + error);
            console.error('Error:', error);
        });
}

/**
 * doLogout
 *
 * Called by the Logout link's onclick. Notifies the SharedWorker (if active)
 * to drop this port – and close the SSE connection if no other tabs remain –
 * then navigates to ./logout. Using an explicit message here is more precise
 * than beforeunload, which would fire on every navigation (refresh, back, etc.)
 * and could prematurely tear down the worker while other tabs are still open.
 */
function doLogout(event) {
    if (typeof sseWorkerPort !== "undefined" && sseWorkerPort !== null) {
        // "shutdown" tells the worker to close the SSE connection and notify
        // all other tabs, not just this one, since the session is now invalid
        // for everyone. Each tab's onmessage handler will redirect to ./login.
        sseWorkerPort.postMessage({ type: "shutdown" });
    }
    window.location.href = "./logout";
}
