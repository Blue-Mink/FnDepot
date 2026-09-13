/* global-radio 加速器包装层 v2（FPK 1.2.7）
 * 把前端直连国外的流量改道走 NAS 缓存代理（http 入口也生效，不依赖 SW）：
 *  - radio-browser.info API  → /rb/        （NAS 缓存 10min，免六镜像探测）
 *  - 跨域封面图 <img>        → /imgproxy/… （NAS 磁盘缓存 30d + 浏览器缓存 30d）
 * 直播流保持客户端直连。兜底清扫采用 300ms 防抖批量处理，避免同步回写风暴。
 */
(function(){
'use strict';
var API_RE=/^https?:\/\/(?:[a-z0-9-]+\.)*api\.radio-browser\.info\/(?:json|xml)\//i;
function rb(u){return u.replace(/^https?:\/\/[^/]+\//,'/rb/');}
function img(u){
  if(typeof u!=='string'||!u)return u;
  if(u.charAt(0)==='/')return u;
  if(!/^https?:\/\//i.test(u))return u;
  try{ if(new URL(u).origin===location.origin)return u; }catch(e){}
  return '/imgproxy/'+encodeURIComponent(u);
}
/* API: fetch */
if(window.fetch){
  var _f=window.fetch;
  window.fetch=function(a,b){
    try{
      var u=(typeof a==='string')?a:(a&&a.url)||'';
      if(API_RE.test(u))return _f.call(this,rb(u),b);
    }catch(e){}
    return _f.call(this,a,b);
  };
}
/* API: XHR（axios 默认通道） */
if(window.XMLHttpRequest){
  var _o=XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open=function(m,u){
    var a=Array.prototype.slice.call(arguments);
    try{ if(typeof u==='string'&&API_RE.test(u))a[1]=rb(u); }catch(e){}
    return _o.apply(this,a);
  };
}
/* 图片：src 属性 setter */
try{
  var P=HTMLImageElement.prototype,d=Object.getOwnPropertyDescriptor(P,'src');
  if(d&&d.set)Object.defineProperty(P,'src',{
    get:d.get,
    set:function(v){return d.set.call(this,img(v));},
    enumerable:d.enumerable,configurable:true
  });
}catch(e){}
/* 图片：setAttribute */
try{
  var _sa=Element.prototype.setAttribute;
  Element.prototype.setAttribute=function(n,v){
    try{
      if(window.HTMLImageElement&&this instanceof HTMLImageElement
         &&String(n).toLowerCase()==='src')v=img(v);
    }catch(e){}
    return _sa.call(this,n,v);
  };
}catch(e){}
/* innerHTML 等旁路写入的兜底：300ms 防抖批量清扫，只重写真实 http(s) 的 src */
var timer=0;
function sweep(){
  timer=0;
  try{
    var list=document.getElementsByTagName('img');
    for(var i=0;i<list.length;i++){
      var el=list[i];
      var s=el.getAttribute&&el.getAttribute('src');
      if(s&&/^https?:\/\//i.test(s))el.setAttribute('src',img(s));
    }
  }catch(e){}
}
function schedule(){ if(!timer)timer=setTimeout(sweep,300); }
var mo=new MutationObserver(schedule);
function boot(){
  mo.observe(document.documentElement,
    {subtree:true,childList:true,attributes:true,attributeFilter:['src']});
}
if(document.documentElement)boot();
else document.addEventListener('DOMContentLoaded',boot);
})();
