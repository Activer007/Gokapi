const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../../internal/webserver/web/static/js/admin_ui_delivery.js'), 'utf8');

function deliveryContext() {
    const nodes = {};
    for (const id of ['deliveryStatus', 'deliveryListStatus', 'deliveryCreateSubmit', 'deliveryCreatePanel',
        'deliveryTitle', 'deliveryNote', 'deliveryExpiry', 'deliveryCreatedURL', 'deliveryCreated', 'deliveryRefresh']) {
        nodes[id] = {value: '', disabled: false, hidden: true, textContent: '', focus(){}, setAttribute(){}, removeAttribute(){}};
    }
    nodes.deliveryTitle.value = '项目资料';
    nodes.deliveryNote.value = '交付说明';
    nodes.deliveryCreateForm = {reportValidity: () => true, reset(){this.resets = (this.resets || 0) + 1;}};
    const context = vm.createContext({document: {getElementById: id => nodes[id]},
        t: (key, values = {}) => key + JSON.stringify(values), console});
    vm.runInContext(source, context);
    context.selectedDeliveryFiles = () => [{id: 'first', e2e: false}, {id: 'second', e2e: false}, {id: 'third', e2e: false}];
    context.renderDeliveries = items => {context.rendered = items;};
    return {context, nodes};
}

test('three selected IDs create one list; duplicate clicks do not submit twice', async () => {
    const {context, nodes} = deliveryContext();
    const calls = [];
    let finish;
    context.deliveryRequest = async (action, payload) => {
        calls.push({action, payload});
        if (action === 'create') return new Promise(resolve => {finish = resolve;});
        return [{title: '项目资料'}];
    };
    const pending = context.createDelivery({preventDefault(){}});
    await context.createDelivery({preventDefault(){}});
    assert.equal(calls.length, 1);
    assert.equal(nodes.deliveryCreateSubmit.disabled, true);
    assert.deepEqual(JSON.parse(JSON.stringify(calls[0].payload)), {
        title: '项目资料', note: '交付说明', fileIds: ['first', 'second', 'third'], expiresAt: 0
    });
    finish({url: 'https://example.test/delivery/high-entropy-token'});
    await pending;
    assert.equal(calls[1].action, 'list');
    assert.equal(nodes.deliveryCreatedURL.value, 'https://example.test/delivery/high-entropy-token');
    assert.equal(nodes.deliveryCreated.hidden, false);
    assert.equal(nodes.deliveryCreatePanel.hidden, true);
    assert.equal(nodes.deliveryCreateSubmit.disabled, false);
    assert.equal(nodes.deliveryCreateForm.resets, 1);
});

test('uncertain creation waits for a successful list refresh before allowing retry', async () => {
    const {context, nodes} = deliveryContext();
    let creates = 0;
    let listAvailable = false;
    context.deliveryRequest = async action => {
        if (action === 'create') { creates++; throw new Error('PRIVATE_SECRET /srv/blob token=secret'); }
        if (!listAvailable) throw new Error('offline');
        return [{title: 'already-created'}];
    };
    await context.createDelivery({preventDefault(){}});
    assert.equal(nodes.deliveryCreateSubmit.disabled, true);
    assert.match(nodes.deliveryStatus.textContent, /delivery.createUncertain/);
    assert.doesNotMatch(nodes.deliveryStatus.textContent, /PRIVATE_SECRET|token|srv/);
    await context.createDelivery({preventDefault(){}});
    assert.equal(creates, 1);
    await context.refreshDeliveries();
    assert.equal(nodes.deliveryCreateSubmit.disabled, true);
    listAvailable = true;
    await context.refreshDeliveries();
    assert.equal(nodes.deliveryCreateSubmit.disabled, false);
    assert.match(nodes.deliveryStatus.textContent, /delivery.inspectBeforeRetry/);
    assert.equal(context.rendered[0].title, 'already-created');
});

test('E2E, invalid selection and elapsed expiry are rejected before sending a create request', async () => {
    const {context, nodes} = deliveryContext();
    context.deliveryRequest = () => {throw new Error('must not request');};
    context.selectedDeliveryFiles = () => [{id:'e2e', e2e:true}, {id:'ordinary', e2e:false}];
    await context.createDelivery({preventDefault(){}});
    assert.match(nodes.deliveryStatus.textContent, /delivery.noE2E/);
    context.selectedDeliveryFiles = () => [{id:'ordinary', e2e:false}];
    await context.createDelivery({preventDefault(){}});
    assert.match(nodes.deliveryStatus.textContent, /delivery.selectionLimit/);
    context.selectedDeliveryFiles = () => [{id:'a', e2e:false}, {id:'b', e2e:false}];
    nodes.deliveryExpiry.value = '2000-01-01T00:00';
    await context.createDelivery({preventDefault(){}});
    assert.match(nodes.deliveryStatus.textContent, /delivery.futureExpiry/);
});

test('management requests reuse scoped permission tokens and JSON bodies', async () => {
    const {context} = deliveryContext();
    const permissions = [], calls = [];
    context.getToken = async permission => {permissions.push(permission); return 'private-auth-token';};
    context.fetch = async (url, options) => {
        calls.push({url, options});
        return {ok: true, json: async () => []};
    };
    await context.deliveryRequest('list');
    await context.deliveryRequest('create', {title: '资料', fileIds: ['a', 'b'], expiresAt: 0});
    await context.deliveryRequest('revoke', {token: 'public-capability'});
    assert.deepEqual(permissions, ['PERM_VIEW', 'PERM_UPLOAD', 'PERM_DELETE']);
    assert.equal(calls[0].url, './api/deliveries/list');
    assert.equal(calls[0].options.method, 'GET');
    assert.equal(calls[0].options.body, undefined);
    assert.equal(calls[1].options.headers.apikey, 'private-auth-token');
    assert.equal(JSON.parse(calls[1].options.body).title, '资料');
    assert.deepEqual(JSON.parse(calls[2].options.body), {token:'public-capability'});
});
