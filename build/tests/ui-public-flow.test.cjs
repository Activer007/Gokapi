const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const templates = path.join(__dirname, '../../internal/webserver/web/templates');
const read = name => fs.readFileSync(path.join(templates,name),'utf8');

test('password-protected download transfers the password and disables its actual submit control', () => {
    const template = read('html_download_password.tmpl');
    const elements = Object.fromEntries([...template.matchAll(/id="([^"]+)"/g)].map(match => [match[1], {}]));
    elements.passwordFile.value = 'disposable-download-password';
    const script = [...template.matchAll(/<script>([\s\S]*?)<\/script>/g)].find(match => match[1].includes('function submitForm'))[1];
    const context = vm.createContext({document:{getElementById:id=>elements[id]}});
    vm.runInContext(script,context);
    assert.equal(context.submitForm(),true);
    assert.equal(elements.pw_hidden.value,'disposable-download-password');
    assert.equal(elements.passwordFile.disabled,true);
    assert.equal(elements.submitbutton.disabled,true);
});

test('mismatched password submission shows feedback, focuses confirmation and does not submit', () => {
    const elements = {newpw:{value:'first'},newpw_c:{value:'different',focus(){this.focused=true;}},pwnotsame:{style:{}},submitbutton:{disabled:false}};
    const context = vm.createContext({document:{getElementById:id=>elements[id]}});
    const script=read('html_changepw.tmpl').match(/<script>([\s\S]*?)<\/script>/)[1];
    vm.runInContext(script,context);
    assert.equal(context.submitForm(),false);
    assert.equal(elements.pwnotsame.style.display,'block');
    assert.equal(elements.newpw_c.focused,true);
    assert.equal(elements.submitbutton.disabled,false);
});

test('download WASM failures display an error and leave the transfer unavailable', async () => {
    const template = read('html_download.tmpl');
    const script = template.slice(template.indexOf('async function initialiseDownload()'),template.indexOf('        initialiseDownload();'));
    for (const failure of ['fetch','compile','http']) {
        let error, ran=false;
        const elements={wasmspinner:{style:{}},downloadbutton:{disabled:true}};
        const context=vm.createContext({WASM_URL:'fixture.wasm',wasm:undefined,t:key=>key,
            fetch:async()=>{if(failure==='fetch')throw new Error('network');return {ok:failure!=='http'};},
            WebAssembly:{instantiateStreaming:async()=>{throw new Error('compile');}},
            go:{importObject:{},run(){ran=true;return Promise.resolve();}},
            displayError:value=>error=value,document:{getElementById:id=>elements[id]}});
        vm.runInContext(script,context);
        await context.initialiseDownload();
        assert.ok(error);
        assert.equal(ran,false);
        assert.equal(elements.downloadbutton.disabled,true);
    }
});

test('admin WASM rejection never enables encrypted uploads or continues setup', async () => {
    let error, continued=false;
    const context=vm.createContext({Go:function(){this.importObject={};},fetch:async()=>{throw new Error('fixture network failure');}});
    vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/end2end_admin.js'),'utf8'),context);
    context.displayError=value=>error=value;
    await context.loadWasm(()=>continued=true);
    assert.ok(error);
    assert.equal(continued,false);
});

test('encrypted transfer failure aborts output and never displays completion, including empty rejection reasons', async () => {
    const template=read('html_download.tmpl');
    let script=template.slice(template.indexOf('async function DownloadEncrypted()'),template.indexOf('        function Download(button)'));
    script=script.replace(/{{ if .EndToEndEncryption }}([\s\S]*?){{ else }}[\s\S]*?{{ end }}/g,'$1')
        .replace(/{{ if .UsesHttps }}[\s\S]*?{{ else }}([\s\S]*?){{ end }}/g,'$1')
        .replace(/{{\s*.Id\s*}}/g,'fixture');
    for (const reason of [undefined, new Error('truncated ciphertext')]) {
        const calls=[];
        const reader={read:async()=>{throw reason;},cancel:async()=>calls.push('cancel')};
        const writer={write:async()=>calls.push('write'),close:async()=>calls.push('close'),abort:async()=>calls.push('abort')};
        const elements={downloadStatus:{textContent:'transferring'},buttondiv:{style:{}},errordiv:{style:{}},errormessage:{}};
        const context=vm.createContext({document:{getElementById:id=>elements[id]},window:{},t:key=>key,
            sessionStorage:{getItem:()=> 'synthetic',removeItem(){}},GokapiDecrypt:async()=>({body:{getReader:()=>reader}}),
            streamSaver:{createWriteStream:()=>({getWriter:()=>writer})},console:{log(){},error(){}}});
        vm.runInContext(script,context);
        await context.DownloadEncrypted();
        assert.deepEqual(calls,['cancel','abort']);
        assert.equal(context.window.gokapiTransferBusy,false);
        assert.equal(elements.downloadStatus.textContent,'');
        assert.equal(elements.buttondiv.style.display,'none');
        assert.equal(elements.errordiv.style.display,'block');
        assert.match(elements.errormessage.textContent,/download.transferFailed|truncated ciphertext/);
    }
});
