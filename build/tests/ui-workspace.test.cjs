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
