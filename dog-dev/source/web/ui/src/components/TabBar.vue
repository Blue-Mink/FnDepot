<script setup>
/* 底部悬浮 Dock：四周圆角胶囊，毛玻璃。8 个 tab 图标沿用原 SVG。 */
import { useKeyboardInset } from '../useKeyboard'

defineProps({ tab: { type: String, required: true } })
const emit = defineEmits(['update:tab'])

/* 软键盘呼出时 Dock 随键盘上移（否则压在输入框上）；无键盘时 kb=0 保持原位。 */
const kb = useKeyboardInset()
const dockStyle = () => (kb.value
  ? { bottom: `calc(10px + env(safe-area-inset-bottom) + ${kb.value}px)` }
  : {})

const TABS = [
  { key: 'overview', label: '总览', fill: 'currentColor', d: '<path d="M3.5 10.5 12 3.8l8.5 6.7v9.2a1 1 0 0 1-1 1h-5v-6h-5v6h-5a1 1 0 0 1-1-1v-9.2Z"/>' },
  { key: 'mirrors', label: '加速源', fill: 'currentColor', d: '<path d="M13.2 2.6 5.4 12.5h4.9l-1.5 8.9 7.8-9.9h-4.9l1.5-8.9Z"/>' },
  { key: 'hosts', label: 'DNS', fill: 'none', d: '<circle cx="12" cy="12" r="8.6"/><path d="M3.4 12h17.2M12 3.4c2.6 2.3 3.9 5.2 3.9 8.6s-1.3 6.3-3.9 8.6c-2.6-2.3-3.9-5.2-3.9-8.6s1.3-6.3 3.9-8.6Z"/>' },
  { key: 'docker', label: 'Docker', fill: 'none', d: '<path d="M12 3.2 4.2 7.4v9.2l7.8 4.2 7.8-4.2V7.4L12 3.2Z"/><path d="M4.4 7.6 12 11.8l7.6-4.2M12 11.8v8.6"/>' },
  { key: 'apps', label: '应用', fill: 'none', d: '<rect x="4" y="4" width="7" height="7" rx="2"/><rect x="13" y="4" width="7" height="7" rx="2"/><rect x="4" y="13" width="7" height="7" rx="2"/><rect x="13" y="13" width="7" height="7" rx="2"/>' },
  { key: 'logs', label: '日志', fill: 'none', d: '<path d="M5 6.5h14M5 12h14M5 17.5h9"/>' },
  { key: 'settings', label: '设置', fill: 'none', d: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/>' },
  { key: 'about', label: '关于', fill: 'none', d: '<circle cx="12" cy="12" r="8.6"/><path d="M12 11v5.4" stroke-linecap="round"/><circle cx="12" cy="7.8" r="1.15" fill="currentColor" stroke="none"/>' },
]
</script>

<template>
  <nav class="dock" role="tablist" :style="dockStyle()">
    <button
      v-for="t in TABS"
      :key="t.key"
      class="tbtn"
      :class="{ on: tab === t.key }"
      role="tab"
      :aria-selected="tab === t.key"
      @click="emit('update:tab', t.key)"
    >
      <svg viewBox="0 0 24 24" :fill="t.fill" stroke="currentColor" stroke-width="1.7" v-html="t.d"></svg>
      <span>{{ t.label }}</span>
    </button>
  </nav>
</template>

<style scoped>
.dock {
  position: fixed; left: 50%; transform: translateX(-50%);
  bottom: calc(10px + env(safe-area-inset-bottom));
  z-index: 50;
  display: grid; grid-auto-flow: column; gap: 1px;
  padding: 5px;
  background: var(--bar-bg);
  backdrop-filter: blur(24px) saturate(1.8); -webkit-backdrop-filter: blur(24px) saturate(1.8);
  border: 0.5px solid var(--hairline);
  border-radius: 24px;
  box-shadow: var(--shadow-dock);
}
.tbtn {
  width: 41px;
  display: flex; flex-direction: column; align-items: center; justify-content: flex-end;
  gap: 2px; padding: 5px 0 4px; border: 0; background: none;
  color: var(--fg3); font-size: 9px; cursor: pointer; border-radius: 16px;
}
.tbtn svg { width: 21px; height: 21px; }
.tbtn.on { color: var(--accent); }
@media (max-width: 374px) {
  .dock { border-radius: 22px; padding: 4px; }
  .tbtn { width: 38px; }
}
</style>
