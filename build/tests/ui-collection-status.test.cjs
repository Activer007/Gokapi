const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const base = path.join(__dirname, '../../internal/webserver/web/static/js');
const source = () => fs.readFileSync(path.join(base, 'admin_ui_collection_status.js'), 'utf8');

function summary(changes = {}) {
    return {id:'r', uploadedfiles:0, reserveduploads:0, maxfiles:2, totalfilesize:0,
        lastupload:0, expiry:1100, fileidlist:[], ...changes};
}

function node(extra = {}) {
    return Object.assign({textContent:'', value:'', hidden:false, disabled:false, dataset:{}, attributes:{}, children:[],
        classList:{add(){},remove(){},toggle(){},contains(){return false;}}, setAttribute(key,value){this.attributes[key]=String(value);},
        getAttribute(key){return this.attributes[key] ?? null;}, removeAttribute(key){delete this.attributes[key];},
        append(...items){this.children.push(...items);}, appendChild(item){this.children.push(item);},
        replaceChildren(...items){this.children=items;},
        querySelector(selector){return this.children.find(child=>selector==='.collapse-toggle' && child.className?.includes('collapse-toggle')) ?? null;}
    }, extra);
}

function statusContext(initial = summary()) {
    let localNow = 9_000_000;
    const nodes = {};
    const row = node({id:'row-r', dataset:{id:initial.id, uploadedfiles:String(initial.uploadedfiles),
        reserveduploads:String(initial.reserveduploads), maxfiles:String(initial.maxfiles),
        totalfilesize:String(initial.totalfilesize), lastupload:String(initial.lastupload),
        expiry:String(initial.expiry), fileids:initial.fileidlist.join(',')}});
    nodes['row-r'] = row;
    for (const id of ['collectionRefresh','collectionRefreshStatus','collectionUpdatedAt','collection-status-r',
        'collection-received-r','collection-inflight-r','collection-empty-r','totalFiles-fr-r',
        'cell-lastupdate-r','cell-expiry-r','collection-count-r','collection-max-r','collection-size-r',
        'collection-name-r','edit-r','delete-r','requestCount','requestEmpty','requestTableWrap']) nodes[id] = node();
    nodes.filerequesttable = node({querySelectorAll:()=>[row]});
    nodes['collapse-filelist-r'] = node({shareLinks:new Map([['original',{id:'new-share',url:'https://example.test/d?id=new-share'}]]),
        shareBatch:[{id:'original',name:'资料.txt'}], deliveryChoices:new Map([['original',true]])});
    nodes.deliveryTitle = node({value:'未完成的交付标题'});
    nodes.deliveryNote = node({value:'保留我的输入'});
    const intervals = [];
    const context = vm.createContext({document:{getElementById:id=>nodes[id] ?? null,
        querySelectorAll:selector=>selector==='[data-collection-summary]'?[row]:[],
        createElement:()=>node(), hidden:false}, window:{addEventListener(){}},
        t:(key,values={})=>key+JSON.stringify(values),
        Date:class extends Date {static now(){return localNow;}},
        setInterval:callback=>{intervals.push(callback);return intervals.length;}, clearInterval(){},
        formatTimestampWithNegative:(stamp,empty)=>stamp?`timestamp:${stamp}`:empty,
        formatUnixTimestamp:stamp=>`timestamp:${stamp}`,
        formatFileRequestExpiry:stamp=>`expiry:${stamp}`,getReadableSize:bytes=>`${bytes} B`});
    vm.runInContext(source(),context);
    return {context,nodes,row,intervals,advance:ms=>{localNow+=ms;}};
}

test('expiry outranks count limits, Unix-second equality remains valid, and reservations never become received files',()=>{
    const {context}=statusContext();
    assert.equal(context.deriveCollectionStatus(summary({expiry:1000,uploadedfiles:2,reserveduploads:1}),1001),'expired');
    assert.equal(context.deriveCollectionStatus(summary({expiry:1000,uploadedfiles:2,reserveduploads:1}),1000),'full');
    assert.equal(context.deriveCollectionStatus(summary({uploadedfiles:0,reserveduploads:2}),1000),'reserved');
    assert.equal(context.deriveCollectionStatus(summary({maxfiles:0,expiry:0,uploadedfiles:9,reserveduploads:12}),999999),'accepting');
    assert.equal(context.deriveCollectionStatus(summary({uploadedfiles:1,reserveduploads:0}),1000),'accepting');
    assert.equal(context.collectionSummarySnapshot(summary({uploadedfiles:-1})),null);
    assert.equal(context.collectionSummarySnapshot(summary({fileidlist:undefined})),null);
});

test('SSR status uses server time and shows zero received separately from active reservations',()=>{
    const {context,nodes,intervals,advance}=statusContext(summary({expiry:1000,reserveduploads:2}));
    context.apiURequestList=()=>{throw new Error('local expiry rendering must not poll');};
    context.initCollectionStatus(1000);
    assert.equal(context.collectionServerNow(),1000);
    assert.match(nodes['collection-status-r'].textContent,/collection.status.reserved/);
    assert.equal(nodes['totalFiles-fr-r'].textContent,'0');
    assert.equal(nodes['collection-empty-r'].hidden,false);
    assert.equal(nodes['collection-inflight-r'].hidden,false);
    assert.match(nodes['collection-inflight-r'].textContent,/"count":2/);
    advance(1000);intervals.at(-1)();
    assert.equal(context.collectionServerNow(),1001);
    assert.match(nodes['collection-status-r'].textContent,/collection.status.expired/);
    assert.match(nodes['cell-expiry-r'].textContent,/common.expired/);
});

test('failed or malformed manual refresh preserves the last confirmed summary and M7 working state',async()=>{
    const {context,nodes,row}=statusContext(summary({uploadedfiles:1,fileidlist:['original']}));
    context.initCollectionStatus(1000);
    const panel=nodes['collapse-filelist-r'];
    const links=panel.shareLinks,batch=panel.shareBatch,choices=panel.deliveryChoices;
    const oldUpdated=nodes.collectionUpdatedAt.textContent;
    context.apiURequestList=async()=>{throw Object.assign(new Error('PRIVATE_KEY /srv/config'),{status:503});};
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.match(nodes.collectionRefreshStatus.textContent,/collection.refreshFailed/);
    assert.doesNotMatch(nodes.collectionRefreshStatus.textContent,/PRIVATE_KEY|srv/);
    assert.equal(nodes['totalFiles-fr-r'].textContent,'1');
    assert.equal(nodes.collectionUpdatedAt.textContent,oldUpdated);
    context.apiURequestList=async()=>({requests:[summary({uploadedfiles:2}),summary({id:'broken',expiry:-1})],serverTime:1200});
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.equal(nodes['totalFiles-fr-r'].textContent,'1');
    assert.equal(context.collectionServerNow(),1000);
    assert.equal(nodes['row-r'],row);
    assert.equal(nodes['collapse-filelist-r'],panel);
    assert.equal(panel.shareLinks,links);
    assert.equal(panel.shareBatch,batch);
    assert.equal(panel.deliveryChoices,choices);
    assert.equal(nodes.deliveryTitle.value,'未完成的交付标题');
    assert.equal(nodes.deliveryNote.value,'保留我的输入');
    assert.equal(nodes.collectionRefresh.disabled,false);
});

test('manual refresh blocks duplicate requests, updates existing rows only and stops after permission loss',async()=>{
    const {context,nodes,row}=statusContext();
    context.initCollectionStatus(1000);
    let finish,calls=0;
    context.apiURequestList=()=>{calls++;return new Promise(resolve=>{finish=resolve;});};
    const pending=context.refreshCollectionSummaries();
    const duplicate=context.refreshCollectionSummaries();
    assert.equal(calls,1);
    assert.equal(nodes.collectionRefresh.disabled,true);
    finish({requests:[summary({uploadedfiles:2,lastupload:1010,totalfilesize:9,fileidlist:['a','b']}),summary({id:'other'})],serverTime:1020});
    assert.equal(await pending,true);
    assert.equal(await duplicate,true);
    assert.equal(nodes['row-r'],row);
    assert.equal(nodes['row-other'],undefined);
    assert.equal(nodes['totalFiles-fr-r'].textContent,'2');
    assert.match(nodes['collection-status-r'].textContent,/collection.status.full/);
    assert.equal(nodes['collection-size-r'].textContent,'9 B');
    assert.equal(nodes['cell-lastupdate-r'].textContent,'timestamp:1010');
    assert.equal(context.collectionServerNow(),1020);
    assert.equal(nodes['collection-count-r'].children.length,1);
    context.apiURequestList=async()=>{calls++;throw Object.assign(new Error('forbidden'),{status:403});};
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.equal(nodes.collectionRefresh.disabled,true);
    assert.match(nodes.collectionRefreshStatus.textContent,/collection.authRequired/);
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.equal(calls,2);
    assert.equal(nodes['totalFiles-fr-r'].textContent,'2');
});

test('editing a collection updates status and action closures in place without resetting delivery inputs',()=>{
    const {context,nodes,row}=statusContext(summary({uploadedfiles:2}));
    vm.runInContext(fs.readFileSync(path.join(base,'admin_ui_filerequest.js'),'utf8'),context);
    context.initCollectionStatus(1000);
    const panel=nodes['collapse-filelist-r'];
    const edited={...summary({uploadedfiles:2,maxfiles:0,expiry:0}),name:'更新后的收集',maxsize:8,notes:'新说明',alloweddownloads:2};
    context.insertOrReplaceFileRequest(edited);
    assert.match(nodes['collection-status-r'].textContent,/collection.status.accepting/);
    assert.equal(nodes['collection-max-r'].textContent,'');
    assert.match(nodes['cell-expiry-r'].textContent,/common.never/);
    assert.equal(nodes['collection-name-r'].textContent,'更新后的收集');
    const editCalls=[],deleteCalls=[];
    context.editFileRequest=(...args)=>editCalls.push(args);
    context.deleteOrShowModal=(...args)=>deleteCalls.push(args);
    nodes['edit-r'].onclick();nodes['delete-r'].onclick();
    assert.deepEqual(editCalls[0],['r','更新后的收集',0,8,0,'新说明',2]);
    assert.deepEqual(deleteCalls[0],['r','更新后的收集',2]);
    assert.equal(nodes['row-r'],row);
    assert.equal(nodes['collapse-filelist-r'],panel);
    assert.equal(panel.shareLinks.get('original').id,'new-share');
    assert.equal(nodes.deliveryTitle.value,'未完成的交付标题');
    assert.equal(nodes.deliveryNote.value,'保留我的输入');
});

test('summary refresh reuses the management permission and server Date without caching public credentials in presentation data',async()=>{
    const {context}=statusContext();
    vm.runInContext(fs.readFileSync(path.join(base,'admin_api.js'),'utf8'),context);
    const records=[summary({apikey:'PUBLIC_UPLOAD_CAPABILITY',name:'资料收集'})];
    const permissions=[],requests=[];
    context.getToken=async permission=>{permissions.push(permission);return 'private-management-token';};
    context.fetch=async(url,options)=>{
        requests.push({url,options});
        return {ok:true,json:async()=>records,headers:{get:name=>name==='Date'?'Thu, 01 Jan 1970 00:16:40 GMT':null}};
    };
    const result=await context.apiURequestList();
    assert.deepEqual(permissions,['PERM_MANAGE_FILE_REQUESTS']);
    assert.equal(requests[0].url,'./api/uploadrequest/list');
    assert.equal(requests[0].options.method,'GET');
    assert.equal(requests[0].options.cache,'no-store');
    assert.equal(requests[0].options.headers.apikey,'private-management-token');
    assert.equal(result.serverTime,1000);
    const presentation=context.collectionSummarySnapshot(result.requests[0]);
    assert.equal(presentation.apikey,undefined);
    assert.doesNotMatch(JSON.stringify(presentation),/PUBLIC_UPLOAD_CAPABILITY|private-management-token/);
});
