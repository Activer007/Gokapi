const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const js = path.join(__dirname, '../../internal/webserver/web/static/js');
const templates = path.join(__dirname, '../../internal/webserver/web/templates');
const plain = value => JSON.parse(JSON.stringify(value));
const tick = () => new Promise(resolve => setImmediate(resolve));

function summary(changes = {}) {
    return {available:true, usedBytes:1024, usedFiles:4, limitBytes:4096, limitFiles:10, asOf:1793430000, ...changes};
}

function node(extra = {}) {
    return Object.assign({textContent:'', value:'', dataset:{}, attributes:{}, disabled:false, hidden:false,
        classList:{add(){},remove(){},toggle(){}},
        setAttribute(key,value){this.attributes[key]=String(value);}, removeAttribute(key){delete this.attributes[key];}
    }, extra);
}

function quotaContext(withPanel = true) {
    const nodes = {};
    for (const id of ['quotaSummary','quotaFiles','quotaBytes','quotaUpdatedAt','quotaStatus','quotaRefresh']) nodes[id] = node();
    nodes.quotaFiles.textContent = nodes.quotaBytes.textContent = '—';
    nodes.deliveryTitle = node({value:'未完成的交付标题'});
    nodes.deliveryNote = node({value:'不要丢失输入'});
    nodes['collapse-filelist-r'] = {shareLinks:new Map([['source',{id:'share-id'}]]),
        shareBatch:[{id:'source',name:'资料.txt'}],deliveryChoices:new Map([['source',false]])};
    if (!withPanel) delete nodes.quotaSummary;
    const context = vm.createContext({document:{getElementById:id => nodes[id] ?? null,
        documentElement:{lang:'en'}, addEventListener(){}},
        t:(key,values={}) => key+JSON.stringify(values),
        console:{error(){},warn(){},log(){}}, Intl, Date,
        setTimeout, clearTimeout,
        setInterval(){throw new Error('personal quota must not poll');}});
    for (const file of ['admin_api.js', 'admin_ui_quota.js']) {
        vm.runInContext(fs.readFileSync(path.join(js,file),'utf8'),context);
    }
    return {context,nodes};
}

test('quota snapshots reject unavailable or unsafe numbers without inventing unlimited capacity', () => {
    const {context} = quotaContext();
    const source = summary({otherUsers:{secret:100},reservedBytes:999,instanceBytes:123});
    assert.deepEqual(plain(context.quotaSummarySnapshot(source)), summary());
    assert.equal(context.quotaSummarySnapshot(summary({usedFiles:0,usedBytes:0})).usedFiles,0);
    assert.equal(context.quotaSummarySnapshot(summary({usedFiles:11})).usedFiles,11);
    for (const input of [summary({available:false}), summary({usedBytes:Number.MAX_SAFE_INTEGER+1}),
        summary({usedFiles:-1}), summary({limitBytes:0}), summary({asOf:Number.MAX_SAFE_INTEGER})]) {
        assert.equal(context.quotaSummarySnapshot(input),null);
    }
    assert.match(context.formatQuotaBytes(1024),/KiB/);
    assert.match(context.formatQuotaBytes(1024**3),/GiB/);
});

test('failures keep confirmed usage and time as stale, and authentication loss stops further reads', async () => {
    const {context,nodes} = quotaContext();
    let reads = 0;
    context.apiQuotaSummary = async () => {reads++;return summary();};
    await context.initQuotaSummary();
    assert.match(nodes.quotaFiles.textContent,/4\s*\/\s*10/);
    assert.match(nodes.quotaBytes.textContent,/1.*KiB.*4.*KiB/);
    const files = nodes.quotaFiles.textContent, bytes = nodes.quotaBytes.textContent;
    const stamp = nodes.quotaUpdatedAt.textContent, dateTime = nodes.quotaUpdatedAt.dateTime;
    const panel = nodes['collapse-filelist-r'], links = panel.shareLinks, batch = panel.shareBatch, choices = panel.deliveryChoices;
    context.apiQuotaSummary = async () => {reads++;throw Object.assign(new Error('SENSITIVE /srv/config apikey=secret'),{status:503});};
    await context.refreshQuotaSummary();
    assert.equal(nodes.quotaFiles.textContent,files);
    assert.equal(nodes.quotaBytes.textContent,bytes);
    assert.equal(nodes.quotaUpdatedAt.textContent,stamp);
    assert.equal(nodes.quotaUpdatedAt.dateTime,dateTime);
    assert.equal(nodes.quotaSummary.dataset.state,'stale');
    assert.equal(nodes['collapse-filelist-r'],panel);
    assert.equal(panel.shareLinks,links);
    assert.equal(panel.shareBatch,batch);
    assert.equal(panel.deliveryChoices,choices);
    assert.equal(nodes.deliveryTitle.value,'未完成的交付标题');
    assert.equal(nodes.deliveryNote.value,'不要丢失输入');
    assert.doesNotMatch(nodes.quotaStatus.textContent,/SENSITIVE|srv|apikey/);
    vm.runInContext('storedTokens.set("PERM_VIEW", {key:"private-token",expiry:9999999999})',context);
    context.apiQuotaSummary = async () => {reads++;throw Object.assign(new Error('forbidden'),{status:403});};
    await context.refreshQuotaSummary();
    assert.equal(nodes.quotaRefresh.disabled,true);
    assert.equal(vm.runInContext('storedTokens.has("PERM_VIEW")',context),false);
    const stopped = reads;
    await context.refreshQuotaSummary();
    await context.refreshQuotaAfterMutation();
    assert.equal(reads,stopped);
    assert.equal(nodes.quotaFiles.textContent,files);
    const fresh = quotaContext();
    fresh.context.apiQuotaSummary = async () => {throw Object.assign(new Error('offline'),{status:503});};
    await fresh.context.initQuotaSummary();
    assert.doesNotMatch(fresh.nodes.quotaFiles.textContent,/0\s*\/\s*0|unlimited/i);
    assert.doesNotMatch(fresh.nodes.quotaBytes.textContent,/0\s*\/\s*0|unlimited/i);
});

test('manual reads are single-flight and a confirmed write during a read causes one trailing refresh', async () => {
    const {context,nodes} = quotaContext();
    const completions = [];
    let reads = 0;
    context.apiQuotaSummary = () => {reads++;return new Promise(resolve => completions.push(resolve));};
    const first = context.refreshQuotaSummary();
    const duplicate = context.refreshQuotaSummary();
    const mutation = context.refreshQuotaAfterMutation();
    context.refreshQuotaAfterMutation();
    assert.equal(reads,1);
    assert.equal(nodes.quotaRefresh.disabled,true);
    completions[0](summary({usedFiles:1}));
    await tick();
    assert.equal(reads,2);
    completions[1](summary({usedFiles:2,asOf:1793430001}));
    await Promise.all([first,duplicate,mutation]);
    assert.match(nodes.quotaFiles.textContent,/2\s*\/\s*10/);
    assert.equal(nodes.quotaRefresh.disabled,false);
    const absent = quotaContext(false);
    absent.context.apiQuotaSummary = () => {throw new Error('public or unrelated pages must not read quota');};
    await absent.context.initQuotaSummary();
});

test('ordinary uploads refresh quota only after authoritative file metadata arrives', async () => {
    const {context} = quotaContext();
    vm.runInContext(fs.readFileSync(path.join(js,'admin_ui_upload.js'),'utf8'),context);
    let finish, refreshes = 0, added = 0;
    context.refreshQuotaAfterMutation = () => {refreshes++;};
    context.apiFilesListById = () => new Promise(resolve => {finish=resolve;});
    context.addRow = () => {added++;};
    context.notifyWorker = () => {};
    context.dropzoneGetFile = () => null;
    context.requestFileInfo('committed','upload-id');
    assert.equal(refreshes,0);
    finish({Id:'committed'});
    await tick();
    assert.equal(added,1);
    assert.equal(refreshes,1);
    context.apiFilesListById = async () => {throw new Error('metadata not confirmed');};
    context.requestFileInfo('unknown','other-upload');
    await tick();
    assert.equal(refreshes,1);
});

test('a conversion batch refreshes once while successful single-file retries refresh separately', async () => {
    const {context,nodes} = quotaContext();
    const fields = ['a','b','c'].map(value => node({value,checked:true,dataset:{name:`${value}.txt`}}));
    const panel = nodes['collapse-filelist-r'] = {querySelectorAll:() => fields};
    nodes['share-batch-status-r'] = node();
    for (const field of fields) {
        const id = field.value;
        for (const stem of ['share-error-','share-result-','share-url-','share-copy-']) nodes[stem+id] = node();
        nodes['button-share-'+id] = node();
        nodes['cell-listupload-'+id] = {dataset:{requestId:'r'},querySelector:() => field};
    }
    vm.runInContext(fs.readFileSync(path.join(js,'admin_ui_filerequest.js'),'utf8'),context);
    context.updateCollectionShareResults = () => {};
    let refreshes = 0, failed = false;
    context.refreshQuotaAfterMutation = () => {refreshes++;};
    context.apiCollectionFileShare = async id => {
        if (id === 'b' && !failed) {failed=true;throw Object.assign(new Error('quota'),{code:21,status:413});}
        return {Id:'share-'+id,UrlDownload:'https://example.test/d?id=share-'+id,IsEndToEndEncrypted:false};
    };
    await context.shareSelectedCollectionFiles('r',node({textContent:'Create shares'}));
    assert.equal(refreshes,1);
    assert.equal(panel.shareBatchRunning,false);
    assert.equal(nodes['button-share-a'].dataset.shared,'true');
    assert.equal(nodes['button-share-c'].dataset.shared,'true');
    assert.notEqual(nodes['button-share-b'].dataset.shared,'true');
    await context.shareCollectionFile('b',nodes['button-share-b']);
    assert.equal(refreshes,2);
    assert.equal(nodes['button-share-b'].dataset.shared,'true');
});

test('the authenticated quota request and shared panel stay within the two management surfaces', async () => {
    const {context} = quotaContext();
    const requests = [], permissions = [];
    context.getToken = async permission => {permissions.push(permission);return 'private-view-token';};
    context.fetch = async (url,options) => {
        requests.push({url,options});
        return {ok:true,json:async () => summary()};
    };
    await context.apiQuotaSummary();
    assert.deepEqual(permissions,['PERM_VIEW']);
    assert.equal(requests[0].url,'./api/quota/summary');
    assert.equal(requests[0].options.method,'GET');
    assert.equal(requests[0].options.cache,'no-store');
    assert.equal(requests[0].options.headers.apikey,'private-view-token');
    assert.equal(requests[0].options.body,undefined);
    const admin = fs.readFileSync(path.join(templates,'html_admin.tmpl'),'utf8');
    const collections = fs.readFileSync(path.join(templates,'html_uploadrequest.tmpl'),'utf8');
    const sharedName = [...admin.matchAll(/template\s+"(quota[^"\s]*)"/g)].map(match => match[1])[0];
    assert.ok(sharedName,'ordinary management page should render the shared quota panel');
    assert.ok(collections.includes(`template "${sharedName}"`));
    assert.match(admin,/initQuotaSummary\(\)/);
    assert.match(collections,/initQuotaSummary\(\)/);
    for (const file of ['html_delivery.tmpl','html_download.tmpl','html_public_upload.tmpl']) {
        const source = fs.readFileSync(path.join(templates,file),'utf8');
        assert.equal(source.includes(`template "${sharedName}"`),false,`${file} must not show personal quota`);
    }
});
