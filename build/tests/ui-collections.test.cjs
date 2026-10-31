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

test('rebuilt collection download buttons retain exhausted-file exclusions after an edit',()=>{
    const cells={
        'cell-downloadsRemaining-spent':{dataset:{remaining:'0'}},
        'cell-downloadsRemaining-ready':{dataset:{remaining:'2'}},
        'cell-downloadsRemaining-legacy':{dataset:{remaining:'-1'}},
    };
    const calls=[];
    const context=vm.createContext({document:{getElementById:id=>cells[id]},
        downloadFilesZipWithPresign:(...args)=>calls.push(['zip',...args]),
        downloadFileWithPresign:id=>calls.push(['file',id])});
    vm.runInContext(source,context);
    for (const [ids,expected] of [
        [['spent','ready','legacy','new-file'],['ready','legacy','new-file']],
        [['spent','ready'],['ready']],
        [['spent'],[]],
    ]) {
        // createFileRequestRow binds the API response's complete fileidlist,
        // then prepares the replacement while its existing file list remains.
        const button={dataset:{},classList:{toggle(){}},removeAttribute(){}};
        context.bindRequestDownload(button,ids,'Renamed collection');
        const row={querySelector:selector=>selector==='[id^="download-"]'?button:null};
        context.prepareFileRequestRow(row);
        assert.equal(button.dataset.fileIds,expected.join(','));
        assert.equal(button.disabled,expected.length===0);
        if (expected.length===0) {
            assert.equal(button.onclick,null);
        } else {
            button.onclick();
            assert.deepEqual(calls.at(-1),expected.length===1?['file',expected[0]]:['zip',expected.join(','),'Renamed collection']);
        }
    }
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

function shareContext(api) {
    const nodes = {
        'share-error-f': {textContent:''}, 'share-result-f': {hidden:true},
        'share-url-f': {value:''}, 'share-copy-f': {dataset:{}}
    };
    const button = {disabled:false,dataset:{},setAttribute(){},removeAttribute(){}};
    const context = vm.createContext({document:{getElementById:id=>nodes[id]},apiCollectionFileShare:api,t:key=>key});
    vm.runInContext(source,context);
    return {context,nodes,button};
}

test('collection sharing waits once and retains the new link without duplicating a successful share',async()=>{
    let complete, calls=0;
    const {context,nodes,button}=shareContext(()=>{calls++;return new Promise(resolve=>complete=resolve);});
    const pending=context.shareCollectionFile('f',button);
    assert.equal(button.disabled,true);
    const duplicate=context.shareCollectionFile('f',button);
    assert.equal(calls,1);
    const url='https://example.test/d?id=ordinary';
    complete({UrlDownload:url});
    await pending;
    await duplicate;
    assert.equal(nodes['share-result-f'].hidden,false);
    assert.equal(nodes['share-url-f'].value,url);
    assert.equal(nodes['share-copy-f'].dataset.clipboardText,url);
    assert.equal(nodes['share-error-f'].textContent,'');
    await context.shareCollectionFile('f',button);
    assert.equal(calls,1);
});

test('collection sharing shows the failure inline and permits a successful retry',async()=>{
    let calls=0;
    const {context,nodes,button}=shareContext(async()=>{
        if (++calls===1) throw new Error('quota full');
        return {UrlDownload:'https://example.test/d?id=retry'};
    });
    await context.shareCollectionFile('f',button);
    assert.equal(button.disabled,false);
    assert.equal(nodes['share-result-f'].hidden,true);
    assert.equal(nodes['share-error-f'].textContent,'request.shareFailedquota full');
    await context.shareCollectionFile('f',button);
    assert.equal(nodes['share-error-f'].textContent,'');
    assert.equal(nodes['share-result-f'].hidden,false);
    assert.equal(button.disabled,true);
});

test('collection share API explicitly detaches the copy and leaves its rules unchanged',async()=>{
    let request;
    const context=vm.createContext({fetch:async(url,options)=>{
        request={url,...options}; return {ok:true,json:async()=>({Id:'new'})};
    }});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_api.js'),'utf8'),context);
    context.getToken=async permission=>{assert.equal(permission,'PERM_UPLOAD');return 'fixture-token';};
    await context.apiCollectionFileShare('received');
    assert.equal(request.url,'./api/files/duplicate');
    assert.equal(request.method,'POST');
    assert.deepEqual({...request.headers},{'Content-Type':'application/json',apikey:'fixture-token',id:'received',asShare:'true'});
});

function collectionFormContext(stored = {}, limits = {files:0,size:0}) {
    const elements = new Map();
    const field = id => {
        if (!elements.has(id)) elements.set(id, {
            value:'',checked:false,disabled:false,hidden:false,attributes:{},
            setAttribute(name,value){this.attributes[name]=String(value);},reportValidity(){return true;}
        });
        return elements.get(id);
    };
    const now = Date.UTC(2026,9,30,6,30,45);
    const context = vm.createContext({
        document:{getElementById:field,querySelector:()=>null},
        localStorage:{getItem:key=>stored[key]??null,setItem:(key,value)=>stored[key]=value},
        Date:class extends Date { static now(){return now;} },
        limitMaxFiles:limits.files,limitMaxSize:limits.size,createCalendar(){},t:key=>key,
        runModalSave:async(_id,_button,save)=>save(),showToast(){},
    });
    vm.runInContext(source,context);
    context.insertOrReplaceFileRequest = () => {};
    return {context,field,now};
}

test('first collection uses server limits without saved defaults and submits those limits',async()=>{
    for (const limits of [{files:7,size:13},{files:0,size:13},{files:7,size:0},{files:0,size:0}]) {
        const {context,field}=collectionFormContext({},limits);
        const calls=[];
        context.apiURequestSave=async(...args)=>{calls.push(args);return {};};
        context.loadFileRequestDefaults();
        for (const [name,limit] of [['maxfiles',limits.files],['maxsize',limits.size]]) {
            const checkbox=field(`mc_${name}`),input=field(`mi_${name}`);
            assert.equal(checkbox.checked,limit!==0);
            assert.equal(checkbox.disabled,limit!==0);
            assert.equal(input.disabled,limit===0);
            assert.equal(input.attributes.max,limit===0?'':String(limit));
            if (limit!==0) assert.equal(Number(input.value),limit);
        }
        await context.saveFileRequest();
        assert.equal(calls.length,1);
        assert.equal(Number(calls[0][2]),limits.files);
        assert.equal(Number(calls[0][3]),limits.size);
        assert.equal(calls[0][6],1);
    }
});

test('first collection preserves saved bounded preferences under current server limits',async()=>{
    const {context,field}=collectionFormContext({fr_maxfiles:'3',fr_maxsize:'5'},{files:7,size:13});
    const calls=[];
    context.apiURequestSave=async(...args)=>{calls.push(args);return {};};
    context.loadFileRequestDefaults();
    assert.equal(field('mi_maxfiles').value,'3');
    assert.equal(field('mi_maxsize').value,'5');
    assert.equal(field('mc_maxfiles').disabled,true);
    assert.equal(field('mc_maxsize').disabled,true);
    await context.saveFileRequest();
    assert.deepEqual(calls[0].slice(2,4),['3','5']);
});

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
    const row={dataset:{requestId:'collection'},querySelectorAll:()=>controls,querySelector:()=>null};
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
