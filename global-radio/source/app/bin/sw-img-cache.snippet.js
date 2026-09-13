/* == gr-img-cache patch v1 (global-radio FPK 1.2.6) ==
 * 跨域电台封面图本地缓存：SWR 策略，先出缓存、后台刷新；
 * 网络失败回退旧缓存（复活裂图）。上限 2000 条 FIFO 淘汰，
 * 非 opaque 条目按 30 天过期清理。仅拦截 GET 图片请求。
 */
(function(){
'use strict';
var CN='gr-img-cache-v1', MAX_AGE_MS=30*24*3600*1000, MAX_ENT=2000;
self.addEventListener('fetch',function(ev){
  var req=ev.request;
  if(req.method!=='GET')return;
  var u;try{u=new URL(req.url);}catch(e){return;}
  if(u.origin===self.location.origin)return;
  var isImg=req.destination==='image'||/\.(png|jpe?g|webp|gif|avif|svg|ico)(\?|#|$)/i.test(u.pathname);
  if(!isImg)return;
  ev.respondWith((async function(){
    var c=await caches.open(CN);
    var hit=await c.match(req);
    var net=fetch(req).then(async function(r){
      if(r&&(r.ok||r.status===0)){
        try{
          if(r.type==='opaque'){await c.put(req,r.clone());}
          else{
            var body=await r.blob();
            var h=new Headers(r.headers);
            h.set('x-gr-cache-at',String(Date.now()));
            await c.put(req,new Response(body,{status:r.status,statusText:r.statusText,headers:h}));
          }
          prune(c);
        }catch(e){}
      }
      return r;
    }).catch(function(){return null;});
    if(hit){net.catch(function(){});return hit;}
    var fresh=await net;
    if(fresh)return fresh;
    var stale=await c.match(req,{ignoreVary:true});
    return stale||Response.error();
  })());
});
async function prune(c){
  try{
    var ks=await c.keys();
    if(ks.length>MAX_ENT){for(var i=0;i<ks.length-MAX_ENT;i++)await c.delete(ks[i]);ks=await c.keys();}
    var now=Date.now();
    for(var j=ks.length-1;j>=0;j--){
      var e=await c.match(ks[j]);if(!e)continue;
      var t=parseInt(e.headers.get('x-gr-cache-at')||'0',10);
      if(t&&now-t>MAX_AGE_MS)await c.delete(ks[j]);
    }
  }catch(e){}
}
})();
