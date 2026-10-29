const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../../internal/configuration/setup/static/setup/ui.js'),'utf8');
function setup(extra={}) {
    const state={navigation:null,alerts:0,confirmations:0};
    const messages=JSON.parse(fs.readFileSync(path.join(__dirname,'../../internal/i18n/locales/zh-CN.json'),'utf8'));
    const context=vm.createContext({URL,document:{documentElement:{lang:'en'}},window:{
        gokapiMessages:messages,location:{href:'http://localhost/setup/start?x=1#step',assign:u=>state.navigation=u},
        alert:()=>state.alerts++,confirm:()=>{state.confirmations++;return false},...extra
    }});
    vm.runInContext(source,context);return {context,state};
}
test('setup language navigation preserves URL and protects dirty/submitting forms',()=>{
    const {context,state}=setup();vm.runInContext("switchSetupLanguage('zh-CN')",context);
    assert.equal(state.navigation,'http://localhost/setup/start?x=1&lang=zh-CN#step');
    const dirty=setup({setupDirty:true});vm.runInContext("switchSetupLanguage('zh-CN')",dirty.context);
    assert.equal(dirty.state.navigation,null);assert.equal(dirty.state.confirmations,1);
    const busy=setup({setupSubmitting:true});vm.runInContext("switchSetupLanguage('zh-CN')",busy.context);
    assert.equal(busy.state.navigation,null);assert.equal(busy.state.alerts,1);
});
test('setup translates known responses while preserving provider diagnostics',()=>{
    const {context}=setup();
    assert.equal(vm.runInContext("setupResponseMessage({messageKey:'setup.awsTestsOK',result:'All tests OK.'})",context),'全部测试通过。');
    assert.equal(vm.runInContext("setupResponseMessage({result:'provider detail'})",context),'provider detail');
    assert.equal(vm.runInContext("t('setup.minLength',{count:8})",context),'长度至少为 8 个字符');
});
