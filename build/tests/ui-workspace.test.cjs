const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

test('upload summary follows enabled limits without exposing the password', () => {
    const fields = {
        uploadSummary:{classList:{toggle(){}}},
        allowedDownloads:{value:'2',disabled:false,checkValidity:()=>true},
        expiryDays:{value:'14',disabled:false,checkValidity:()=>true},
        password:{value:'private-fixture',disabled:false}
    };
    const messages = JSON.parse(fs.readFileSync(path.join(__dirname,'../../internal/i18n/locales/en.json'),'utf8'));
    const context = vm.createContext({document:{getElementById:id=>fields[id]},t:(key,args={})=>messages[key].replace(/\{(\w+)\}/g,(_,name)=>args[name])});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_workspace.js'),'utf8'),context);
    context.updateUploadSummary();
    assert.match(fields.uploadSummary.textContent,/Downloads: 2/);
    assert.match(fields.uploadSummary.textContent,/Password protected/);
    assert.doesNotMatch(fields.uploadSummary.textContent,/private-fixture/);
    fields.allowedDownloads.disabled=true;
    fields.expiryDays.disabled=true;
    fields.password.disabled=true;
    context.updateUploadSummary();
    assert.match(fields.uploadSummary.textContent,/Unlimited downloads · No expiry · No password/);
    fields.allowedDownloads.disabled=false;
    fields.allowedDownloads.value='';
    context.updateUploadSummary();
    assert.equal(fields.uploadSummary.textContent,messages['files.summaryInvalid']);
});

test('upload admission reveals invalid enabled rules and respects encryption readiness', () => {
    let clicks = 0, focused = '', reported = 0;
    const field = (id, value, type = 'number') => ({value, type, disabled:false,
        checkValidity(){return Number(this.value) >= 1;},
        focus(){focused=id;}, reportValidity(){reported++;}});
    const fields = {
        uploadSummary:{classList:{toggle(){}}}, uploadSettings:{open:false},
        selectUploadFiles:{disabled:false},
        allowedDownloads:field('downloads','0'), expiryDays:field('expiry','14'),
        password:{value:'',disabled:true,type:'text'}
    };
    const context = vm.createContext({document:{getElementById:id=>fields[id]}, t:key=>key,
        dropzoneObject:{disabled:false,hiddenFileInput:{click(){clicks++;}}}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_workspace.js'),'utf8'),context);
    context.chooseUploadFiles();
    assert.equal(clicks,0);
    assert.equal(fields.uploadSettings.open,true);
    assert.equal(focused,'downloads');
    assert.equal(reported,1);
    fields.allowedDownloads.disabled=true;
    context.chooseUploadFiles();
    assert.equal(clicks,1);
    fields.selectUploadFiles.disabled=true;
    context.chooseUploadFiles();
    assert.equal(clicks,1);
    fields.selectUploadFiles.disabled=false;
    context.dropzoneObject.disabled=true;
    context.chooseUploadFiles();
    assert.equal(clicks,1);
    context.dropzoneObject.disabled=false;
    fields.password.disabled=false;
    assert.equal(context.validateUploadRules(),false);
    fields.password.value='fixture-password';
    assert.equal(context.validateUploadRules(),true);
});

test('file counts reflect filtered rows and clearing search restores focus', () => {
    const fields = {fileCount:{}, clearFileSearch:{}, input:{value:'missing',focus(){this.focused=true;}}};
    let query='missing', draws=0;
    const table = {rows:options=>({count:()=>options ? 0 : 3}),
        search(value){if(value===undefined)return query; query=value; return this;}, draw(){draws++;}};
    const context=vm.createContext({document:{getElementById:id=>fields[id],querySelector:()=>fields.input},
        $:()=>({DataTable:()=>table})});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_workspace.js'),'utf8'),context);
    context.updateFileCount(table);
    assert.equal(fields.fileCount.textContent,'0 / 3');
    assert.equal(fields.clearFileSearch.hidden,false);
    context.clearFileSearch();
    context.updateFileCount(table);
    assert.equal(query,'');
    assert.equal(draws,1);
    assert.equal(fields.input.focused,true);
    assert.equal(fields.fileCount.textContent,'3');
    assert.equal(fields.clearFileSearch.hidden,true);
});

test('file decoration preserves untrusted filenames as text and is idempotent', () => {
    const name='<img src=x onerror=alert(1)>.pdf';
    let nodes=[], replacements=0;
    const cell={textContent:name,querySelector:()=>nodes.find(n=>n.className==='file-name'),
        replaceChildren(...children){nodes=children; replacements++;}};
    const context=vm.createContext({document:{createElement:tag=>({tag,setAttribute(){}})}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_workspace.js'),'utf8'),context);
    context.decorateFileName(cell);
    context.decorateFileName(cell);
    assert.equal(replacements,1);
    assert.equal(nodes[1].textContent,name);
    assert.equal(nodes[1].title,name);
    assert.equal(nodes[1].innerHTML,undefined);
    assert.match(nodes[0].className,/bi-file-earmark-pdf/);
});

test('uploaded and removed rows redraw through DataTables so search and counts remain current', () => {
    let added, removed, redraws=0, scheduled;
    const api={draw(reset){assert.equal(reset,false);redraws++;return this;}};
    const rowApi=row=>({remove(){removed=row;return api;}});
    rowApi.add=row=>{added=row;return api;};
    const context=vm.createContext({$:()=>({DataTable:()=>({row:rowApi})}),
        setTimeout:callback=>{scheduled=callback;}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_upload.js'),'utf8'),context);
    const row={classList:{add(){}}};
    context.changeRowCount(true,row);
    assert.equal(added,row);
    assert.equal(redraws,1);
    context.changeRowCount(false,row);
    assert.equal(removed,undefined);
    scheduled();
    assert.equal(removed,row);
    assert.equal(redraws,2);
});

test('replacement choices include filtered rows and use plain filename text', () => {
    const cells=[{id:'cell-name-one',innerHTML:'<span>one.txt</span>',textContent:'one.txt'},
        {id:'cell-name-two',innerHTML:'&lt;b&gt;.txt',textContent:'<b>.txt'}];
    // DataTables returns an array-like API object, not a JavaScript iterable.
    const nodes={0:{querySelector:()=>cells[0]},1:{querySelector:()=>cells[1]},length:2};
    const context=vm.createContext({$:()=>({DataTable:()=>({rows:()=>({nodes:()=>nodes})})})});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_upload.js'),'utf8'),context);
    assert.deepEqual(JSON.parse(JSON.stringify(context.getAllAvailableFiles())),[['one','two'],['one.txt','<b>.txt']]);
});
