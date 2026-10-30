const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_filerequest.js'),'utf8');

test('collection download binding survives edits and transitions from ZIP to single to empty',()=>{
    const button={dataset:{},classList:{toggle(){}},removeAttribute(){}};
    const calls=[];
    const context=vm.createContext({document:{getElementById:()=>button},
        downloadFilesZipWithPresign:(...args)=>calls.push(['zip',...args]),
        downloadFileWithPresign:id=>calls.push(['file',id])});
    vm.runInContext(source,context);
    const name="Team's <documents>";
    context.bindRequestDownload(button,['a','b'],name);
    button.onclick();
    assert.deepEqual(calls,[['zip','a,b',name]]);
    context.removeDownloadFileReference('a','collection');
    button.onclick();
    assert.deepEqual(calls[1],['file','b']);
    context.removeDownloadFileReference('b','collection');
    assert.equal(button.disabled,true);
    assert.equal(button.onclick,null);
    assert.equal(button.dataset.recordName,name);
});

test('collection count and empty state follow creation and deletion',()=>{
    let rows=[];
    const elements={filerequesttable:{querySelectorAll:()=>rows},requestCount:{},requestEmpty:{},requestTableWrap:{}};
    const context=vm.createContext({document:{getElementById:id=>elements[id]}});
    vm.runInContext(source,context);
    context.updateFileRequestEmptyState();
    assert.equal(elements.requestEmpty.hidden,false);
    assert.equal(elements.requestTableWrap.hidden,true);
    rows=[{},{}];
    context.updateFileRequestEmptyState();
    assert.equal(elements.requestCount.textContent,'2');
    assert.equal(elements.requestEmpty.hidden,true);
    assert.equal(elements.requestTableWrap.hidden,false);
});

test('failed collection and received-file deletions allow retry',async()=>{
    const buttons={deleteRequest:{disabled:false},deleteFile:{disabled:false}};
    const context=vm.createContext({document:{getElementById:id=>id==='delete-r'?buttons.deleteRequest:id==='button-delete-f'?buttons.deleteFile:{}},
        apiURequestDelete:()=>Promise.reject(new Error('offline')),
        apiFilesDelete:()=>Promise.reject(new Error('offline')),t:key=>key,alert(){},console:{error(){}}});
    vm.runInContext(source,context);
    context.deleteFileRequest('r');
    context.deleteFileFr('f','r');
    assert.equal(buttons.deleteRequest.disabled,true);
    assert.equal(buttons.deleteFile.disabled,true);
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(buttons.deleteRequest.disabled,false);
    assert.equal(buttons.deleteFile.disabled,false);
});

function collectionFormContext(stored = {}) {
    const elements = new Map();
    const field = id => {
        if (!elements.has(id)) elements.set(id, {
            value:'',checked:false,disabled:false,hidden:false,
            setAttribute(){},reportValidity(){return true;}
        });
        return elements.get(id);
    };
    const now = Date.UTC(2026,9,30,6,30,45);
    const context = vm.createContext({
        document:{getElementById:field,querySelector:()=>null},
        localStorage:{getItem:key=>stored[key]??null,setItem:(key,value)=>stored[key]=value},
        Date:class extends Date { static now(){return now;} },
        limitMaxFiles:0,limitMaxSize:0,createCalendar(){},t:key=>key,
        runModalSave:async(_id,_button,save)=>save(),showToast(){},
    });
    vm.runInContext(source,context);
    context.insertOrReplaceFileRequest = () => {};
    return {context,field,now};
}

test('new collections always start with five days and one download despite old expiry preferences',()=>{
    for (const oldExpiry of ['0','1209600']) {
        const {context,field,now}=collectionFormContext({fr_expiry:oldExpiry,fr_maxfiles:'3',fr_maxsize:'25'});
        context.loadFileRequestDefaults();
        assert.equal(field('mc_expiry').checked,true);
        assert.equal(field('mi_expiry').disabled,false);
        assert.equal(Number(field('mi_expiry').value),now/1000+5*24*60*60);
        assert.equal(field('mi_maxfiles').value,'3');
        assert.equal(field('mi_maxsize').value,'25');
        assert.equal(field('mi_downloads').value,1);
        assert.equal(field('mi_downloads').disabled,false);
        assert.equal(field('legacyDownloadsOption').hidden,true);
        assert.equal(field('mc_keepdownloads').checked,false);
    }
});

test('editing preserves the cutoff and legacy unlimited policy until an explicit change',async()=>{
    const {context,field,now}=collectionFormContext();
    const calls=[];
    context.apiURequestSave=async(...args)=>{calls.push(args);return {};};
    const expiry=now/1000+3600;
    context.setModalValues('old','Legacy',0,0,expiry,'note',0);
    assert.equal(Number(field('mi_expiry').value),expiry);
    assert.equal(field('legacyDownloadsOption').hidden,false);
    assert.equal(field('mc_keepdownloads').checked,true);
    assert.equal(field('mi_downloads').disabled,true);
    await context.saveFileRequest();
    assert.equal(calls[0][4],expiry);
    assert.equal(calls[0][6],undefined);
    field('mc_keepdownloads').checked=false;
    context.handleRequestDownloadPolicyChange();
    field('mi_downloads').value='10';
    await context.saveFileRequest();
    assert.equal(field('mi_downloads').disabled,false);
    assert.equal(calls[1][6],10);

    context.setModalValues('old','Legacy',0,0,0,'note',0);
    assert.equal(field('mc_expiry').checked,false);
    assert.equal(field('mi_expiry').disabled,true);
});

test('collection form accepts only integer download allowances from one through ten',async()=>{
    const {context,field}=collectionFormContext();
    const calls=[];
    context.apiURequestSave=async(...args)=>{calls.push(args);return {};};
    context.loadFileRequestDefaults();
    for (const invalid of ['', '0','-1','11','1.5','not a number']) {
        field('mi_downloads').value=invalid;
        await assert.rejects(context.saveFileRequest(),/request.invalidDownloads/);
    }
    assert.equal(calls.length,0);
    for (const valid of ['1','10']) {
        field('mi_downloads').value=valid;
        await context.saveFileRequest();
    }
    assert.deepEqual(calls.map(args=>args[6]),[1,10]);
});

test('collection API omits an unchanged legacy allowance and transmits an explicit bounded allowance',async()=>{
    const requests=[];
    const context=vm.createContext({console,Base64:{encode:value=>value},fetch:async(_url,options)=>{
        requests.push(options);
        return {ok:true,json:async()=>({})};
    }});
    const apiSource=fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_api.js'),'utf8');
    vm.runInContext(apiSource,context);
    context.getToken=async()=> 'fixture-token';
    await context.apiURequestSave('old','Legacy',0,0,0,'note');
    await context.apiURequestSave('new','Limited',0,0,1,'note',10);
    assert.equal(Object.hasOwn(requests[0].headers,'allowedDownloads'),false);
    assert.equal(requests[1].headers.allowedDownloads,10);
});

test('collection download events update allowances and remove exhausted files from single and ZIP actions',()=>{
    const sources=[];
    const controls=[{},{}].map(()=>({disabled:false,attrs:{onclick:'download'},classList:{add(){}},
        setAttribute(name,value){this.attrs[name]=value;},removeAttribute(name){delete this.attrs[name];}}));
    const button={dataset:{},classList:{toggle(){}},removeAttribute(){}};
    const cell={dataset:{remaining:'2'},textContent:'2'};
    const status={hidden:true};
    const row={dataset:{requestId:'collection'},querySelectorAll:()=>controls};
    const nodes={
        'cell-downloadsRemaining-a':cell,'cell-listupload-a':row,
        'cell-expired-file-a':status,'download-collection':button,
    };
    const downloads=[];
    const context=vm.createContext({document:{getElementById:id=>nodes[id]},
        EventSource:class {constructor(url){this.url=url;sources.push(this);}},
        downloadFileWithPresign:id=>downloads.push(id),downloadFilesZipWithPresign(){},
    });
    vm.runInContext(source,context);
    context.bindRequestDownload(button,['a','b'],'Files');
    context.registerCollectionChanges();
    assert.equal(sources.length,1);
    assert.equal(sources[0].url,'./uploadStatus');
    const emit=event=>sources[0].onmessage({data:JSON.stringify(event)});
    emit({event:'uploadStatus',file_id:'a',downloads_remaining:0});
    assert.equal(cell.textContent,'2');
    emit({event:'download',file_id:'a',downloads_remaining:1});
    assert.equal(cell.textContent,'1');
    emit({event:'download',file_id:'a',downloads_remaining:0});
    assert.equal(cell.textContent,'0');
    assert.equal(status.hidden,false);
    assert.ok(controls.every(control=>control.disabled && control.attrs['aria-disabled']==='true' && !('onclick' in control.attrs)));
    assert.equal(button.dataset.fileIds,'b');
    button.onclick();
    assert.deepEqual(downloads,['b']);
    emit({event:'download',file_id:'a',downloads_remaining:1});
    assert.equal(cell.textContent,'0');
    emit({event:'download',file_id:'a',downloads_remaining:-1});
    emit({event:'download',file_id:'a',downloads_remaining:'2'});
    sources[0].onmessage({data:'invalid JSON'});
    assert.equal(cell.textContent,'0');
});

test('collection SharedWorker ignores upload-table notifications and consumes only download events',()=>{
    const port={started:false,start(){this.started=true;},close(){}};
    const cell={dataset:{remaining:'3'},textContent:'3'};
    const context=vm.createContext({document:{getElementById:id=>id==='cell-downloadsRemaining-a'?cell:null},
        SharedWorker:class {constructor(){this.port=port;}},window:{location:{}},
        EventSource:class {constructor(){throw new Error('Unexpected fallback');}},
        addRow(){throw new Error('Collection has no upload table');},
    });
    vm.runInContext(source,context);
    context.registerCollectionChanges();
    assert.equal(port.started,true);
    port.onmessage({data:{type:'fileAdded',item:{Id:'a'}}});
    port.onmessage({data:{type:'message',data:JSON.stringify({event:'download',file_id:'a',downloads_remaining:2})}});
    assert.equal(cell.textContent,'2');
    port.onmessage({data:{type:'shutdown'}});
    assert.equal(context.window.location.href,'./login');
});
