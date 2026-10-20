/* 访问前缀：直连控制台端口时为空串，经飞牛统一网关（/app/dog-dev/）访问时为该前缀。
 *
 * 飞牛桌面图标走统一网关，控制台端口被用户改掉也不影响入口；
 * 同一份前端要同时适配两种访问方式，站内绝对地址一律经 appURL() 拼接。
 * 路径形态在页面生命周期内不变（控制台是单页、无前端路由），模块加载时算一次即可。
 */
const matched = typeof location !== 'undefined' ? location.pathname.match(/^\/app\/[^/]+/) : null

export const BASE = matched ? matched[0] : ''

/* 把站内绝对路径（/api/... 、/logo-solid.svg 、/ca）转成当前访问方式下的地址。 */
export function appURL(path) {
  if (!path || !path.startsWith('/')) return path
  return BASE + path
}
