import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const source=fs.readFileSync(new URL('../webApp/src/wasmJsMain/resources/bootstrap.js',import.meta.url),'utf8');
function fixture(mode='success') {
  const nodes=new Map();
  const listeners=new Map();
  let probes=0,releases=0,reloads=0,focus=0;
  const scripts=[];
  const timers=new Map();let timerId=0;
  for(const id of ['startup','startup-title','startup-message','startup-retry','monee'])nodes.set(id,{
    hidden:id==='startup-retry',textContent:'',attributes:{},events:{},
    setAttribute(k,v){this.attributes[k]=v;},removeAttribute(k){delete this.attributes[k];},
    addEventListener(k,v){this.events[k]=v;},focus(){focus++;}
  });
  const document={
    getElementById(id){return nodes.get(id);},
    createElement(tag){
      if(tag==='script')return {};
      assert.equal(tag,'canvas');
      return {getContext(kind,options){
        probes++;assert.equal(kind,'webgl2');assert.equal(options.failIfMajorPerformanceCaveat,false);
        if(mode==='throws')throw new Error('synthetic context error');
        if(mode==='null'||mode==='webgl1')return null;
        return {isContextLost(){return mode==='lost';},getExtension(name){
          assert.equal(name,'WEBGL_lose_context');
          if(mode==='no-release')return null;
          return {loseContext(){releases++;if(mode==='release-throws')throw new Error('synthetic release error');}};
        }};
      }};
    },
    body:{appendChild(script){scripts.push(script);}}
  };
  const window={location:{reload(){reloads++;}},
    addEventListener(name,fn){listeners.set(name,fn);},
    removeEventListener(name,fn){if(listeners.get(name)===fn)listeners.delete(name);}
  };
  nodes.get('monee').attributes.inert='';
  vm.runInNewContext(source,{document,window,WebAssembly:mode==='no-wasm'?undefined:{},
    setTimeout(fn){const id=++timerId;timers.set(id,fn);return id;},
    clearTimeout(id){timers.delete(id);}});
  return {nodes,listeners,scripts,window,timers,
    get probes(){return probes;},get releases(){return releases;},get reloads(){return reloads;},get focus(){return focus;}};
}

test('null, thrown, lost and WebGL1-only contexts block Compose loading with native feedback',()=>{
  for(const mode of ['null','throws','lost','webgl1']) {
    const f=fixture(mode);
    assert.equal(f.probes,1);assert.equal(f.scripts.length,0);
    assert.equal(f.nodes.get('startup-title').textContent,'无法启动图形界面');
    assert.equal(f.nodes.get('startup').hidden,false);
    assert.equal(f.nodes.get('startup').attributes.role,'alert');
    assert.equal(f.nodes.get('startup-retry').hidden,false);assert.equal(f.focus,1);
    f.nodes.get('startup-retry').events.click();assert.equal(f.reloads,1);
  }
});
test('unavailable WebAssembly fails before allocating a graphics context',()=>{
  const f=fixture('no-wasm');assert.equal(f.probes,0);assert.equal(f.scripts.length,0);
  assert.equal(f.nodes.get('startup-title').textContent,'浏览器不兼容');
});
test('successful probe releases once and only loads the real application once',()=>{
  const f=fixture();assert.equal(f.probes,1);assert.equal(f.releases,1);
  assert.equal(f.scripts.length,1);assert.equal(f.scripts[0].src,'monee.js');
  assert.equal(f.nodes.get('startup').hidden,false);
  f.window.moneeStartupReady();assert.equal(f.nodes.get('startup').hidden,true);
  assert.equal(f.listeners.size,0);assert.equal(f.timers.size,0);
  f.window.moneeStartupReady();assert.equal(f.probes,1);assert.equal(f.scripts.length,1);
});
test('optional or throwing context release extension does not block a supported browser',()=>{
  for(const mode of ['no-release','release-throws']){
    const f=fixture(mode);assert.equal(f.probes,1);assert.equal(f.scripts.length,1);
  }
});
test('startup failure keeps underlying app inert and ready removes the interaction guard',()=>{
  const failure=fixture('null');assert.equal(failure.nodes.get('monee').attributes.inert,'');
  const f=fixture();const oldError=f.listeners.get('error');
  assert.equal(f.nodes.get('monee').attributes.inert,'');
  f.window.moneeStartupReady();assert.equal(f.nodes.get('monee').attributes.inert,undefined);
  oldError();assert.equal(f.nodes.get('startup').hidden,true);
});
test('script load and asynchronous startup errors use generic failure without swallowing events',()=>{
  for(const name of ['script','error','unhandledrejection']){
    const f=fixture();
    if(name==='script')f.scripts[0].onerror();else f.listeners.get(name)();
    assert.equal(f.nodes.get('startup-title').textContent,'界面启动失败');
    assert.equal(f.nodes.get('startup').hidden,false);
    assert.equal(f.nodes.get('startup').attributes['aria-busy'],'false');
    f.window.moneeStartupReady();assert.equal(f.nodes.get('startup').hidden,false);
    assert.equal(f.focus,1);
  }
});
test('slow loading is not mislabeled graphics failure and can still complete',()=>{
  const f=fixture();for(const fn of f.timers.values())fn();
  assert.match(f.nodes.get('startup-message').textContent,/启动时间较长/);
  assert.equal(f.nodes.get('startup-retry').hidden,false);
  f.window.moneeStartupReady();assert.equal(f.nodes.get('startup').hidden,true);
});
