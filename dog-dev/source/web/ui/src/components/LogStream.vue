<script setup>
/* 日志视图：初始拉 /api/logs?limit=120 + SSE 追加，级别过滤、实时跟随、清屏，最多保留 800 行。
 * v2 修复：旧版 @scroll.passive="maybeScroll" 把 scroll 事件对象当 force 参数传入（恒真），
 * 导致每次滚动都被强制拉回底部 → 无法上下滚动。现拆出 onScroll()（不带 force）+ 独立回底按钮。 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, logsStreamUrl } from '../api'

const MAX_LINES = 800
const props = defineProps({
  level: { type: String, default: '' },
  follow: { type: Boolean, default: true },
})
const entries = ref([])
const viewEl = ref(null)
const nearBottom = ref(true)
let src = null

function pad(n) { return String(n).padStart(2, '0') }
function ts(iso) {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '--:--:--'
  return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds())
}

function push(entry) {
  const list = entries.value
  list.push(entry)
  if (list.length > MAX_LINES) list.splice(0, list.length - MAX_LINES)
}

const lines = computed(() =>
  entries.value
    .filter((e) => !props.level || (e.level || 'info').toLowerCase() === props.level)
    .map((e) => ({
      key: e.seq + '-' + e.time,
      t: ts(e.time),
      l: (e.level || 'info').toLowerCase(),
      m: e.message,
    })))

/* 仅在用户已停在底部附近时才自动贴底，翻历史不被打断 */
function maybeScroll(force) {
  const el = viewEl.value
  if (!el) return
  if (force || (props.follow && nearBottom.value)) el.scrollTop = el.scrollHeight
}
/* 滚动事件处理：只更新"是否在底部"状态，绝不强制回底（v2 修复点） */
function onScroll() {
  const el = viewEl.value
  if (!el) return
  nearBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < 60
}
function jump() { nearBottom.value = true; maybeScroll(true) }

watch(entries, () => nextTick(() => maybeScroll()))
watch(() => props.follow, (v) => { if (v) nextTick(() => maybeScroll(true)) })

function clear() { entries.value = [] }
defineExpose({ clear })

onMounted(async () => {
  try {
    const data = await api('/api/logs?limit=120')
    ;(data || []).forEach(push)
  } catch { /* 401 由 App 处理 */ }
  src = new EventSource(logsStreamUrl())
  src.onmessage = (ev) => {
    try { push(JSON.parse(ev.data)) } catch { /* 忽略心跳/格式异常 */ }
  }
  // EventSource 断线自动重连，不处理 onerror 避免刷屏。
  nextTick(() => maybeScroll(true))
})
onBeforeUnmount(() => { if (src) src.close() })
</script>

<template>
  <div class="log-wrap">
    <div ref="viewEl" class="log-view" @scroll.passive="onScroll">
      <p v-if="!lines.length" class="log-empty">暂无日志</p>
      <div v-for="x in lines" :key="x.key" class="log-line" :class="x.l">
        <span class="t">{{ x.t }}</span>
        <span class="l">{{ x.l }}</span>
        <span class="m">{{ x.m }}</span>
      </div>
    </div>
    <button v-show="!nearBottom" class="log-jump" title="回到底部" @click="jump">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 10l6 6 6-6"/></svg>
    </button>
  </div>
</template>

<style scoped>
.log-wrap { position: relative; }
.log-view {
  height: min(520px, calc(100dvh - 240px)); min-height: 300px;
  overflow: auto; overscroll-behavior: contain;
  -webkit-overflow-scrolling: touch;
  padding: 10px 12px;
  background: var(--code-bg); border-radius: var(--r-sm);
  font: 12px/1.7 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.log-empty { color: #8e8e93; text-align: center; margin: 20px 0; }
.log-line { display: flex; gap: 8px; white-space: pre-wrap; word-break: break-all; }
.log-line .t { color: #6e6e73; flex: none; }
.log-line .l { flex: none; width: 44px; font-weight: 700; text-transform: uppercase; }
.log-line .m { color: #e8e8ed; }
.log-line.info .l { color: #64d2ff; }
.log-line.warn .l { color: #ffd60a; }
.log-line.warn .m { color: #ffe9a8; }
.log-line.error .l { color: #ff453a; }
.log-line.error .m { color: #ffb4ae; }
.log-line.debug .l { color: #8e8e93; }
.log-line.debug .m { color: #b9b9be; }
.log-jump {
  position: absolute; right: 10px; bottom: 10px;
  width: 34px; height: 34px; border-radius: 50%;
  border: 0; background: rgba(60, 60, 67, 0.8); color: #fff;
  display: flex; align-items: center; justify-content: center;
  cursor: pointer; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
}
.log-jump svg { width: 17px; height: 17px; }
</style>
