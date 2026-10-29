const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const base = path.join(__dirname, '../../internal/webserver/web/static/js');
const common = fs.readFileSync(path.join(base, 'all_public.js'), 'utf8');
const upload = fs.readFileSync(path.join(base, 'public_upload.js'), 'utf8');
const messages = JSON.parse(fs.readFileSync(path.join(__dirname, '../../internal/i18n/locales/zh-CN.json'), 'utf8'));

test('real upload error parser translates quota and expiry while keeping diagnostics', async () => {
    const sandbox = vm.createContext({window:{gokapiMessages:messages}, document:{querySelectorAll:()=>[]}});
    vm.runInContext(common+'\n'+upload+'\nvar modalCode; showModal = code => modalCode = code;', sandbox);
    sandbox.input = {Result:'error',ErrorCode:21,ErrorMessage:'quota detail'};
    let error = await vm.runInContext('parseErrorResponse({text:async()=>JSON.stringify(input)})', sandbox);
    assert.equal(error.message,'上传容量或文件数量已达到上限');
    assert.equal(error.raw.ErrorMessage,'quota detail');
    sandbox.input = {Result:'error',ErrorCode:14,ErrorMessage:'expired'};
    error = await vm.runInContext('parseErrorResponse({text:async()=>JSON.stringify(input)})', sandbox);
    assert.equal(error.message,'文件收集已过期');
    assert.equal(vm.runInContext('modalCode',sandbox),'expired');
    sandbox.input = {Result:'error',ErrorCode:999,ErrorMessage:'specific detail'};
    error = await vm.runInContext('parseErrorResponse({text:async()=>JSON.stringify(input)})',sandbox);
    assert.equal(error.message,'specific detail');
});
