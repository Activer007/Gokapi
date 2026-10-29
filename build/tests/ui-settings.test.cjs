const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = name => fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/',name),'utf8');

test('permission batch respects dependencies and retries only unconfirmed changes', async () => {
    const calls=[];
    let fail=true;
    const context=vm.createContext({apiUserModify:async(id,key,modifier)=>{
        calls.push([id,key,modifier]);
        if(fail && key==='PERM_REPLACE_OTHER') throw new Error('network failure');
    }});
    vm.runInContext(source('admin_ui_permissions.js'),context);
    const draft=[
        {key:'PERM_REPLACE_OTHER',checked:true,granted:false},
        {key:'PERM_REPLACE',checked:true,granted:false},
        {key:'PERM_USERS',checked:true,granted:false,locked:true}
    ];
    const saved=[];
    await assert.rejects(context.applyPermissionChanges('user','fixture',draft,p=>saved.push(p.key)),/network failure/);
    assert.deepEqual(saved,['PERM_REPLACE']);
    assert.equal(draft[0].granted,false);
    fail=false;
    await context.applyPermissionChanges('user','fixture',draft,p=>saved.push(p.key));
    assert.deepEqual(calls.map(c=>c[1]),['PERM_REPLACE','PERM_REPLACE_OTHER','PERM_REPLACE_OTHER']);
    calls.length=0;
    draft[0].checked=draft[1].checked=false;
    await context.applyPermissionChanges('user','fixture',draft,()=>{});
    assert.deepEqual(calls.map(c=>c.slice(1)),[['PERM_REPLACE_OTHER','REVOKE'],['PERM_REPLACE','REVOKE']]);
});

test('modal save blocks duplicate submission and dismissal; failure preserves draft and retry closes', async () => {
    const input={disabled:false,value:'draft name'}, locked={disabled:true};
    const button={disabled:false,textContent:'Save'};
    const error={textContent:''};
    const listeners={};
    let hidden=0;
    const modal={dataset:{},querySelector:()=>error,querySelectorAll:()=>[input,locked,button],
        setAttribute(){},removeAttribute(){},addEventListener:(event,fn)=>listeners[event]=fn,
        removeEventListener:event=>delete listeners[event]};
    const context=vm.createContext({document:{getElementById:()=>modal},window:{},t:key=>key,
        bootstrap:{Modal:{getOrCreateInstance:()=>({hide(){hidden++;}})}}});
    vm.runInContext(source('admin_ui_forms.js'),context);
    let reject,calls=0;
    const pending=context.runModalSave('fixture',button,()=>{calls++;return new Promise((resolve,no)=>reject=no);});
    assert.equal(context.window.gokapiFormBusy,true);
    assert.equal(input.disabled,true);
    let prevented=false;
    listeners['hide.bs.modal']({preventDefault(){prevented=true;}});
    assert.equal(prevented,true);
    await context.runModalSave('fixture',button,()=>calls++);
    assert.equal(calls,1);
    reject(new Error('try again'));
    assert.equal(await pending,false);
    assert.equal(hidden,0);
    assert.equal(input.value,'draft name');
    assert.equal(input.disabled,false);
    assert.equal(locked.disabled,true);
    assert.equal(error.textContent,'try again');
    assert.equal(context.window.gokapiFormBusy,false);
    assert.equal(await context.runModalSave('fixture',button,async()=>{}),true);
    assert.equal(hidden,1);
    assert.equal(error.textContent,'');
});
