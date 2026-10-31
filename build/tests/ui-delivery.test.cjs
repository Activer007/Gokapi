const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../../internal/webserver/web/static/js/admin_ui_delivery.js'), 'utf8');

function deliveryContext() {
    const nodes = {};
    for (const id of ['deliveryStatus', 'deliveryListStatus', 'deliveryCreateSubmit', 'deliveryCreatePanel',
        'deliveryTitle', 'deliveryNote', 'deliveryExpiry', 'deliveryCreatedURL', 'deliveryCreated', 'deliveryRefresh',
        'deliverySelectedFiles', 'deliveryCreateContext', 'deliveryCreatedOpen']) {
        nodes[id] = {value: '', disabled: false, hidden: true, textContent: '', focus(){}, setAttribute(){}, removeAttribute(){}};
    }
    nodes.deliverySelectedFiles.children = [];
    nodes.deliverySelectedFiles.replaceChildren = function(){this.children = [];};
    nodes.deliverySelectedFiles.appendChild = function(item){this.children.push(item);};
    nodes.deliveryTitle.value = '项目资料';
    nodes.deliveryNote.value = '交付说明';
    nodes.deliveryCreateForm = {reportValidity: () => true, reset(){this.resets = (this.resets || 0) + 1;}};
    const context = vm.createContext({document: {getElementById: id => nodes[id], createElement: () => ({textContent:''})},
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
    context.openDeliveryCreate([{id:'converted-first', name:'甲.txt', e2e:false}, {id:'converted-second', name:'乙.txt', e2e:false}], '本次成功项');
    context.selectedDeliveryFiles = () => {throw new Error('collection flow must not read the ordinary-file table');};
    let creates = 0;
    let listAvailable = false;
    context.deliveryRequest = async action => {
        if (action === 'create') { creates++; throw new Error('PRIVATE_SECRET /srv/blob token=secret'); }
        if (!listAvailable) throw new Error('offline');
        return [{title: 'already-created'}];
    };
    await context.createDelivery({preventDefault(){}});
    assert.equal(nodes.deliveryCreateSubmit.disabled, true);
    assert.equal(nodes.deliveryTitle.value, '项目资料');
    assert.equal(nodes.deliveryNote.value, '交付说明');
    assert.deepEqual(nodes.deliverySelectedFiles.children.map(item => item.textContent), ['甲.txt', '乙.txt']);
    assert.equal(context.openDeliveryCreate([{id:'other-first'}, {id:'other-second'}]), false);
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
    assert.equal(nodes.deliveryCreateForm.resets, undefined);
});

test('explicit converted-share sources stay frozen and visible after a server rejection', async () => {
    const {context, nodes} = deliveryContext();
    const files = [{id:'share-one',name:'<img src=x onerror=bad>',e2e:false}, {id:'share-two',name:'资料二.txt',e2e:false}];
    assert.equal(context.openDeliveryCreate(files, '本次已转换的普通分享'), true);
    files[0].id = 'mutated-source';
    context.selectedDeliveryFiles = () => {throw new Error('must use the confirmed snapshot');};
    const calls = [];
    context.deliveryRequest = async (action, payload) => {
        calls.push({action, payload});
        throw Object.assign(new Error('PRIVATE_TOKEN /srv/blob'), {status:400});
    };
    await context.createDelivery({preventDefault(){}});
    assert.deepEqual(Array.from(calls[0].payload.fileIds), ['share-one', 'share-two']);
    assert.equal(nodes.deliverySelectedFiles.children[0].textContent, '<img src=x onerror=bad>');
    assert.equal(nodes.deliveryCreateContext.textContent, '本次已转换的普通分享');
    assert.equal(nodes.deliveryCreatePanel.hidden, false);
    assert.equal(nodes.deliveryTitle.value, '项目资料');
    assert.equal(nodes.deliveryNote.value, '交付说明');
    assert.equal(nodes.deliveryCreateForm.resets, undefined);
    assert.equal(nodes.deliveryCreateSubmit.disabled, false);
    assert.match(nodes.deliveryStatus.textContent, /delivery.errorInvalid/);
    assert.doesNotMatch(nodes.deliveryStatus.textContent, /PRIVATE_TOKEN|srv/);
});

test('a list query started before a lost creation response cannot unlock another submission', async () => {
    const {context, nodes} = deliveryContext();
    context.openDeliveryCreate([{id:'converted-a',e2e:false}, {id:'converted-b',e2e:false}]);
    let finishOldList;
    let lists = 0;
    context.deliveryRequest = async action => {
        if (action === 'create') throw new TypeError('response lost');
        if (++lists === 1) return new Promise(resolve => {finishOldList = resolve;});
        return [{title:'committed while the old query was in flight'}];
    };
    const oldList = context.refreshDeliveries();
    await context.createDelivery({preventDefault(){}});
    finishOldList([]);
    await oldList;
    assert.equal(nodes.deliveryCreateSubmit.disabled,true);
    assert.match(nodes.deliveryStatus.textContent,/delivery.createUncertain/);
    assert.equal(nodes.deliveryTitle.value,'项目资料');
    await context.refreshDeliveries();
    assert.equal(nodes.deliveryCreateSubmit.disabled,false);
    assert.match(nodes.deliveryStatus.textContent,/delivery.inspectBeforeRetry/);
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
