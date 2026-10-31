const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_collection_files.js'),'utf8');

function file(id,changes={}) {
    return {Id:id,Name:`${id}.txt`,Size:'1 B',UploadDate:1000,DownloadsRemaining:1,
        UnlimitedDownloads:false,IsPendingDeletion:false,IsFileRequest:true,FileRequestId:'r',UploaderId:0,...changes};
}

function element(extra={}) {
    return Object.assign({id:'',dataset:{},attributes:{},children:[],checked:false,disabled:false,hidden:false,
        classList:{add(){},remove(){},contains(){return false;}},scrollIntoView(){},
        setAttribute(key,value){this.attributes[key]=String(value);},
        appendChild(child){this.children.push(child);return child;}},extra);
}

function rowSkeleton() {
    const fields={};
    for(const name of ['select','name','error','result','url','copy','date','remaining','expired','share','delete','size','share-label','download']) fields[name]=element();
    fields.select.checked=true;
    const row=element({fields,children:Object.values(fields),querySelector:selector=>fields[selector.match(/data-file-field="([^"]+)"/)?.[1]]??null});
    return row;
}

function filesContext(api) {
    const list=element();
    const summary=element({id:'row-r',dataset:{ownerId:'7'}});
    const panel=element({id:'collapse-filelist-r',shareLinks:new Map([['old',{id:'existing-share',url:'https://example.test/d?id=existing-share'}]]),
        shareBatch:[{id:'old',name:'old.txt'}],deliveryChoices:new Map([['old',true]]),querySelector:selector=>selector==='[data-collection-files]'?list:null});
    const old=rowSkeleton();old.id='cell-listupload-old';old.dataset.requestId='r';
    old.fields.select.checked=true;old.fields.share.dataset.shared='true';old.fields.url.value='https://example.test/d?id=existing-share';
    list.appendChild(old);
    const nodes={'row-r':summary,'collapse-filelist-r':panel,deliveryTitle:{value:'Keep delivery title'},
        deliveryNote:{value:'Keep delivery note'},'collection-file-row-template':{content:{cloneNode:()=>({firstElementChild:rowSkeleton()})}}};
    const find=(root,id)=>root.id===id?root:root.children?.map(child=>find(child,id)).find(Boolean);
    const calls=[];
    const context=vm.createContext({document:{getElementById:id=>nodes[id]??find(list,id)??null},
        currentUserId:7,canDeleteOtherCollectionFiles:false,canViewOtherRequests:false,
        collectionSummarySnapshots:new Map([['r',{id:'r'}]]),collectionSummaryAuthBlocked:false,
        storedTokens:new Map([['PERM_VIEW','cached-token']]),collectionArrivalCanView:()=>true,
        apiFilesListById:api,t:key=>key,formatUnixTimestamp:stamp=>`time:${stamp}`,
        prepareFileRequestRow:row=>calls.push(['prepare',row]),
        updateCollectionShareSelection:id=>calls.push(['selection',id]),ensureCollectionToggle:snapshot=>calls.push(['toggle',snapshot.id]),
        shareCollectionFile(){},downloadFileWithPresign(){},deleteFileFr(){},expireCollectionFile(){}});
    vm.runInContext(source,context);
    return {context,nodes,list,panel,old,summary,calls};
}

test('arrival details append once with unchecked selection and authorized request-owner controls while retaining M7 state',async()=>{
    const fetched=[];
    const scene=filesContext(async id=>{fetched.push(id);return file(id,{Name:'<script>新资料.txt'});});
    const {context,nodes,list,panel,old,summary,calls}=scene;
    const links=panel.shareLinks,batch=panel.shareBatch,choices=panel.deliveryChoices;
    const result=await context.loadCollectionArrivalFiles('r',['old','new','new']);
    assert.deepEqual(fetched,['new']);
    assert.deepEqual(Array.from(result.shown),['old','new']);
    assert.equal(list.children.length,2);
    const added=list.children[1];
    assert.equal(added.fields.select.checked,false);
    assert.equal(added.fields.select.value,'new');
    assert.equal(added.fields.name.textContent,'<script>新资料.txt');
    assert.equal(added.fields.delete.disabled,false); // UploaderId=0 does not override the authorized request owner.
    assert.equal(list.children[0],old);
    assert.equal(old.fields.select.checked,true);
    assert.equal(old.fields.share.dataset.shared,'true');
    assert.equal(old.fields.url.value,'https://example.test/d?id=existing-share');
    assert.equal(panel.shareLinks,links);assert.equal(panel.shareBatch,batch);assert.equal(panel.deliveryChoices,choices);
    assert.equal(nodes.deliveryTitle.value,'Keep delivery title');assert.equal(nodes.deliveryNote.value,'Keep delivery note');
    assert.equal(calls.find(call=>call[0]==='prepare')[1],summary);
    summary.dataset.ownerId='8';
    assert.equal(context.createCollectionArrivalFileRow('r',file('other')).fields.delete.disabled,true);
});

test('wrong collection metadata is rejected and one deleted item does not prevent the remaining valid arrival',async()=>{
    const {context,list}=filesContext(async id=>{
        if(id==='gone') throw Object.assign(new Error('not found'),{status:404});
        if(id==='wrong') return file(id,{FileRequestId:'another-request'});
        return file(id);
    });
    const result=await context.loadCollectionArrivalFiles('r',['wrong','gone','valid']);
    assert.deepEqual(Array.from(result.shown),['valid']);
    assert.deepEqual(Array.from(result.unavailable),['gone']);
    assert.deepEqual(Array.from(result.failed),['wrong']);
    assert.equal(list.children.length,2);
    assert.equal(list.children[1].id,'cell-listupload-valid');
    assert.equal(context.collectionArrivalFileValid(file('different'),'requested','r'),false);
    assert.equal(context.collectionArrivalFileValid(file('ordinary',{IsFileRequest:false}),'ordinary','r'),false);
});

test('detail authorization failure or a withdrawn scope discards the staged batch before any new DOM appears',async()=>{
    const {context,list}=filesContext(async id=>{
        if(id==='forbidden') throw Object.assign(new Error('PRIVATE_DIAGNOSTIC'),{status:403});
        return file(id);
    });
    await assert.rejects(context.loadCollectionArrivalFiles('r',['valid','forbidden']),error=>error.status===403);
    assert.equal(list.children.length,1);
    assert.equal(context.storedTokens.has('PERM_VIEW'),false);
    let active=true;
    context.collectionArrivalCanView=()=>active;
    context.apiFilesListById=async id=>{active=false;return file(id);};
    const withdrawn=await context.loadCollectionArrivalFiles('r',['valid']);
    assert.equal(withdrawn.shown.length,0);
    assert.deepEqual(Array.from(withdrawn.failed),['valid']);
    assert.equal(list.children.length,1);
});
