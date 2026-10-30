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

// Adapt the existing wizard through its events, without changing validation or
// the enabled-card rules that determine which configuration fields are sent.
function enhanceSetupWizard(wizard) {
    wizard.header.append(document.getElementById('setup-languages'));
    wizard.el.attr({role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': 'setup-heading'});
    wizard.header.find('.wizard-title').attr({id: 'setup-heading', role: 'heading', 'aria-level': '1'});
    wizard.el.find('.wizard-nav-link .glyphicon').attr('aria-hidden', 'true');
    wizard.el.find('.wizard-failure, .wizard-error').attr('role', 'alert');
    wizard.el.find('.wizard-success').attr('role', 'status');

    // Keep Bootstrap's popover lifecycle, but place validation text in the
    // document flow so it cannot escape the scrollable card on small desktops.
    wizard.errorPopover = function(el, message) {
        el.popover('destroy').popover({content: message, trigger: 'manual', html: false,
            placement: 'bottom', container: false, animation: false}).popover('show');
        const tip = el.next('.popover').addClass('setup-field-error');
        const id = el.attr('id') + '-error';
        tip.attr({id: id, role: 'alert'});
        el.attr({'aria-invalid': 'true', 'aria-describedby': id});
        wizard.popovers.push(el);
        return tip;
    };
    wizard.el.on('input change', '[aria-invalid="true"]', function() {
        $(this).popover('destroy').removeAttr('aria-invalid aria-describedby');
    });

    wizard._cards.forEach(function(card) {
        const link = card.nav.find('a');
        const heading = card.el.find('h3').first().attr({tabindex: '-1', 'aria-level': '2'});
        link.attr({role: 'button', tabindex: '-1', 'aria-disabled': 'true'});
        link.on('keydown', function(event) {
            if ((event.key === 'Enter' || event.key === ' ') && card.alreadyVisited() && !card.isDisabled()) {
                event.preventDefault();
                link.trigger('click');
            }
        });
        card.on('markVisited', function() { link.attr({tabindex: '0', 'aria-disabled': 'false'}); });
        card.on('disabled', function() { link.attr({tabindex: '-1', 'aria-disabled': 'true'}).removeAttr('aria-current'); });
        card.on('deselect', function() { link.removeAttr('aria-current'); });
        card.on('validated', function() { card.el.find('[aria-invalid]').removeAttr('aria-invalid aria-describedby'); });
        card.on('invalid', function() { card.el.find('[aria-invalid="true"]').first().trigger('focus'); });
        card.on('selected', function() {
            link.attr('aria-current', 'step');
            wizard.wizardCardContainer.scrollTop(0);
            heading.trigger('focus');
        });
    });
}
