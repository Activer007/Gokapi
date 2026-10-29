const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const base=path.join(__dirname,'../../internal/webserver/web/static/js');
function setup(language='zh-CN') {
    const messages=JSON.parse(fs.readFileSync(path.join(__dirname,`../../internal/i18n/locales/${language}.json`),'utf8'));
    const context=vm.createContext({TextEncoder,btoa,window:{gokapiMessages:messages},document:{documentElement:{lang:language}}});
    for(const file of ['all_public.js','admin_api.js','admin_ui_allPages.js'])
        vm.runInContext(fs.readFileSync(path.join(base,file),'utf8'),context);
    return context;
}
test('calendar and table component settings follow selected locale',()=>{
    const context=setup();
    assert.equal(vm.runInContext('calendarLanguage().months.longhand[9]',context),'十月');
    assert.equal(vm.runInContext('calendarLanguage().weekdays.longhand[1]',context),'星期一');
    assert.equal(vm.runInContext('tableLanguage().search',context),'搜索：');
    assert.equal(vm.runInContext('calendarLanguage()',setup('en')),'default');
});

test('names use UTF-8 encoded headers and status durations follow the locale',async()=>{
    const context=setup();
    let headers;
    context.fetch=async(_url,options)=>{headers=options.headers;return {ok:true,json:async()=>({id:123})};};
    vm.runInContext("storedTokens.set('PERM_MANAGE_USERS',{key:'fixture',expiry:Date.now()/1000+600})",context);
    await vm.runInContext("apiUserCreate('中文用户😀')",context);
    assert.equal(Buffer.from(headers.username.slice(7),'base64').toString('utf8'),'中文用户😀');
    vm.runInContext(fs.readFileSync(path.join(base,'admin_ui_logs.js'),'utf8'),context);
    assert.equal(vm.runInContext('formatDuration(3665)',context),'1小时 1分钟');
    assert.equal(vm.runInContext('formatTimestampWithNegative(0)',context),'从未');
    const english=setup('en');
    vm.runInContext(fs.readFileSync(path.join(base,'admin_ui_logs.js'),'utf8'),english);
    assert.equal(vm.runInContext('formatDuration(3665)',english),'1h 1m');
});
test('API failures translate codes, preserve status/details, and read body once',async()=>{
    const context=setup();
    context.response={status:413,text:async()=>JSON.stringify({ErrorCode:21,ErrorMessage:'quota detail'})};
    let error=await vm.runInContext('apiFailure(response)',context);
    assert.equal(error.message,'上传容量或文件数量已达到上限');
    assert.equal(error.raw.ErrorMessage,'quota detail');
    assert.equal(error.status,413);
    let reads=0;
    context.response={status:502,text:async()=>{assert.equal(++reads,1);return 'proxy detail';}};
    error=await vm.runInContext('apiFailure(response)',context);
    assert.equal(error.message,'proxy detail');
    context.response={status:503,text:async()=>''};
    error=await vm.runInContext('apiFailure(response)',context);
    assert.equal(error.message,'请求失败，状态码：503');
});
