const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const base = path.join(__dirname, '../../internal/webserver/web/static/js');

function element(values = {}) {
    return Object.assign({value:'', textContent:'', disabled:false, hidden:false, dataset:{}, attributes:{}, listeners:{},
        setAttribute(name, value) { this.attributes[name] = value; },
        removeAttribute(name) { delete this.attributes[name]; },
        addEventListener(name, listener) { this.listeners[name] = listener; },
        removeEventListener(name) { delete this.listeners[name]; },
        focus() { this.focused = true; }, select() { this.selected = true; }
    }, values);
}

function modal(id) {
    const fields = Object.fromEntries(['username','password','loginUrl'].map(name => [name, element({type:name === 'password' ? 'password' : 'text'})]));
    const copies = Object.fromEntries(['all','username','password','loginUrl'].map(name => [name, element({dataset:{credentialCopy:name}})]));
    const icon = element({className:'bi bi-eye'});
    const toggle = element({querySelector:selector => selector === 'i' ? icon : null});
    const fallbackValue = element();
    const fallback = element({hidden:true, querySelector: selector => selector === 'textarea' ? fallbackValue : null});
    const panel = element({hidden:true,
        querySelector(selector) {
            const match = selector.match(/^\[data-credential-field="(\w+)"\]$/);
            if (match) return fields[match[1]];
            const copy = selector.match(/^\[data-credential-copy="(\w+)"\]$/);
            if (copy) return copies[copy[1]];
            if (selector === '[data-credential-toggle]') return toggle;
            if (selector === '[data-credential-fallback]') return fallback;
            return null;
        },
        querySelectorAll(selector) {
            if (selector === 'input, textarea') return [...Object.values(fields),fallbackValue];
            if (selector === 'button') return [...Object.values(copies),toggle];
            if (selector === '[data-credential-copy]') return Object.values(copies);
            return [];
        }
    });
    const name = element();
    const form = element({reportValidity:() => true, reset() {name.value = '';}});
    const submit = element({textContent:'Save'}), cancel = element(), close = element({hidden:true});
    const error = element(), title = element(), force = element({checked:true}), random = element({checked:false}), resetName = element();
    const entry = id === 'newUserModal' ? form : element();
    const result = element({id, fields,copies,toggle,fallback,fallbackValue,name,form,submit,cancel,close,error,title,force,random,resetName,
        querySelector(selector) {
            if (selector === '[data-user-credentials]') return panel;
            if (selector === '.save-error') return error;
            if (selector === 'form') return id === 'newUserModal' ? form : null;
            if (selector === '.modal-title') return title;
            if (selector === '#forceNewPassword') return force;
            if (selector === '#generateRandomPassword') return random;
            if (selector === '#l_userpwreset') return resetName;
            if (selector === '#resetPasswordButton') return submit;
            return panel.querySelector(selector);
        },
        querySelectorAll(selector) {
            if (selector === '[data-credential-entry], [data-credential-actions]') return [entry,submit,cancel];
            if (selector === '[data-credential-close]') return [close];
            if (selector === 'button, input, textarea, select') return [submit,cancel,close,name,force,random,...Object.values(fields),...Object.values(copies),toggle,fallbackValue];
            return [];
        }
    });
    result.panel = panel;
    result.entry = entry;
    return result;
}

function setup(language = 'en') {
    const create = modal('newUserModal'), reset = modal('resetPasswordModal');
    const elements = {newUserModal:create, resetPasswordModal:reset, e_userName:create.name, newUserForm:create.form,
        mb_addUser:create.submit, l_userpwreset:reset.resetName, resetPasswordButton:reset.submit, generateRandomPassword:reset.random};
    const messages = JSON.parse(fs.readFileSync(path.join(__dirname, `../../internal/i18n/locales/${language}.json`), 'utf8'));
    const copies = [], notices = [], rows = [];
    const context = vm.createContext({document:{getElementById:id => elements[id]}, window:{gokapiMessages:messages},
        navigator:{clipboard:{writeText:async text => copies.push(text)}}, isInternalAuth:true,
        bootstrap:{Modal:{getOrCreateInstance:target => ({show(){target.shown = true;}, hide(){
            let prevented = false;
            target.listeners['hide.bs.modal']?.({preventDefault(){prevented = true;}});
            if (prevented) return;
            target.shown = false;
            target.listeners['hidden.bs.modal']?.();
        }})}}, showToast:(...args) => notices.push(args), console,
        apiUserCreate:async () => ({id:17,name:'canonical-user',permissions:0,password:'Temporary-fixture-password',loginUrl:'https://share.example/prefix/login'}),
        apiUserResetPassword:async () => ({Result:'OK',username:'canonical-user',password:'Replacement-fixture-password',loginUrl:'https://share.example/prefix/login'})});
    for (const file of ['all_public.js','admin_ui_forms.js','admin_ui_users.js']) vm.runInContext(fs.readFileSync(path.join(base,file),'utf8'),context);
    context.addRowUser = (...args) => rows.push(args);
    return {context,create,reset,copies,notices,rows,messages};
}

test('creation immediately presents masked canonical credentials, copy actions and clears on close', async () => {
    const {context,create,copies,rows,messages} = setup();
    let calls = 0;
    const original = context.apiUserCreate;
    context.apiUserCreate = async name => {calls++; assert.equal(name,'Mixed-Case'); return original();};
    context.showAddUserModal();
    create.name.value = ' Mixed-Case ';
    await context.addNewUser();
    assert.equal(create.shown,true);
    assert.equal(rows.length,1);
    assert.equal(create.entry.hidden,true);
    assert.equal(create.submit.hidden,true);
    assert.equal(create.panel.hidden,false);
    assert.equal(create.fields.username.value,'canonical-user');
    assert.equal(create.fields.password.type,'password');
    assert.equal(create.toggle.querySelector('i').className,'bi bi-eye');
    assert.equal(create.toggle.attributes['aria-label'],messages['users.showPassword']);
    assert.equal(create.fields.loginUrl.value,'https://share.example/prefix/login');
    create.toggle.onclick();
    assert.equal(create.fields.password.type,'text');
    assert.equal(create.toggle.attributes['aria-pressed'],'true');
    assert.equal(create.toggle.querySelector('i').className,'bi bi-eye-slash');
    assert.equal(create.toggle.attributes['aria-label'],messages['users.hidePassword']);
    await create.copies.all.onclick();
    assert.equal(copies[0], 'Your SupaShare account is ready.\n\nLogin address: https://share.example/prefix/login\nUsername: canonical-user\nTemporary password: Temporary-fixture-password\n\nPlease choose a new password after signing in for the first time.');
    await create.copies.username.onclick();
    await create.copies.password.onclick();
    await create.copies.loginUrl.onclick();
    assert.deepEqual(copies.slice(1),['canonical-user','Temporary-fixture-password','https://share.example/prefix/login']);
    await context.addNewUser();
    assert.equal(calls,1);
    context.bootstrap.Modal.getOrCreateInstance(create).hide();
    assert.equal(create.fields.password.value,'');
    assert.equal(create.fields.password.type,'password');
    assert.equal(create.toggle.querySelector('i').className,'bi bi-eye');
    assert.equal(create.toggle.attributes['aria-label'],messages['users.showPassword']);
    assert.equal(create.fields.username.value,'');
    assert.equal(create.copies.all.onclick,null);
    assert.equal(create.title.textContent,messages['users.create']);
    context.showAddUserModal();
    assert.equal(create.name.value,'');
    assert.equal(create.panel.hidden,true);
});

test('creation failure keeps the draft, blocks concurrent submission and allows a single retry', async () => {
    const {context,create} = setup();
    context.showAddUserModal();
    create.name.value = 'draft';
    let reject, calls = 0;
    context.apiUserCreate = () => {calls++; return new Promise((_resolve,no) => reject = no);};
    const pending = context.addNewUser();
    await context.addNewUser();
    assert.equal(calls,1);
    context.bootstrap.Modal.getOrCreateInstance(create).hide();
    assert.equal(create.shown,true);
    reject(new Error('network failure'));
    await pending;
    assert.equal(create.name.value,'draft');
    assert.equal(create.submit.hidden,false);
    assert.equal(create.submit.disabled,false);
    assert.equal(create.error.textContent,'network failure');
    context.apiUserCreate = async () => {calls++; return {id:9,name:'draft',permissions:0,password:'new-fixture',loginUrl:'https://configured.example/login'};};
    await context.addNewUser();
    assert.equal(calls,2);
    assert.equal(create.panel.hidden,false);
    assert.equal(create.error.textContent,'');
});

test('random password reset reuses localized delivery and force-only reset closes without credentials', async () => {
    const {context,reset,copies,messages} = setup('zh-CN');
    const calls = [];
    context.apiUserResetPassword = async (id,generate) => {calls.push([id,generate]); return generate ?
        {Result:'OK',username:'测试用户',password:'Reset-fixture',loginUrl:'https://configured.example/share/login'} : {Result:'OK',password:''};};
    context.showResetPwModal(17,'old display');
    await context.resetPw(17,true);
    assert.equal(reset.shown,true);
    assert.equal(reset.title.textContent,messages['users.accountResetReady']);
    assert.equal(reset.fields.username.value,'测试用户');
    assert.equal(reset.fields.password.type,'password');
    await reset.copies.all.onclick();
    assert.equal(copies[0], '你的 SupaShare 账号已开通：\n\n登录地址：https://configured.example/share/login\n用户名：测试用户\n临时密码：Reset-fixture\n\n首次登录后，请按提示修改密码。');
    await context.resetPw(17,true);
    assert.equal(calls.length,1);
    context.bootstrap.Modal.getOrCreateInstance(reset).hide();
    assert.equal(reset.fields.password.value,'');
    context.showResetPwModal(17,'display');
    await context.resetPw(17,false);
    assert.equal(reset.shown,false);
    assert.equal(reset.panel.hidden,true);
    assert.deepEqual(calls,[[17,true],[17,false]]);
});

test('clipboard rejection preserves multiline manual selection and copy retry does not recreate the user', async () => {
    const {context,create,notices} = setup();
    context.showAddUserModal(); create.name.value = 'draft';
    await context.addNewUser();
    context.navigator.clipboard.writeText = async () => {throw new Error('denied');};
    await create.copies.all.onclick();
    assert.equal(notices.length,0);
    assert.equal(create.fallback.hidden,false);
    assert.match(create.fallbackValue.value,/\nUsername: canonical-user\nTemporary password:/);
    assert.equal(create.fallbackValue.selected,true);
    context.navigator.clipboard.writeText = async () => {};
    await create.copies.all.onclick();
    assert.equal(notices.length,1);
    assert.equal(create.fallbackValue.value,'');
    assert.equal(create.fallback.hidden,true);
    context.bootstrap.Modal.getOrCreateInstance(create).hide();
    assert.equal(create.fallbackValue.value,'');
    assert.equal(create.fallback.hidden,true);
});

test('a late clipboard rejection cannot restore secrets into a closed modal', async () => {
    const {context,create,notices} = setup();
    context.showAddUserModal(); create.name.value = 'draft'; await context.addNewUser();
    let reject;
    context.navigator.clipboard.writeText = () => new Promise((_resolve,no) => reject = no);
    const pending = create.copies.all.onclick();
    context.bootstrap.Modal.getOrCreateInstance(create).hide();
    reject(new Error('denied'));
    await pending;
    assert.equal(create.fallbackValue.value,'');
    assert.equal(create.fallback.hidden,true);
    assert.equal(notices.length,0);
});

test('external-auth creation stays compatible and incomplete credential response cannot resubmit creation', async () => {
    const external = setup();
    external.context.isInternalAuth = false;
    external.context.apiUserCreate = async () => ({id:11,name:'external',permissions:0});
    external.context.showAddUserModal(); external.create.name.value = 'external';
    await external.context.addNewUser();
    assert.equal(external.create.shown,false);
    assert.equal(external.create.panel.hidden,true);
    const internal = setup();
    let calls = 0;
    internal.context.apiUserCreate = async () => {calls++; return {id:12,name:'created',permissions:0};};
    internal.context.showAddUserModal(); internal.create.name.value = 'created';
    await internal.context.addNewUser();
    assert.equal(internal.create.submit.hidden,true);
    assert.equal(internal.create.close.hidden,false);
    assert.match(internal.create.error.textContent,/account was saved/);
    await internal.context.addNewUser();
    assert.equal(calls,1);
});
