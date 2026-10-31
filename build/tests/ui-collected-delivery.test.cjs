const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const base = path.join(__dirname, '../../internal/webserver/web/static');
const plain = value => JSON.parse(JSON.stringify(value));

function element(extra = {}) {
    return Object.assign({children:[], attributes:{}, dataset:{}, value:'', textContent:'', disabled:false, hidden:false,
        setAttribute(key, value){this.attributes[key] = value;}, removeAttribute(key){delete this.attributes[key];},
        append(...items){this.children.push(...items);}, appendChild(item){this.children.push(item);},
        replaceChildren(...items){this.children = items;}, focus(){},
        querySelectorAll(selector){
            const found = [];
            for (const child of this.children) {
                if (selector === '[data-collection-delivery-select]' && 'data-collection-delivery-select' in child.attributes) found.push(child);
                found.push(...child.querySelectorAll(selector));
            }
            return found;
        }
    }, extra);
}

function collectedContext(api, requests = {r:['a','b','c'], s:['x','y']}) {
    const nodes = {}, fields = {};
    for (const [request, ids] of Object.entries(requests)) {
        fields[request] = ids.map(value => element({value, checked:true, dataset:{name:`${value}.txt`}}));
        nodes[`collapse-filelist-${request}`] = element({querySelectorAll:() => fields[request]});
        for (const stem of ['share-select-all-', 'share-batch-status-', 'share-batch-result-', 'share-batch-links-',
            'share-delivery-choices-', 'share-delivery-status-', 'share-delivery-create-']) nodes[stem+request] = element();
        for (const field of fields[request]) {
            const id = field.value;
            nodes[`button-share-${id}`] = element();
            for (const stem of ['share-error-', 'share-result-', 'share-url-', 'share-copy-']) nodes[stem+id] = element();
            nodes[`cell-listupload-${id}`] = element({dataset:{requestId:request}, querySelector:() => field});
        }
    }
    for (const id of ['deliveryStatus', 'deliveryListStatus', 'deliveryCreateSubmit', 'deliveryCreatePanel',
        'deliveryTitle', 'deliveryNote', 'deliveryExpiry', 'deliveryCreatedURL', 'deliveryCreated', 'deliveryRefresh',
        'deliverySelectedFiles', 'deliveryCreateContext', 'deliveryCreatedOpen']) nodes[id] = element();
    nodes.deliveryCreateForm = element({reportValidity:() => true, reset(){this.resets = (this.resets || 0)+1;}});
    nodes.deliveryTitle.value = '本次交付';
    const context = vm.createContext({document:{getElementById:id => nodes[id], createElement:() => element()},
        t:(key,values={}) => key+JSON.stringify(values), showToast(){}});
    for (const source of ['admin_api.js', 'admin_ui_allPages.js', 'admin_ui_filerequest.js', 'admin_ui_delivery.js']) {
        vm.runInContext(fs.readFileSync(path.join(base, 'js', source), 'utf8'), context);
    }
    context.apiCollectionFileShare = api;
    context.selectedDeliveryFiles = () => {throw new Error('collection entry must not read the ordinary file table');};
    context.renderDeliveries = items => {context.rendered = items;};
    return {context, nodes, fields, choices:request => nodes[`share-delivery-choices-${request}`].querySelectorAll('[data-collection-delivery-select]')};
}

function ordinaryShare(id) {
    // Deliberately make the URL disagree with Id: only the server's Id field is
    // the authority for delivery membership.
    return {Id:`new-share-${id}`, UrlDownload:`https://example.test/d?id=url-${id}`, IsEndToEndEncrypted:false};
}

test('current batch retains partial successes on retry and replaces scope without leaking cached or other-collection shares', async () => {
    const calls = [];
    const {context, nodes, fields, choices} = collectedContext(async id => {
        calls.push(id);
        if (id === 'b' && calls.filter(value => value === 'b').length === 1) throw new Error('quota');
        return ordinaryShare(id);
    });
    const batchButton = element({textContent:'Convert'});
    await context.shareSelectedCollectionFiles('r', batchButton);
    assert.deepEqual(calls, ['a','b','c']);
    assert.deepEqual(plain(context.selectedCollectionDeliveryFiles('r')).map(file => file.id), ['new-share-a','new-share-c']);
    assert.match(nodes['share-delivery-status-r'].textContent, /"failed":1/);
    context.createCollectionDelivery('r');
    assert.deepEqual(nodes.deliverySelectedFiles.children.map(item => item.textContent), ['a.txt','c.txt']);
    await context.shareCollectionFile('b', nodes['button-share-b']);
    assert.deepEqual(plain(context.selectedCollectionDeliveryFiles('r')).map(file => file.id), ['new-share-a','new-share-b','new-share-c']);
    // An explicit recipient selection survives a later render within this batch.
    choices('r')[1].checked = false; choices('r')[1].onchange();
    context.updateCollectionShareResults('r');
    assert.deepEqual(plain(context.selectedCollectionDeliveryFiles('r')).map(file => file.id), ['new-share-a','new-share-c']);
    fields.r[0].checked = false;
    await context.shareSelectedCollectionFiles('r', batchButton);
    assert.deepEqual(calls, ['a','b','c','b']);
    assert.deepEqual(plain(context.collectionShareLinks('r')).map(file => file.id), ['new-share-b','new-share-c']);
    assert.doesNotMatch(nodes['share-batch-links-r'].value, /url-a/);
    await context.shareSelectedCollectionFiles('s', element({textContent:'Convert other collection'}));
    context.createCollectionDelivery('s');
    const submitted = [];
    context.deliveryRequest = async (action,payload) => {
        if (action === 'create') {submitted.push(plain(payload)); return {url:'https://example.test/delivery/one-token'};}
        return [];
    };
    await context.createDelivery({preventDefault(){}});
    assert.deepEqual(submitted[0].fileIds, ['new-share-x','new-share-y']);
    assert.doesNotMatch(JSON.stringify(submitted), /url-|new-share-[abc]/);
    // Source-row retirement does not discard the confirmed successful shares.
    fields.s = [];
    context.updateCollectionShareResults('s');
    assert.equal(context.collectionShareLinks('s').length, 2);
});

test('a new single-file conversion clears the old successful batch immediately and stays unavailable after failure', async () => {
    let rejectSingle;
    const {context,nodes,fields,choices} = collectedContext(async id => {
        if (id === 'c') return new Promise((_,reject) => {rejectSingle = reject;});
        return ordinaryShare(id);
    }, {r:['a','b','c']});
    fields.r[2].checked = false;
    await context.shareSelectedCollectionFiles('r',element());
    assert.equal(choices('r').length,2);
    assert.equal(nodes['share-delivery-create-r'].disabled,false);
    assert.match(nodes['share-batch-links-r'].value,/url-a/);
    const pending = context.shareCollectionFile('c',nodes['button-share-c']);
    assert.equal(nodes['share-batch-result-r'].hidden,true);
    assert.equal(nodes['share-batch-links-r'].value,'');
    assert.equal(choices('r').length,0);
    assert.equal(nodes['share-delivery-create-r'].disabled,true);
    assert.match(nodes['share-delivery-status-r'].textContent,/delivery.collectionRunning/);
    assert.doesNotMatch(nodes['share-batch-status-r'].textContent,/allFailed|allSucceeded/);
    let opened = 0;
    context.openDeliveryCreate = () => {opened++;};
    context.createCollectionDelivery('r');
    assert.equal(opened,0);
    rejectSingle(Object.assign(new Error('unavailable'),{status:503}));
    assert.equal(await pending,false);
    assert.equal(nodes['share-batch-result-r'].hidden,true);
    assert.equal(nodes['share-batch-links-r'].value,'');
    assert.equal(nodes['share-delivery-create-r'].disabled,true);
    assert.match(nodes['share-batch-status-r'].textContent,/sharing.allFailed.*"success":0,"failed":1/);
    assert.match(nodes['share-batch-status-r'].textContent,/c.txt/);
    assert.equal(context.collectionShareLinks('r').length,0);
    assert.equal(nodes['collapse-filelist-r'].shareLinks.size,2);
});

test('only explicit ordinary-share responses are deliverable, with 2–20 chosen items and no E2E fragment in the create body', async () => {
    const {context,nodes,choices} = collectedContext(async id => {
        if (id === 'encrypted') return {Id:'e2e-share',UrlDownload:'https://example.test/d?id=e2e-share#PRIVATE_KEY',IsEndToEndEncrypted:true};
        if (id === 'unknown') return {Id:'unknown-share',UrlDownload:'https://example.test/d?id=unknown#UNKNOWN_KEY'};
        if (id === 'missing') return {UrlDownload:'https://example.test/d?id=guessed-id',IsEndToEndEncrypted:false};
        return ordinaryShare(id);
    }, {r:['a','b','encrypted','unknown','missing']});
    await context.shareSelectedCollectionFiles('r', element());
    assert.equal(choices('r').filter(field => field.disabled).length, 3);
    assert.deepEqual(plain(context.selectedCollectionDeliveryFiles('r')).map(file => file.id), ['new-share-a','new-share-b']);
    context.createCollectionDelivery('r');
    let body;
    context.deliveryRequest = async (action,payload) => {
        if (action === 'create') {body = plain(payload); return {url:'https://example.test/delivery/list'};}
        return [];
    };
    await context.createDelivery({preventDefault(){}});
    assert.deepEqual(body.fileIds, ['new-share-a','new-share-b']);
    assert.doesNotMatch(JSON.stringify(body), /PRIVATE_KEY|UNKNOWN_KEY|guessed-id|https:/);
    choices('r')[0].checked = false; choices('r')[0].onchange();
    assert.equal(nodes['share-delivery-create-r'].disabled, true);
    context.createCollectionDelivery('r');
    assert.match(nodes.deliveryStatus.textContent, /delivery.selectionLimit/);

    const many = collectedContext(async id => ordinaryShare(id), {r:Array.from({length:21},(_,index) => `file-${index}`)});
    await many.context.shareSelectedCollectionFiles('r', element());
    assert.equal(many.context.selectedCollectionDeliveryFiles('r').length,21);
    assert.equal(many.nodes['share-delivery-create-r'].disabled,true);
    many.context.createCollectionDelivery('r');
    assert.match(many.nodes.deliveryStatus.textContent,/delivery.selectionLimit/);
    many.choices('r')[20].checked=false;many.choices('r')[20].onchange();
    assert.equal(many.nodes['share-delivery-create-r'].disabled,false);
    many.context.createCollectionDelivery('r');
    assert.equal(many.nodes.deliverySelectedFiles.children.length,20);
});

test('both management pages render the shared creation and management templates once', () => {
    const templates = path.join(__dirname,'../../internal/webserver/web/templates');
    const shared = fs.readFileSync(path.join(templates,'html_delivery_admin.tmpl'),'utf8');
    for (const name of ['html_admin.tmpl','html_uploadrequest.tmpl']) {
        const page = fs.readFileSync(path.join(templates,name),'utf8');
        assert.equal((page.match(/template "delivery_create"/g)||[]).length,1);
        assert.equal((page.match(/template "delivery_management"/g)||[]).length,1);
        assert.doesNotMatch(page,/id="deliveryCreateForm"/);
    }
    assert.equal((shared.match(/id="deliveryCreateForm"/g)||[]).length,1);
    assert.match(shared,/id="deliverySelectedFiles"/);
    assert.match(shared,/id="deliveryCreatedOpen"[^>]+rel="noopener noreferrer"/);
});
