<script setup>
/* 根组件：登录门 + 顶栏（品牌 / 状态点 / 主题亮面按钮 / 退出）+ 8 个 view 切换 + 底部 Dock */
import { computed, inject, provide, reactive, ref, watch } from 'vue'
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme } from 'naive-ui'
import { api, getToken, setToken, setOnUnauthorized } from './api'
import TabBar from './components/TabBar.vue'
import OverviewView from './views/OverviewView.vue'
import MirrorsView from './views/MirrorsView.vue'
import HostsView from './views/HostsView.vue'
import DockerView from './views/DockerView.vue'
import AppsView from './views/AppsView.vue'
import LogsView from './views/LogsView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'

const isDark = inject('isDark')
const theme = inject('theme')
const setTheme = inject('setTheme')

const VIEWS = {
  overview: OverviewView, mirrors: MirrorsView, hosts: HostsView, docker: DockerView,
  apps: AppsView, logs: LogsView, settings: SettingsView, about: AboutView,
}
const tab = ref('overview')
const activeView = computed(() => VIEWS[tab.value])
provide('goTab', (key) => { if (VIEWS[key]) tab.value = key })
provide('logout', doLogout)

/* ---- 登录门 ---- */
const authed = ref(!!getToken())
const login = reactive({ username: '', password: '' })
const loginErr = ref('')
const busy = ref(false)

async function doLogin() {
  if (busy.value) return
  busy.value = true
  loginErr.value = ''
  try {
    const data = await api('/api/login', { method: 'POST', body: { ...login } })
    setToken(data.token)
    authed.value = true
  } catch (e) {
    loginErr.value = e.message
  } finally {
    busy.value = false
  }
}

function doLogout() {
  api('/api/logout', { method: 'POST' }).catch(() => {})
  setToken('')
  authed.value = false
  tab.value = 'overview'
}
setOnUnauthorized(doLogout)

/* ---- 运行状态（顶栏小圆点） ---- */
const run = reactive({ ok: false, label: '加载中' })
async function checkRun() {
  try {
    await api('/api/status')
    run.ok = true
    run.label = '运行中'
  } catch (e) {
    if (String(e.message).includes('登录')) return
    run.ok = false
    run.label = '服务异常'
  }
}
watch(authed, (a) => { if (a) checkRun() }, { immediate: true })

/* ---- 主题：亮面圆形按钮，循环 自动 → 浅色 → 深色 ---- */
const THEME_LABEL = { auto: '跟随系统', light: '浅色', dark: '深色' }
const themeLabel = computed(() => THEME_LABEL[theme.mode] || '跟随系统')
function cycleTheme() {
  const next = theme.mode === 'auto' ? 'light' : theme.mode === 'light' ? 'dark' : 'auto'
  setTheme(next)
}

/* 顶部通知配色：换成系统柔和 token（毛玻璃/圆角等版式在 style.css 覆盖），
 * 明暗主题下自动跟随 CSS 变量翻转。 */
const themeOverrides = {
  message: {
    colorMap: {
      info:    { color: 'var(--bar-bg)', textColor: 'var(--fg)' },
      default: { color: 'var(--bar-bg)', textColor: 'var(--fg)' },
      success: { color: 'var(--green-soft)', textColor: 'var(--fg)' },
      warning: { color: 'var(--orange-soft)', textColor: 'var(--fg)' },
      error:   { color: 'var(--red-soft)', textColor: 'var(--fg)' },
    },
  },
}
</script>

<template>
  <n-config-provider :theme="isDark ? darkTheme : null" :theme-overrides="themeOverrides">
    <n-message-provider>
      <n-dialog-provider>

        <div v-if="!authed" class="login-wrap">
          <div class="login-glow" aria-hidden="true"></div>
          <form class="login-card" @submit.prevent="doLogin">
            <img :src="isDark ? '/logo-white.png' : '/logo-solid.svg'" alt="Dog-dev" class="login-logo">
            <h1 class="login-title">Dog-dev</h1>
            <p class="login-sub">飞牛 NAS 加速器</p>
            <label class="field"><span>账号</span>
              <input v-model.trim="login.username" autocomplete="username" placeholder="admin">
            </label>
            <label class="field"><span>密码</span>
              <input v-model="login.password" type="password" autocomplete="current-password" placeholder="初始密码 admin123">
            </label>
            <p v-if="loginErr" class="notice err">{{ loginErr }}</p>
            <button type="submit" class="btn primary full" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
            <p class="login-hint">默认账号 admin · 初始密码 admin123 · 登录后请修改</p>
          </form>
        </div>

        <template v-else>
          <header class="topbar">
            <div class="topbar-brand">
              <!-- 品牌规范：≤64px 用实色版。30px 下反白版负形（狗身/水线）糊成白团泛白不可辨，深色主题同样用实色 -->
              <img src="/logo-solid.svg" alt="Dog-dev">
              <strong>Dog-dev</strong>
            </div>
            <div class="topbar-right">
              <span class="run-dot" :class="{ on: run.ok }" :title="run.label"></span>
              <button class="btn icon" :title="'主题：' + themeLabel" :aria-label="themeLabel" @click="cycleTheme">
                <svg v-if="theme.mode === 'light'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
                  <circle cx="12" cy="12" r="4.4"/>
                  <path d="M12 2.8v2.4M12 18.8v2.4M2.8 12h2.4M18.8 12h2.4M5.2 5.2l1.7 1.7M17.1 17.1l1.7 1.7M18.8 5.2l-1.7 1.7M6.9 17.1l-1.7 1.7"/>
                </svg>
                <svg v-else-if="theme.mode === 'dark'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round">
                  <path d="M20 13.6A8.4 8.4 0 0 1 10.4 4a8.4 8.4 0 1 0 9.6 9.6Z"/>
                </svg>
                <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
                  <circle cx="12" cy="12" r="8.4"/>
                  <path d="M12 3.6a8.4 8.4 0 0 1 0 16.8Z" fill="currentColor" stroke="none"/>
                </svg>
              </button>
              <button class="btn icon" title="退出登录" aria-label="退出登录" @click="doLogout">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 4h4a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-4M10 8l-4 4 4 4M6 12h9"/>
                </svg>
              </button>
            </div>
          </header>

          <main class="main">
            <component :is="activeView" :key="tab" />
          </main>

          <TabBar :tab="tab" @update:tab="tab = $event" />
        </template>

      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.topbar {
  position: sticky; top: 0; z-index: 40;
  display: flex; align-items: center; justify-content: space-between;
  height: 54px; padding: 0 16px;
  background: var(--bar-bg);
  backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px);
  border-bottom: 0.5px solid var(--hairline);
}
.topbar-brand { display: flex; align-items: center; gap: 10px; }
/* 品牌标是自由轮廓（圆盘+破形水线），border-radius 会裁掉水线端头，规范要求不裁切 */
.topbar-brand img { width: 30px; height: 30px; }
.topbar-brand strong { font-size: 15px; letter-spacing: -0.1px; }
.topbar-right { display: flex; align-items: center; gap: 8px; }
.run-dot { width: 9px; height: 9px; border-radius: 50%; background: var(--fg3); flex: none; }
.run-dot.on { background: var(--green); }
.main {
  padding: 16px 14px calc(var(--tabbar-h) + 24px + env(safe-area-inset-bottom));
  max-width: 860px; margin: 0 auto;
  display: flex; flex-direction: column; gap: var(--gap);
}

/* 登录页：与整体同一套材质——顶栏同款毛玻璃卡（bar-bg + blur + hairline + dock 阴影），
 * 背后两团品牌色柔光把纯色背景点亮；字段/按钮沿用全站 .field / .btn。 */
.login-wrap {
  position: relative; min-height: 100dvh; overflow: hidden;
  display: flex; align-items: center; justify-content: center; padding: 24px;
}
.login-glow {
  position: absolute; inset: 0; pointer-events: none;
  background:
    radial-gradient(620px 340px at 50% -80px, var(--accent-soft), transparent 70%),
    radial-gradient(520px 320px at 88% 112%, var(--green-soft), transparent 70%);
}
.login-card {
  position: relative; width: 100%; max-width: 340px;
  display: flex; flex-direction: column; gap: 12px;
  background: var(--bar-bg);
  -webkit-backdrop-filter: blur(24px) saturate(1.5);
  backdrop-filter: blur(24px) saturate(1.5);
  border: 0.5px solid var(--hairline);
  border-radius: var(--r);
  box-shadow: var(--shadow-dock);
  padding: 30px 24px 22px;
}
.login-logo { width: 60px; height: 60px; align-self: center; }
.login-title { margin: 0; font-size: 22px; text-align: center; letter-spacing: -0.2px; }
.login-sub { margin: -8px 0 4px; font-size: 12px; color: var(--fg2); text-align: center; }
.login-hint { margin: 0; font-size: 11px; color: var(--fg3); text-align: center; }
</style>
