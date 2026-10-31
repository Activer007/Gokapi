const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const base=path.join(__dirname,'../../internal/webserver/web/static/js');

function batchContext(api) {
    const fields=['a','b'].map(value=>({value,checked:true,disabled:false,dataset:{name:`${value}.txt`}}));
    const nodes={
        'collapse-filelist-r':{querySelectorAll:()=>fields},
        'share-select-all-r':{}, 'share-batch-status-r':{},
        'share-batch-result-r':{hidden:true}, 'share-batch-links-r':{value:''}
    };
    for (const field of fields) {
        nodes[`button-share-${field.value}`]={disabled:false,dataset:{},setAttribute(){},removeAttribute(){}};
        nodes[`share-error-${field.value}`]={textContent:''};
        nodes[`share-result-${field.value}`]={hidden:true};
        nodes[`share-url-${field.value}`]={value:''};
        nodes[`share-copy-${field.value}`]={dataset:{}};
        nodes[`cell-listupload-${field.value}`]={dataset:{requestId:'r'},querySelector:()=>field};
        nodes[`cell-name-${field.value}`]={textContent:field.dataset.name};
    }
    const context=vm.createContext({document:{getElementById:id=>nodes[id]},
        apiCollectionFileShare:api,t:(key,values={})=>key+JSON.stringify(values),showToast(){}});
    vm.runInContext(fs.readFileSync(path.join(base,'admin_ui_allPages.js'),'utf8'),context);
    vm.runInContext(fs.readFileSync(path.join(base,'admin_ui_filerequest.js'),'utf8'),context);
    return {context,nodes,fields};
}

test('collection batch is sequential, retains partial results and retries only failures',async()=>{
    const calls=[];
    let active=0,maxActive=0;
    const {context,nodes}=batchContext(async id=>{
        calls.push(id); maxActive=Math.max(maxActive,++active);
        await new Promise(resolve=>setImmediate(resolve)); active--;
        if (id==='b' && calls.filter(id=>id==='b').length===1) throw new Error('quota full');
        return {UrlDownload:`https://example.test/d/${id}`};
    });
    const button={disabled:false};
    const pending=context.shareSelectedCollectionFiles('r',button);
    await context.shareSelectedCollectionFiles('r',button);
    await pending;
    assert.deepEqual(calls,['a','b']);
    assert.equal(maxActive,1);
    assert.equal(nodes['share-batch-links-r'].value,'a.txt\nhttps://example.test/d/a');
    assert.match(nodes['share-batch-status-r'].textContent,/"failed":1/);
    assert.match(nodes['share-batch-status-r'].textContent,/b.txt/);
    assert.equal(button.disabled,false);
    await context.shareSelectedCollectionFiles('r',button);
    assert.deepEqual(calls,['a','b','b']);
    assert.equal(nodes['share-batch-result-r'].hidden,false);
    assert.equal(nodes['share-batch-links-r'].value,'a.txt\nhttps://example.test/d/a\n\nb.txt\nhttps://example.test/d/b');
    assert.match(nodes['share-batch-status-r'].textContent,/"success":2,"failed":0/);
    // The new shares stay copyable when the original collection rows disappear.
    nodes['collapse-filelist-r'].querySelectorAll=()=>[];
    context.updateCollectionShareResults('r');
    assert.equal(context.collectionShareLinks('r').length,2);
    assert.match(nodes['share-batch-links-r'].value,/a.txt.*\nhttps:\/\/example.test\/d\/a/);
});

test('collection selection and empty feedback exclude unavailable files',async()=>{
    const {context,nodes,fields}=batchContext(()=>{throw new Error('must not call');});
    fields[1].checked=false; fields[1].disabled=true;
    context.selectCollectionShares('r',false);
    assert.equal(fields[0].checked,false);
    context.selectCollectionShares('r',true);
    assert.equal(fields[0].checked,true);
    assert.equal(fields[1].checked,false);
    assert.equal(nodes['share-select-all-r'].checked,true);
    context.selectCollectionShares('r',false);
    const notices=[]; context.showToast=(...args)=>notices.push(args);
    await context.shareSelectedCollectionFiles('r',{disabled:false});
    assert.match(notices[0][1],/sharing.noSelection/);
});

test('batch clipboard uses exact names and URLs and offers the complete multiline fallback',async()=>{
    const {context}=batchContext(()=>{});
    const entries=[{name:'a.txt',url:'https://example.test/a#key'},{name:'b.txt',url:'https://example.test/b?name=b'}];
    let written;
    context.navigator={clipboard:{writeText:async text=>{written=text;}}};
    const notices=[],fallbacks=[];
    context.showToast=(...args)=>notices.push(args);
    context.showClipboardFallback=(...args)=>fallbacks.push(args);
    const button={disabled:false};
    await context.copyShareLinks(entries,button);
    assert.equal(written,'a.txt\nhttps://example.test/a#key\n\nb.txt\nhttps://example.test/b?name=b');
    assert.equal(notices.length,1);
    context.navigator.clipboard.writeText=async()=>{throw new Error('denied');};
    await context.copyShareLinks(entries,button);
    assert.equal(notices.length,1);
    assert.equal(fallbacks[0][1],written);
    assert.equal(button.disabled,false);
});
