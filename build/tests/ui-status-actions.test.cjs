const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_logs.js'),'utf8');
function setup() {
    const handlers={};
    const elements={statusActionModal:{addEventListener:(name,callback)=>handlers[name]=callback},statusActionConfirm:{},statusActionTitle:{},statusActionHelp:{},statusActionError:{},statusActionResult:{},deleteLogsSel:{value:'all'}};
    const calls=[];
    const context=vm.createContext({document:{getElementById:id=>elements[id]},t:key=>key,
        bootstrap:{Modal:{getOrCreateInstance:()=>({show:()=>calls.push('show')})}},
        runModalSave:async (id,button,action)=>action(),apiLogsDelete:async timestamp=>calls.push(['delete',timestamp]),apiLogResetTraffic:async()=>calls.push('reset')});
    vm.runInContext(source,context);
    context.loadLogs=async timestamp=>calls.push(['refreshLogs',timestamp]);
    context.loadStatus=async()=>calls.push('refreshStatus');
    return {context,elements,handlers,calls};
}
test('opening and cancelling log deletion never calls the delete API',()=>{
    const {context,elements,handlers,calls}=setup();
    let focused=false;
    context.deleteLogs({isConnected:true,focus:()=>focused=true});
    assert.deepEqual(calls,['show']);
    handlers['hidden.bs.modal']();
    assert.deepEqual(calls,['show']);
    assert.equal(elements.deleteLogsSel.value,'none');
    assert.equal(elements.statusActionConfirm.onclick,null);
    assert.equal(focused,true);
});
test('log deletion submits the selected cutoff only after explicit confirmation',async()=>{
    const {context,elements,calls}=setup();
    elements.deleteLogsSel.value='7';
    const before=Math.floor(Date.now()/1000)-7*86400;
    context.deleteLogs();
    assert.deepEqual(calls,['show']);
    await elements.statusActionConfirm.onclick();
    assert.equal(calls[1][0],'delete');
    assert.ok(calls[1][1]>=before && calls[1][1]<=before+1);
    assert.deepEqual(calls[2],['refreshLogs',0]);
    assert.equal(elements.statusActionResult.textContent,'status.logsDeleted');
});
test('invalid log selection is inert and traffic reset also waits for confirmation',async()=>{
    const {context,elements,calls}=setup();
    elements.deleteLogsSel.value='none';
    context.deleteLogs();
    assert.deepEqual(calls,[]);
    context.resetTrafficStat();
    assert.deepEqual(calls,['show']);
    await elements.statusActionConfirm.onclick();
    assert.deepEqual(calls,['show','reset','refreshStatus']);
    assert.equal(elements.statusActionResult.textContent,'status.trafficReset');
});
