const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const base = path.join(__dirname, '../../internal/webserver/web/static/js');

test('clipboard only reports confirmed success; rejection offers manual copy', async () => {
    const callbacks = {};
    const context = vm.createContext({
        ClipboardJS: function() { this.on = (name, callback) => callbacks[name] = callback; },
        document: {}, navigator: {clipboard: {writeText: async () => {throw new Error('denied');}}}
    });
    vm.runInContext(fs.readFileSync(path.join(base, 'admin_ui_allPages.js'), 'utf8'), context);
    const notices = [], fallbacks = [];
    context.showToast = (...args) => notices.push(args);
    context.showClipboardFallback = (...args) => fallbacks.push(args);
    assert.equal(notices.length, 0);
    const trigger = {id:'copy'};
    callbacks.error({trigger});
    assert.equal(notices.length, 0);
    assert.equal(fallbacks[0][0], trigger);
    let cleared = false;
    callbacks.success({clearSelection: () => cleared = true});
    assert.equal(cleared, true);
    assert.equal(notices.length, 1);
    await context.copySensitiveText('fixture', trigger, 'Copied');
    assert.equal(notices.length, 1);
    assert.deepEqual(fallbacks[1], [trigger, 'fixture']);
    context.navigator.clipboard.writeText = async () => {};
    await context.copySensitiveText('fixture', trigger, 'Copied');
    assert.deepEqual(notices[1], [3500, 'Copied']);
});

test('key deletion waits for confirmation, blocks duplicates and recovers from failure', async () => {
    const elements = new Map();
    const el = id => {
        if (!elements.has(id)) elements.set(id, {
            id, textContent:id === 'friendlyname-fixture' ? '<Team key>' : '',
            disabled:false, isConnected:true, listeners:{},
            addEventListener(name, fn) {this.listeners[name] = fn;},
            removeEventListener(name) {delete this.listeners[name];},
            focus() {}, remove() {this.removed = true;}
        });
        return elements.get(id);
    };
    let calls = 0, resolve, reject, hidden = false;
    const context = vm.createContext({
        document:{getElementById:el}, window:{}, t:key=>key, showToast:()=>{},
        bootstrap:{Modal:{getOrCreateInstance:()=>({show(){},hide(){hidden=true;}})}},
        apiAuthDelete:()=>{calls++; return new Promise((yes,no)=>{resolve=yes;reject=no;});}
    });
    vm.runInContext(fs.readFileSync(path.join(base, 'admin_ui_api.js'), 'utf8'), context);
    context.deleteApiKey('fixture');
    assert.equal(calls, 0);
    assert.equal(el('deleteApiName').textContent, '<Team key>');
    const pending = el('confirmDeleteApi').onclick();
    await el('confirmDeleteApi').onclick();
    assert.equal(calls, 1);
    let prevented = false;
    el('deleteApiModal').listeners['hide.bs.modal']({preventDefault(){prevented=true;}});
    assert.equal(prevented, true);
    reject(new Error('temporary failure')); await pending;
    assert.equal(hidden, false);
    assert.equal(el('confirmDeleteApi').disabled, false);
    assert.match(el('deleteApiError').textContent, /temporary failure/);
    assert.equal(el('row-fixture').removed, undefined);
    const retry = el('confirmDeleteApi').onclick(); resolve({}); await retry;
    assert.equal(calls, 2);
    assert.equal(hidden, true);
    assert.equal(el('row-fixture').removed, true);
});
