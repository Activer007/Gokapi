const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const base=path.join(__dirname,'../../internal/webserver/web/static/js');
const settle=()=>new Promise(resolve=>setImmediate(resolve));

function snapshot(id,ids=[],changes={}) {
    return {id,userid:7,name:`收集 ${id}`,uploadedfiles:ids.length,reserveduploads:0,maxfiles:0,
        totalfilesize:ids.length,lastupload:1000,expiry:0,fileidlist:ids,...changes};
}

function element(extra={}) {
    return Object.assign({dataset:{},textContent:'',value:'',hidden:true,disabled:false,children:[],attributes:{},
        classList:{contains(){return false;},add(){},remove(){}},
        setAttribute(key,value){this.attributes[key]=String(value);},removeAttribute(key){delete this.attributes[key];},
        appendChild(child){this.children.push(child);},querySelector(){return null;}},extra);
}

function arrivalContext(initial={r:[]}) {
    const nodes={},rows=[],listeners={},timers=[];
    for(const id of ['collectionRefresh','collectionRefreshStatus','collectionUpdatedAt']) nodes[id]=element();
    const addRequest=(id,ids=[])=>{
        const data=snapshot(id,ids),actions=element();
        const row=element({id:`row-${id}`,dataset:{...data,fileids:ids.join(',')},querySelector:selector=>selector==='.request-actions'?actions:null});
        delete row.dataset.fileidlist;
        nodes[row.id]=row;rows.push(row);
        for(const stem of ['collection-status-','collection-arrivals-','collection-arrivals-count-','collection-arrivals-view-',
            'collection-arrivals-detail-','collection-inflight-','collection-empty-','collection-max-',
            'collection-count-','cell-lastupdate-','cell-expiry-','totalFiles-fr-','collection-size-']) nodes[stem+id]=element();
        nodes[`collection-name-${id}`]=element({textContent:data.name});
        nodes[`collapse-filelist-${id}`]=element({shareLinks:new Map([['source',{id:'ordinary-share'}]]),shareBatch:[{id:'source'}]});
        return row;
    };
    for(const [id,ids] of Object.entries(initial)) addRequest(id,ids);
    nodes.deliveryTitle=element({value:'正在编辑的交付'});
    const document={visibilityState:'visible',getElementById:id=>nodes[id]??null,
        querySelectorAll:selector=>selector==='[data-collection-summary]'?rows:[],createElement:()=>element(),
        addEventListener:(event,callback)=>{listeners[event]=callback;}};
    const context=vm.createContext({document,t:(key,values={})=>key+JSON.stringify(values),
        formatUnixTimestamp:stamp=>String(stamp),formatTimestampWithNegative:(stamp,empty)=>stamp?String(stamp):empty,getReadableSize:bytes=>String(bytes),
        setInterval:(callback,delay)=>{timers.push({callback,delay,active:true});return timers.length;},
        clearInterval:id=>{if(timers[id-1])timers[id-1].active=false;}});
    for(const file of ['admin_ui_collection_status.js','admin_ui_collection_arrivals.js']) vm.runInContext(fs.readFileSync(path.join(base,file),'utf8'),context);
    const init=()=>{context.initCollectionStatus(1000);context.initCollectionArrivals();};
    const pending=id=>vm.runInContext(`collectionArrivalStates.get(${JSON.stringify(id)})?.pendingIDs.size`,context);
    return {context,nodes,rows,timers,listeners,document,addRequest,init,pending};
}

test('initial history is silent and a zero-to-one receipt stays pending until its row is successfully viewed',async()=>{
    const {context,nodes,init,pending}=arrivalContext({r:['historic'],empty:[]});
    let refreshes=0;
    context.apiURequestList=async()=>{refreshes++;return {requests:[snapshot('r',['historic']),snapshot('empty',['first'])],serverTime:1000};};
    init();
    assert.equal(refreshes,0);
    assert.equal(nodes['collection-arrivals-r'].hidden,true);
    assert.equal(pending('r'),0);
    const panel=nodes['collapse-filelist-empty'],links=panel.shareLinks;
    assert.equal(await context.refreshCollectionSummaries(),true);
    assert.equal(pending('empty'),1);
    assert.equal(nodes['collection-arrivals-empty'].hidden,false);
    assert.match(nodes['collection-arrivals-count-empty'].textContent,/"count":1/);
    const loads=[];
    context.loadCollectionArrivalFiles=async(id,ids)=>{loads.push([id,Array.from(ids)]);return {shown:[],unavailable:[],failed:ids};};
    assert.equal(await context.viewCollectionArrivals('empty'),false);
    assert.equal(pending('empty'),1);
    assert.match(nodes['collection-arrivals-detail-empty'].textContent,/collection.arrivals.retry/);
    context.loadCollectionArrivalFiles=async(id,ids)=>{loads.push([id,Array.from(ids)]);return {shown:ids,unavailable:[],failed:[]};};
    assert.equal(await context.viewCollectionArrivals('empty'),true);
    assert.deepEqual(loads,[['empty',['first']],['empty',['first']]]);
    assert.equal(pending('empty'),0);
    assert.equal(nodes['collection-arrivals-empty'].hidden,true);
    assert.equal(panel.shareLinks,links);
    assert.equal(nodes.deliveryTitle.value,'正在编辑的交付');
});

test('cumulative file IDs detect same-second and delete/add replacements without repeating restored history',async()=>{
    const {context,nodes,init,pending}=arrivalContext({r:['a']});init();
    context.loadCollectionArrivalFiles=async(_id,ids)=>({shown:ids,unavailable:[],failed:[]});
    context.processCollectionArrivalSnapshots([snapshot('r',['b'])]); // same count and lastupload as the baseline
    assert.equal(pending('r'),1);
    assert.match(nodes['collection-arrivals-count-r'].textContent,/"count":1/);
    await context.viewCollectionArrivals('r');
    context.processCollectionArrivalSnapshots([snapshot('r',['a','b','c'])]);
    assert.equal(pending('r'),1); // a was seen before deletion, only c is new
    await context.viewCollectionArrivals('r');
    context.processCollectionArrivalSnapshots([snapshot('r',['a','b'])]);
    context.processCollectionArrivalSnapshots([snapshot('r',['a','b','c'])]);
    assert.equal(pending('r'),0);
    assert.equal(nodes['collection-arrivals-r'].hidden,true);
});

test('failed polling preserves the baseline, hidden pages stay quiet, and visible/manual refresh share one request',async()=>{
    const {context,nodes,document,listeners,timers,init,pending}=arrivalContext();init();
    let calls=0,finish;
    context.apiURequestList=async()=>{calls++;throw Object.assign(new Error('PRIVATE_KEY'),{status:503});};
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.equal(pending('r'),0);
    assert.doesNotMatch(nodes.collectionRefreshStatus.textContent,/PRIVATE_KEY/);
    const poll=timers.find(timer=>timer.delay===60000);
    assert.ok(poll);
    document.visibilityState='hidden';poll.callback();await settle();
    assert.equal(calls,1);
    context.apiURequestList=()=>{calls++;return new Promise(resolve=>{finish=resolve;});};
    document.visibilityState='visible';listeners.visibilitychange();
    const manual=context.refreshCollectionSummaries();
    poll.callback();
    assert.equal(calls,2);
    finish({requests:[snapshot('r',['after-outage'])],serverTime:1000});
    assert.equal(await manual,true);
    assert.equal(pending('r'),1);
    assert.equal(nodes['collection-arrivals-r'].hidden,false);
});

test('a withdrawn collection scope suppresses its pending notice and authentication failure stops further polling',async()=>{
    const {context,nodes,timers,init,pending}=arrivalContext();init();
    context.processCollectionArrivalSnapshots([snapshot('r',['new'])]);
    assert.equal(pending('r'),1);
    context.processCollectionArrivalSnapshots([]);
    assert.equal(context.collectionArrivalCanView('r'),false);
    assert.equal(nodes['collection-arrivals-r'].hidden,true);
    context.loadCollectionArrivalFiles=()=>{throw new Error('must not read a withdrawn scope');};
    assert.equal(await context.viewCollectionArrivals('r'),false);
    context.processCollectionArrivalSnapshots([snapshot('r',['new','during-withdrawal'])]);
    assert.equal(nodes['collection-arrivals-r'].hidden,true);
    assert.equal(pending('r'),0);
    context.processCollectionArrivalSnapshots([snapshot('r',['new','during-withdrawal','later'])]);
    let calls=0;
    context.apiURequestList=async()=>{calls++;throw Object.assign(new Error('forbidden'),{status:403});};
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.equal(context.collectionArrivalCanView('r'),false);
    assert.equal(nodes['collection-arrivals-r'].hidden,true);
    assert.equal(timers.find(timer=>timer.delay===60000).active,false);
    assert.equal(await context.refreshCollectionSummaries(),false);
    assert.equal(calls,1);
});

test('newly visible unknown scopes establish a quiet baseline while a locally created empty scope can report its first receipt',()=>{
    const {context,nodes,addRequest,init,pending}=arrivalContext();init();
    context.processCollectionArrivalSnapshots([snapshot('r'),snapshot('unknown',['historical'])]);
    assert.equal(pending('unknown'),0);
    assert.equal(nodes['row-unknown'],undefined);
    assert.equal(vm.runInContext("collectionArrivalStates.get('unknown').seenIDs.has('historical')",context),true);
    addRequest('local');
    context.seedCollectionArrivalBaseline('local',[]);
    context.processCollectionArrivalSnapshots([snapshot('r'),snapshot('unknown',['historical']),snapshot('local',['first-local-upload'])]);
    assert.equal(pending('local'),1);
    assert.equal(nodes['collection-arrivals-local'].hidden,false);
});

test('fifty unavailable arrivals rotate behind a later valid file without issuing automatic extra requests',async()=>{
    const {context,nodes,init,pending}=arrivalContext();init();
    const unavailable=Array.from({length:50},(_,index)=>`unavailable-${index}`);
    context.processCollectionArrivalSnapshots([snapshot('r',[...unavailable,'tail-valid'])]);
    const calls=[],opened=[];
    nodes['collapse-filelist-r'].classList.add=name=>opened.push(name);
    context.loadCollectionArrivalFiles=async(_requestId,ids)=>{
        calls.push(Array.from(ids));
        // Preserve the reader's existing 50-item cap; the controller must
        // fairly select which pending items reach that bounded reader.
        const attempted=Array.from(ids).slice(0,50);
        const rejected=attempted.filter(id=>id!=='tail-valid');
        return {shown:attempted.filter(id=>id==='tail-valid'),unavailable:rejected.slice(0,25),failed:rejected.slice(25)};
    };
    assert.equal(await context.viewCollectionArrivals('r'),false);
    assert.deepEqual(calls[0],unavailable);
    assert.equal(pending('r'),51);
    assert.deepEqual(opened,[]);
    await settle();
    assert.equal(calls.length,1);
    assert.equal(await context.viewCollectionArrivals('r'),false);
    assert.equal(calls[1][0],'tail-valid');
    assert.equal(calls[1].length,50);
    assert.deepEqual(opened,['show']);
    const remaining=Array.from(vm.runInContext("collectionArrivalStates.get('r').pendingIDs",context));
    assert.deepEqual(new Set(remaining),new Set(unavailable));
    assert.equal(pending('r'),50);
    assert.equal(nodes['collection-arrivals-view-r'].disabled,false);
    assert.match(nodes['collection-arrivals-detail-r'].textContent,/collection.arrivals.retry/);
    await settle();
    assert.equal(calls.length,2);
});
