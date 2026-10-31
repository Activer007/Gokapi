const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_api.js'),'utf8');

function sharingContext() {
    const context=vm.createContext({t:key=>key});
    vm.runInContext(source,context);
    return context;
}

test('sharing feedback maps existing error categories and transport status to safe messages',()=>{
    const {shareFailureMessage}=sharingContext();
    for (const [error,key] of [
        [{code:21,status:413},'sharing.errorQuota'],
        [{code:6,status:401},'sharing.errorPermission'],
        [{status:403},'sharing.errorPermission'],
        [{code:5,status:404},'sharing.errorUnavailable'],
        [{status:409},'sharing.errorUnavailable'],
        [{code:22,status:503},'sharing.errorBusy'],
        [new TypeError('Failed to fetch'),'sharing.errorNetwork'],
    ]) assert.equal(shareFailureMessage(error),key);
});

test('sharing feedback never exposes raw exceptions, response bodies or credentials',()=>{
    const {shareFailureMessage}=sharingContext();
    const sensitive='SENSITIVE_SENTINEL /srv/private/config.json apikey=fixture-secret';
    const error=Object.assign(new Error(sensitive),{status:500,raw:{ErrorCode:999,ErrorMessage:sensitive}});
    assert.equal(shareFailureMessage(error),'sharing.errorGeneric');
    error.code=21;
    assert.equal(shareFailureMessage(error),'sharing.errorQuota');
    assert.equal(shareFailureMessage(null),'sharing.errorGeneric');
});
