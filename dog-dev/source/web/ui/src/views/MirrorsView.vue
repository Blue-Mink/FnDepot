<script setup>
/* 加速源 v2：iOS 列表行（点入 Sheet 编辑，含测速/删除/启停）+ 默认前 8 条 + 展开全部。
 * 修复旧版表格 9 列 td 对 8 列 th 的错位（名称列混入重复 checkbox）。 */
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useMessage, useDialog } from 'naive-ui'
import { api, fmtLatency, fmtSpeed, fmtTime } from '../api'
import Sheet from '../components/Sheet.vue'

const message = useMessage()
const dialog = useDialog()

const KIND_NAMES = { prefix: '前缀中转', raw: 'Raw CDN', git: '仓库镜像', direct: '官方直连', pathfetch: '路径抓取' }
const KIND_OPTIONS = [
  ['prefix', '前缀中转（通用）'], ['raw', 'Raw 文件 CDN'], ['git', 'Git 仓库镜像'], ['direct', '官方直连'], ['pathfetch', '路径抓取（xget 型）'],
]
const PREVIEW_N = 8

const mirrors = ref([])
const status = ref({})
const testing = ref({})
const allTesting = ref(false)
const allOpen = ref(false)

async function load() {
  try { mirrors.value = (await api('/api/mirrors')) || [] } catch { /* 401 由 App 处理 */ }
}
async function loadStatus() {
  try { status.value = (await api('/api/status')) || {} } catch { /* ignore */ }
}
onMounted(() => {
  load()
  loadStatus()
  const t = setInterval(() => { load(); loadStatus() }, 30000)
  onUnmounted(() => clearInterval(t))
})

const visible = computed(() => (allOpen.value ? mirrors.value : mirrors.value.slice(0, PREVIEW_N)))

const summary = computed(() =>
  mirrors.value.filter((m) => m.enabled).length + ' / ' + mirrors.value.length + ' 启用')

function badge(m) {
  const st = m.stat || {}
  const tested = st.at && new Date(st.at).getFullYear() > 2000
  if (!m.enabled) return { text: '已停用', cls: '' }
  if (!tested) return { text: '未测速', cls: '' }
  if (st.ok) return { text: st.in_cooldown ? '冷却中' : '正常', cls: st.in_cooldown ? 'warn' : 'ok', title: st.error || '' }
  return { text: '不可用', cls: 'err', title: st.error || '' }
}

function rankText(m) {
  if (!m.rank) return '—'
  return m.rank === 1 ? 'TOP1' : 'P' + m.rank
}

/* 行副标题：类型 · 延迟 · 吞吐 · 得分 */
function subline(m) {
  const st = m.stat || {}
  const score = Math.max(0, Math.min(100, Number(st.score) || 0))
  return [KIND_NAMES[m.kind] || m.kind,
    '延迟 ' + fmtLatency(st.latency_ms),
    '吞吐 ' + fmtSpeed(st.throughput_kbps),
    '得分 ' + Math.round(score)].join(' · ')
}

const monitorText = computed(() => {
  const s = (status.value && status.value.status) || {}
  if (!s.mirror_monitor_min) return ''
  const last = s.last_mirror_monitor && new Date(s.last_mirror_monitor).getFullYear() > 2000
    ? fmtTime(s.last_mirror_monitor) : '尚未'
  return '自动监测 ' + s.mirror_monitor_min + ' 分钟 · 最近 ' + last + ' · 按健康度自动排序'
})

async function toggle(m, enabled) {
  try {
    await api('/api/mirrors', { method: 'PUT', body: { id: m.id, enabled } })
    message.success((enabled ? '已启用 ' : '已停用 ') + m.name)
    await load()
  } catch (e) { message.error(e.message) }
}

async function testOne(m) {
  testing.value[m.id] = true
  try {
    const results = await api('/api/mirrors/test?id=' + encodeURIComponent(m.id), { method: 'POST' })
    const r = (results || [])[0]
    if (r && r.ok) message.success('延迟 ' + fmtLatency(r.latency_ms) + '，吞吐 ' + fmtSpeed(r.throughput_kbps))
    else message.error('测速失败：' + ((r && r.error) || '无响应'))
    await load()
  } catch (e) { message.error(e.message) }
  finally { testing.value[m.id] = false }
}

async function testAll() {
  allTesting.value = true
  try {
    await api('/api/mirrors/test-all', { method: 'POST' })
    message.success('全部测速完成')
    await load()
  } catch (e) { message.error(e.message) }
  finally { allTesting.value = false }
}

/* ---- Sheet 添加/编辑 ---- */
const show = ref(false)
const form = reactive({ id: '', name: '', url: '', kind: 'prefix', weight: 1, note: '', enabled: true })
const saving = ref(false)

function openAdd() {
  Object.assign(form, { id: '', name: '', url: '', kind: 'prefix', weight: 1, note: '', enabled: true })
  show.value = true
}
function openEdit(m) {
  Object.assign(form, { id: m.id, name: m.name, url: m.url, kind: m.kind, weight: m.weight || 1, note: m.note || '', enabled: !!m.enabled })
  show.value = true
}
const editing = computed(() => mirrors.value.find((m) => m.id === form.id) || null)

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    await api('/api/mirrors', {
      method: form.id ? 'PUT' : 'POST',
      body: {
        id: form.id, name: form.name.trim(), url: form.url.trim(),
        kind: form.kind, weight: Number(form.weight) || 1,
        note: form.note.trim(), enabled: !!form.enabled,
      },
    })
    show.value = false
    message.success(form.id ? '已保存' : '已添加')
    await load()
  } catch (e) { message.error(e.message) }
  finally { saving.value = false }
}

function del() {
  const m = editing.value
  if (!m) return
  dialog.warning({
    title: '删除加速源',
    content: '确定删除「' + m.name + '」？',
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api('/api/mirrors?id=' + encodeURIComponent(m.id), { method: 'DELETE' })
        show.value = false
        message.success('已删除')
        await load()
      } catch (e) { message.error(e.message) }
    },
  })
}
</script>

<template>
  <section class="card">
    <div class="card-head">
      <h2>加速源</h2>
      <div class="head-actions">
        <span class="badge" v-if="mirrors.length">{{ summary }}</span>
        <button class="btn sm" :disabled="allTesting" @click="testAll">{{ allTesting ? '测速中…' : '全部测速' }}</button>
        <button class="btn sm primary" @click="openAdd">添加</button>
      </div>
    </div>
    <div class="card-body">
      <p v-if="monitorText" class="monitor-line">
        <span class="pulse-dot"></span>{{ monitorText }}
      </p>
      <div class="mlist">
        <p v-if="!mirrors.length" class="empty">暂无加速源</p>
        <div
          v-for="m in visible" :key="m.id"
          class="lrow mrow" :class="{ dim: !m.enabled }"
          @click="openEdit(m)"
        >
          <span class="rank" :class="{ top: m.rank === 1 }" :title="m.rank ? '转发优先顺序第 ' + m.rank + ' 位' : '已停用'">{{ rankText(m) }}</span>
          <div class="lrow-main">
            <div class="lrow-t">
              <span class="name" :title="m.name">{{ m.name }}</span>
              <span class="badge" :class="badge(m).cls" :title="badge(m).title">{{ badge(m).text }}</span>
            </div>
            <div class="lrow-s" :title="m.url">{{ subline(m) }}</div>
          </div>
          <label class="sw" @click.stop>
            <input type="checkbox" :checked="!!m.enabled" @change="toggle(m, $event.target.checked)"><i></i>
          </label>
          <svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>
        </div>
      </div>
      <button v-if="mirrors.length > PREVIEW_N" class="btn sm ghost-line" @click="allOpen = !allOpen">
        {{ allOpen ? '收起' : '展开全部（' + mirrors.length + '）' }}
      </button>
    </div>
  </section>

  <Sheet :show="show" :title="form.id ? '编辑加速源' : '添加加速源'" @close="show = false">
    <div class="sheet-form">
      <label class="field"><span>名称</span>
        <input v-model.trim="form.name" placeholder="例如：ghproxy.net" required>
      </label>
      <label class="field"><span>地址</span>
        <input v-model.trim="form.url" placeholder="https://ghproxy.net/" required>
        <small>前缀型中转填站点根地址，末尾带斜杠</small>
      </label>
      <label class="field"><span>类型</span>
        <select v-model="form.kind">
          <option v-for="[v, l] in KIND_OPTIONS" :key="v" :value="v">{{ l }}</option>
        </select>
      </label>
      <div class="field-row">
        <label class="field grow"><span>权重（越大越优先）</span>
          <input v-model.number="form.weight" type="number" step="0.1" min="0.1" max="10">
        </label>
        <label class="sw-wrap">
          <span class="sw"><input type="checkbox" v-model="form.enabled"><i></i></span>
          <span>启用</span>
        </label>
      </div>
      <label class="field"><span>备注（可选）</span>
        <input v-model.trim="form.note" placeholder="可选">
      </label>
      <div class="sheet-actions">
        <button v-if="form.id" class="btn danger" :disabled="testing[form.id]" @click="testOne(editing)">
          {{ testing[form.id] ? '测速中…' : '测速' }}
        </button>
        <button v-if="form.id" class="btn danger plain" @click="del">删除</button>
        <span class="sp"></span>
        <button class="btn" @click="show = false">取消</button>
        <button class="btn primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
      </div>
    </div>
  </Sheet>
</template>

<style scoped>
.head-actions { display: flex; align-items: center; gap: 6px; }
.monitor-line { display: flex; align-items: center; gap: 8px; margin: 0; font-size: 12px; color: var(--fg2); }
.pulse-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--green); display: inline-block; animation: pulse 2s ease-in-out infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }
.mlist { display: flex; flex-direction: column; }
.mrow { cursor: pointer; }
.mrow.dim .lrow-t .name { color: var(--fg3); }
.mrow .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rank {
  flex: none; width: 44px; text-align: center;
  font-family: ui-monospace, Menlo, monospace; font-size: 11px; font-weight: 700;
  color: var(--fg2); background: var(--card2); border-radius: 7px; padding: 3px 0;
}
.rank.top { background: var(--green-soft); color: var(--green); }
.chev { width: 14px; height: 14px; color: var(--fg3); flex: none; }
.ghost-line { align-self: center; border-top: 0; }

/* Sheet 内表单 */
.sheet-form { display: flex; flex-direction: column; gap: 14px; padding-top: 10px; }
.field-row { display: flex; align-items: flex-end; gap: 12px; }
.grow { flex: 1; }
.sw-wrap { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--fg); padding-bottom: 10px; flex: none; }
.sheet-actions { display: flex; align-items: center; gap: 8px; margin-top: 4px; }
.sp { flex: 1; }
.btn.plain { background: var(--red-soft); }
</style>
