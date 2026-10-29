// Setup has a separate static server, but uses the same server-side dictionaries.
function t(key, params) {
    let message = window.gokapiMessages[key] || key;
    for (const [name, value] of Object.entries(params || {})) {
        message = message.split('{' + name + '}').join(String(value));
    }
    return message;
}

function switchSetupLanguage(language) {
    if (language !== 'en' && language !== 'zh-CN') return;
    if (language === document.documentElement.lang) return;
    if (window.setupSubmitting) {
        window.alert(t('setup.busy'));
        return;
    }
    if (window.setupDirty && !window.confirm(t('setup.discard'))) return;
    const url = new URL(window.location.href);
    url.searchParams.set('lang', language);
    window.location.assign(url.href);
}

function setupResponseMessage(response) {
    return (response.messageKey && window.gokapiMessages[response.messageKey]) || response.result;
}
