import { createApp, reactive, computed } from 'vue'
import App from './App.vue'
import './style.css'

/* 主题：auto / light / dark。
 * index.html 的首帧脚本已把 data-theme 写到 <html> 上（localStorage 持久化），
 * 这里只负责：回读 → 响应式同步 → 系统偏好变化时刷新（auto 模式跟随系统）。
 * 页面级 CSS token 由 style.css 消费 data-theme；naive-ui 走 isDark 切换主题。
 */
const root = document.documentElement
const allowed = ['auto', 'light', 'dark']

const theme = reactive({
  mode: (() => {
    let t = 'auto'
    try { t = localStorage.getItem('theme') || 'auto' } catch { /* ignore */ }
    return allowed.includes(t) ? t : 'auto'
  })(),
})

const mq = window.matchMedia('(prefers-color-scheme: dark)')
const system = reactive({ dark: mq.matches })
mq.addEventListener('change', (e) => { system.dark = e.matches })

const isDark = computed(
  () => theme.mode === 'dark' || (theme.mode === 'auto' && system.dark),
)

function applyAttr() { root.setAttribute('data-theme', theme.mode) }

export function setTheme(mode) {
  if (!allowed.includes(mode)) mode = 'auto'
  theme.mode = mode
  try { localStorage.setItem('theme', mode) } catch { /* ignore */ }
  applyAttr()
}

applyAttr()

const app = createApp(App)
app.provide('theme', theme)
app.provide('isDark', isDark)
app.provide('setTheme', setTheme)
app.mount('#app')
