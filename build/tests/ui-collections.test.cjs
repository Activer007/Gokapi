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
