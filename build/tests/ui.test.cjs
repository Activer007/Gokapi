const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../../internal/webserver/web/static/js/all_public.js'), 'utf8');

function context(extra = '') {
    const state = { navigated: null, alerted: false };
    const sandbox = vm.createContext({ URL, document: { querySelectorAll: () => [] }, window: {
        gokapiMessages: { greeting: 'Hello {name}', 'language.busy': 'busy' },
        location: { href: 'https://example.com/share/d?id=abc&x=1#encrypted-key', assign: url => state.navigated = url },
        alert: () => state.alerted = true
    }});
    vm.runInContext(source + '\n' + extra, sandbox);
    return { sandbox, state };
}

test('language switch preserves query, encrypted hash and pathname', () => {
    const { sandbox, state } = context();
    vm.runInContext("switchLanguage('zh-CN')", sandbox);
    const url = new URL(state.navigated);
    assert.equal(url.pathname, '/share/d');
    assert.equal(url.searchParams.get('id'), 'abc');
    assert.equal(url.searchParams.get('x'), '1');
    assert.equal(url.searchParams.get('lang'), 'zh-CN');
    assert.equal(url.hash, '#encrypted-key');
});

test('invalid languages and active transfers cannot navigate', () => {
    for (const extra of ['var isUploading = true;', 'var isUploadInProgress = true;',
        "const filesMap = new Map([['id', { status: 'pending' }]]);", 'window.gokapiTransferBusy = true;', 'window.gokapiBackupPending = true;']) {
        const { sandbox, state } = context(extra);
        vm.runInContext("switchLanguage('en')", sandbox);
        assert.equal(state.navigated, null);
        assert.equal(state.alerted, true);
    }
    const { sandbox, state } = context();
    vm.runInContext("switchLanguage('invalid')", sandbox);
    assert.equal(state.navigated, null);
});

test('processing remains protected after upload queue completion', () => {
    const { sandbox, state } = context('var isUploading = false;');
    sandbox.document.querySelectorAll = () => [{ querySelector: () => null }];
    vm.runInContext("switchLanguage('en')", sandbox);
    assert.equal(state.navigated, null);
    assert.equal(state.alerted, true);
});

test('interpolation treats substituted values literally', () => {
    const { sandbox } = context();
    assert.equal(vm.runInContext("t('greeting', {name: '$&<script>'})", sandbox), 'Hello $&<script>');
    assert.equal(vm.runInContext("t('unknown')", sandbox), 'unknown');
});

test('upload error codes use translations and preserve unknown details', () => {
    const { sandbox } = context();
    sandbox.window.gokapiMessages['api.error.21'] = '配额已满';
    assert.equal(vm.runInContext("apiErrorMessage({ErrorCode:21,ErrorMessage:'quota full'})", sandbox), '配额已满');
    assert.equal(vm.runInContext("apiErrorMessage({ErrorCode:999,ErrorMessage:'specific provider detail'})", sandbox), 'specific provider detail');
});
