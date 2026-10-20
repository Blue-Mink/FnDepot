import { createApp, reactive, computed } from 'vue'
import App from './App.vue'
import './style.css'

/* 主题：light / dark 两态（1.3.0 去掉「跟随系统」）。
 * index.html 的首帧脚本已把 data-theme 写到 <html> 上（localStorage 持久化），
 * 这里只负责回读、响应式同步与切换。
 */
const root = document.documentElement
const allowed = ['light', 'dark']

function initialMode() {
  try {
    const t = localStorage.getItem('theme')
    if (allowed.includes(t)) return t
  } catch { /* ignore */ }
  // 首次访问：按系统偏好定一次初值，之后不再跟随系统变化。
  const dark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
  return dark ? 'dark' : 'light'
}

const theme = reactive({ mode: initialMode() })
const isDark = computed(() => theme.mode === 'dark')

function applyAttr() { root.setAttribute('data-theme', theme.mode) }

export function setTheme(mode) {
  if (!allowed.includes(mode)) mode = 'light'
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
