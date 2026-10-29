const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/end2end_admin.js'),'utf8');

test('E2E file selection stays disabled on cipher failure and enables after initialization', () => {
    const elements={selectUploadFiles:{disabled:true},uploadBoxTitle:{},uploadBoxSubtitle:{}};
    let enabled=false,cipherError=new Error('invalid fixture cipher');
    const context=vm.createContext({document:{getElementById:id=>elements[id]},
        isE2EKeySet:()=>true,loadWasm:callback=>callback(),localStorage:{getItem:()=>''},
        GokapiE2ESetCipher:()=>cipherError,dropzoneObject:{enable(){enabled=true;}},t:key=>key});
    vm.runInContext(source,context);
    context.isE2EKeySet=()=>true;
    context.loadWasm=callback=>callback();
    context.displayError=()=>{};
    context.getE2EInfo=()=>{};
    context.checkIfE2EKeyIsSet();
    assert.equal(enabled,false);
    assert.equal(elements.selectUploadFiles.disabled,true);
    cipherError=null;
    context.checkIfE2EKeyIsSet();
    assert.equal(enabled,true);
    assert.equal(elements.selectUploadFiles.disabled,false);
});

test('E2E decrypt updates copy, QR and email URLs while retaining hidden ID column', () => {
    const link={href:'https://example.test/d/Encrypted%20File',getAttribute(){return this.href;},setAttribute(key,value){this[key]=value;}};
    const copy={setAttribute(key,value){this[key]=value;}};
    const cells=[{id:'cell-name-fixture'}, {}, {}, {}, {}, {querySelector:()=>link}, {querySelector:()=>copy}];
    const qr={},email={};
    let invalidated=0, draws=0;
    const table={rows:()=>({nodes:()=>[{}]}),cell:(row,col)=>({node:()=>cells[col],
        invalidate(source){assert.equal(col,0);assert.equal(source,'dom');invalidated++;}}),
        draw(reset){assert.equal(reset,false);draws++;}};
    let qrUrl;
    const context=vm.createContext({IncludeFilename:true,
        $:value=>value==='#maintable'?{DataTable:()=>table}:{attr:()=>value.id},
        document:{getElementById:id=>id==='qrcode-fixture'?qr:email},showQrCode:url=>qrUrl=url});
    vm.runInContext(source,context);
    context.decryptFileEntries({fixture:['中文.txt','test-cipher']});
    assert.equal(cells[0].textContent,'中文.txt');
    assert.equal(copy['data-clipboard-text'],link.href);
    assert.match(link.href,/#test-cipher$/);
    qr.onclick(); assert.equal(qrUrl,link.href);
    assert.equal(email.href,'mailto:?body='+encodeURIComponent(link.href));
    context.decryptFileEntries({fixture:['中文.txt','test-cipher']});
    assert.equal(link.href.split('#').length,2);
    assert.equal(invalidated,2);
    assert.equal(draws,2);
});
