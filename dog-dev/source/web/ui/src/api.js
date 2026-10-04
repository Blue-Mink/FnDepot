/* /api/* 请求封装 — 与原 app.js 契约完全一致：
 * - Bearer token（localStorage 键 ghpp_token）
 * - 默认 JSON，body 对象自动序列化
 * - 401 → 触发 onUnauthorized（由 App.vue 接回登录页）
 * - 响应若带 data.data 则自动解包（后端统一包裹格式）
 */
const TOKEN_KEY = 'ghpp_token'
let onUnauthorized = null

export function getToken() {
  try { return localStorage.getItem(TOKEN_KEY) || '' } catch { return '' }
}

export function setToken(t) {
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  } catch { /* ignore */ }
}

export function setOnUnauthorized(fn) { onUnauthorized = fn }

export async function api(path, options) {
  const opt = Object.assign({ method: 'GET' }, options || {})
  opt.headers = Object.assign({ 'Content-Type': 'application/json' }, opt.headers || {})
  const token = getToken()
  if (token) opt.headers.Authorization = 'Bearer ' + token
  if (opt.body && typeof opt.body !== 'string') opt.body = JSON.stringify(opt.body)

  let resp
  try {
    resp = await fetch(path, opt)
  } catch {
    throw new Error('无法连接服务，请确认程序正在运行')
  }

  if (resp.status === 401) {
    if (onUnauthorized) onUnauthorized()
    throw new Error('登录已过期，请重新登录')
  }

  const text = await resp.text()
  let data = null
  if (text) {
    try { data = JSON.parse(text) } catch { throw new Error('服务返回了无法解析的内容') }
  }
  if (!resp.ok) {
    throw new Error((data && data.error) || ('请求失败：HTTP ' + resp.status))
  }
  return data && data.data !== undefined ? data.data : data
}

/* 日志 SSE 需要 query 传 token（EventSource 不能带 header） */
export function logsStreamUrl() {
  return '/api/logs/stream?token=' + encodeURIComponent(getToken())
}

/* 常用格式化（与原实现一致） */
export function fmtBytes(n) {
  n = Number(n) || 0
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

export function fmtMs(n) {
  n = Number(n)
  if (!isFinite(n) || n < 0) return '—'
  return n < 1000 ? Math.round(n) + ' ms' : (n / 1000).toFixed(2) + ' s'
}

export function fmtLatency(ms) {
  const v = Number(ms) || 0
  if (v <= 0) return '—'
  return Math.round(v) + ' ms'
}

export function fmtSpeed(kbps) {
  const v = Number(kbps) || 0
  if (v <= 0) return '—'
  if (v < 1024) return Math.round(v) + ' KB/s'
  return (v / 1024).toFixed(1) + ' MB/s'
}

export function fmtDuration(seconds) {
  const s = Math.round(Number(seconds) || 0)
  if (s < 60) return s + ' 秒'
  if (s < 3600) return Math.floor(s / 60) + ' 分 ' + (s % 60) + ' 秒'
  return Math.floor(s / 3600) + ' 时 ' + Math.floor((s % 3600) / 60) + ' 分'
}

export function fmtTime(iso) {
  if (!iso) return '暂无'
  const d = new Date(iso)
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '暂无'
  const pad = (n) => String(n).padStart(2, '0')
  return pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' +
    pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds())
}

export function parseDuration(str) {
  if (!str) return 0
  const m = String(str).match(/(?:(\d+)h)?(?:(\d+)m)?([\d.]+)s/)
  if (!m) return 0
  return (Number(m[1]) || 0) * 3600 + (Number(m[2]) || 0) * 60 + Number(m[3] || 0)
}
