<script setup>
/* 根组件：顶栏（品牌 / 状态点 / 主题按钮）+ 8 个 view 切换 + 底部 Dock。
 * 1.3.0：去掉登录门与右上角退出按钮（控制台默认免登录），主题只保留浅色/深色。 */
import { computed, inject, provide, reactive, ref } from 'vue'
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme } from 'naive-ui'
import { api } from './api'
import { appURL } from './base'
import TabBar from './components/TabBar.vue'
import OverviewView from './views/OverviewView.vue'
import MirrorsView from './views/MirrorsView.vue'
import HostsView from './views/HostsView.vue'
import DockerView from './views/DockerView.vue'
import AppsView from './views/AppsView.vue'
import LogsView from './views/LogsView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'

// 顶栏品牌标：带统一网关前缀（直连端口时前缀为空）。
const logoURL = appURL('/logo-solid.svg')

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

/* ---- 运行状态（顶栏小圆点） ---- */
const run = reactive({ ok: false, label: '加载中' })
async function checkRun() {
  try {
    await api('/api/status')
    run.ok = true
    run.label = '运行中'
  } catch (e) {
    run.ok = false
    run.label = '服务异常'
  }
}
checkRun()

/* ---- 主题：亮面圆形按钮，浅色 ↔ 深色 ---- */
const THEME_LABEL = { light: '浅色', dark: '深色' }
const themeLabel = computed(() => THEME_LABEL[theme.mode] || '浅色')
function cycleTheme() {
  setTheme(theme.mode === 'light' ? 'dark' : 'light')
}

/* 顶部通知配色：系统柔和 token，明暗主题下自动跟随 CSS 变量翻转。 */
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

        <header class="topbar">
          <div class="topbar-brand">
            <!-- 品牌规范：≤64px 用实色版 -->
            <img :src="logoURL" alt="Dog-dev">
            <strong>Dog-dev</strong>
          </div>
          <div class="topbar-right">
            <span class="run-dot" :class="{ on: run.ok }" :title="run.label"></span>
            <button class="btn icon" :title="'主题：' + themeLabel" :aria-label="themeLabel" @click="cycleTheme">
              <svg v-if="theme.mode === 'light'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
                <circle cx="12" cy="12" r="4.4"/>
                <path d="M12 2.8v2.4M12 18.8v2.4M2.8 12h2.4M18.8 12h2.4M5.2 5.2l1.7 1.7M17.1 17.1l1.7 1.7M18.8 5.2l-1.7 1.7M6.9 17.1l-1.7 1.7"/>
              </svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round">
                <path d="M20 13.6A8.4 8.4 0 0 1 10.4 4a8.4 8.4 0 1 0 9.6 9.6Z"/>
              </svg>
            </button>
          </div>
        </header>

        <main class="main">
          <component :is="activeView" :key="tab" />
        </main>

        <TabBar :tab="tab" @update:tab="tab = $event" />

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
</style>
