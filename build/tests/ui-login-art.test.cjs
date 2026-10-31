const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../../internal/webserver/web/static/js/all_public.js'), 'utf8');

function fixture({count = 4, retry = false, previous, random = 0.5, blocked = false, narrow = false, absent = false} = {}) {
    const entries = Array.from({length: count}, (_, i) => ({dataset: {
        loginArtId: 'art-' + i, loginArtSrc: './assets/brand/login/art-' + i + '-v1.webp', position: '50% 50%'
    }}));
    const requests = [], writes = [], classes = new Set();
    const image = {style: {}, classList: {add: x => classes.add(x), remove: x => classes.delete(x)},
        set src(value) { requests.push(value); }, removeAttribute(name) { assert.equal(name, 'src'); }};
    const panel = {dataset: {retry: String(retry)}, querySelectorAll: () => entries};
    const media = {matches: narrow, addEventListener: (_, fn) => { media.listener = fn; },
        removeEventListener: (_, fn) => { assert.equal(fn, media.listener); media.listener = null; }};
    const context = vm.createContext({Math: {floor: Math.floor, random: () => random},
        window: {matchMedia: () => media, sessionStorage: {
            getItem() { if (blocked) throw Error('blocked'); return previous; },
            setItem(key, value) { if (blocked) throw Error('blocked'); writes.push([key, value]); }
        }}, document: {getElementById: id => absent ? null : id === 'loginArt' ? panel : image}});
    vm.runInContext(source, context);
    return {context, requests, writes, classes, image, panel, media};
}

test('login artwork selects every catalog entry, including future additions, with one request', () => {
    for (let i = 0; i < 5; i++) {
        const f = fixture({count: 5, random: (i + 0.5) / 5});
        f.context.initLoginArt();
        f.context.initLoginArt();
        assert.deepEqual(f.requests, ['./assets/brand/login/art-' + i + '-v1.webp']);
        assert.deepEqual(f.writes, [['supashare.loginArt', 'art-' + i]]);
        f.image.onload();
        assert.ok(f.classes.has('is-loaded'));
    }
});

test('failed login reuses a known ID but never treats stored data as a URL', () => {
    const same = fixture({retry: true, previous: 'art-1'});
    same.context.initLoginArt();
    assert.match(same.requests[0], /art-1-v1/);
    for (const previous of ['https://example.com/tracker', 'removed-image', null]) {
        const f = fixture({retry: true, previous});
        f.context.initLoginArt();
        assert.deepEqual(f.requests, ['./assets/brand/login/art-2-v1.webp']);
    }
    const fresh = fixture({previous: 'art-1'});
    fresh.context.initLoginArt();
    assert.match(fresh.requests[0], /art-2-v1/);
});

test('blocked storage does not prevent artwork and failure recovery terminates', () => {
    const f = fixture({blocked: true});
    f.context.initLoginArt();
    f.image.onerror();
    assert.deepEqual(f.requests, ['./assets/brand/login/art-2-v1.webp', './assets/brand/login/art-0-v1.webp']);
    f.image.onerror();
    assert.equal(f.image.onerror, null);
    assert.equal(f.image.onload, null);
    assert.equal(f.classes.size, 0);
    const first = fixture({random: 0});
    first.context.initLoginArt();
    first.image.onerror();
    assert.equal(first.requests.length, 1);
    assert.equal(first.image.onerror, null);
});

test('hidden narrow artwork waits until visible; later resize never rerolls it', () => {
    const f = fixture({narrow: true});
    f.context.initLoginArt();
    assert.equal(f.requests.length, 0);
    f.media.listener();
    assert.equal(f.requests.length, 0);
    f.media.matches = false;
    f.media.listener();
    assert.equal(f.requests.length, 1);
    assert.equal(f.media.listener, null);
});

test('other public pages and empty catalogs perform no artwork or storage work', () => {
    for (const options of [{absent: true}, {count: 0}]) {
        const f = fixture(options);
        f.context.initLoginArt();
        assert.equal(f.requests.length, 0);
        assert.equal(f.writes.length, 0);
    }
});
