<script setup>
/* 总览 v2：指标×6 / 加速模式 / 请求分布 / 接入方式（折叠）— 移动端优先，文案精简 */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useMessage, useDialog } from 'naive-ui'
import { api, fmtBytes, fmtDuration } from '../api'
import StatCard from '../components/StatCard.vue'
import Fold from '../components/Fold.vue'
import Sheet from '../components/Sheet.vue'
import { useCopy } from '../useCopy'

const message = useMessage()
const dialog = useDialog()
const copyText = useCopy()

const MODES = [['auto', '自动'], ['proxy', '镜像中转'], ['hosts', 'DNS 优选'], ['direct', '直连']]
const MODE_NAMES = Object.fromEntries(MODES)
const CHART_COLORS = ['#0a84ff', '#30d158', '#ff9f0a', '#bf5af2']
const CHART_LABELS = ['网页与 API', '文件下载', '仓库克隆', 'Docker 拉取']

const st = ref(null)
let timer = null

async function load() {
  try { st.value = await api('/api/status') } catch { /* 401 由 App 处理 */ }
}
onMounted(() => { load(); timer = setInterval(load, 10000) })
onBeforeUnmount(() => clearInterval(timer))

const s = computed(() => (st.value && st.value.status) || null)
const m = computed(() => (s.value && s.value.metrics) || null)

const metrics = computed(() => {
  if (!m.value) return []
  const hit = m.value.total_requests > 0 ? Math.round(m.value.accelerated / m.value.total_requests * 100) : 0
  return [
    { label: '累计请求', value: m.value.total_requests.toLocaleString(), sub: '加速 ' + m.value.accelerated.toLocaleString() + ' 次' },
    { label: '加速命中率', value: hit + '%', sub: m.value.failed > 0 ? '失败 ' + m.value.failed + ' 次' : '无失败' },
    { label: '流量转发', value: fmtBytes(m.value.bytes_out), sub: '节省约 ' + fmtDuration(m.value.saved_ms / 1000) },
    { label: '可用加速源', value: s.value.mirrors.healthy + ' / ' + s.value.mirrors.enabled, sub: '共 ' + s.value.mirrors.total + ' 个' },
    { label: '运行时长', value: s.value.uptime || '—', sub: '代理 :37710' },
    { label: '换源次数', value: String(m.value.failovers), sub: '自动故障转移' },
  ]
})

const cats = computed(() => {
  if (!m.value) return []
  const data = [m.value.web_count, m.value.raw_count, m.value.clone_count, m.value.docker_count || 0]
  const total = data.reduce((a, b) => a + b, 0)
  return CHART_LABELS.map((label, i) => ({
    label, color: CHART_COLORS[i],
    pct: total > 0 ? Math.round(data[i] / total * 100) : 0, n: data[i],
  }))
})

async function setMode(mode) {
  try {
    await api('/api/mode', { method: 'POST', body: { mode } })
    message.success('已切换到「' + MODE_NAMES[mode] + '」')
    await load()
  } catch (e) { message.error(e.message) }
}

function toggleService() {
  const running = !!(s.value && s.value.running)
  dialog.warning({
    title: running ? '停止服务' : '启动服务',
    content: running ? '停止后所有加速通道中断，需手动启动恢复。' : '启动加速服务？',
    positiveText: running ? '停止' : '启动',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api('/api/service', { method: 'POST', body: { action: running ? 'stop' : 'start' } })
        message.success(running ? '服务已停止' : '服务已启动')
        setTimeout(load, running ? 500 : 2500)
      } catch (e) { message.error(e.message) }
    },
  })
}

/* ---- 接入方式（折叠） ----
 * 地址选择不用原生 select（浏览器弹出的系统下拉与整体 iOS 风格不搭），
 * 改用全站统一的 Sheet 弹层（移动端底部抽屉 / 桌面居中卡片）。 */
const connectsOpen = ref(false)
const pickOpen = ref(false)
const hostSel = ref('')
const hostInput = ref('')
const curHost = computed(() => location.hostname || '127.0.0.1')
const hostOptions = computed(() => {
  const opts = [{ v: curHost.value, t: '当前访问', s: curHost.value }]
  ;((s.value && s.value.local_ips) || []).forEach((ip) => {
    if (ip !== curHost.value) opts.push({ v: ip, t: '局域网', s: ip })
  })
  const ext = s.value && s.value.external_host
  if (ext) opts.push({ v: ext, t: '外网', s: ext })
  opts.push({ v: '__custom__', t: '自定义', s: '填域名或 IP' })
  return opts
})
const accessHost = computed(() => {
  if (hostSel.value === '__custom__') return hostInput.value.trim() || curHost.value
  return hostSel.value || curHost.value
})
const proxyPort = computed(() => ((s.value && s.value.proxy_addr) || '').split(':').pop() || '37710')

function pickOption(o) {
  hostSel.value = o.v
  if (o.v !== '__custom__') pickOpen.value = false // 普通选项即选即关；自定义需继续输入
}
function closePick() {
  if (hostSel.value === '__custom__' && !hostInput.value.trim()) return
  pickOpen.value = false
}

const connects = computed(() => {
  const host = accessHost.value
  return [
    { tag: 'HTTP', title: 'HTTP 代理（推荐）', code: host + ':' + proxyPort.value, desc: '系统或浏览器代理设为该地址' },
    { tag: 'Git', title: 'Git 全局配置', code: 'git config --global http.proxy http://' + host + ':' + proxyPort.value, desc: 'git clone / pull 自动加速' },
    { tag: 'HOST', title: 'hosts 指向本机', code: host + '  github.com', desc: '域名解析到 NAS，透明加速' },
    { tag: 'DOCKER', title: 'Docker 镜像加速（本机）', code: 'http://127.0.0.1:' + proxyPort.value, desc: '写入 registry-mirrors' },
    { tag: 'URL', title: '前缀下载（wget / curl）', code: 'http://' + host + ':' + proxyPort.value + '/https://github.com/用户/仓库/…', desc: 'GitHub 地址前加本机前缀' },
  ]
})

async function saveExternal() {
  const host = accessHost.value
  if (!host) { message.warning('先选择或填写地址'); return }
  try {
    await api('/api/network', { method: 'PUT', body: { host } })
    message.success('外网接入地址已保存')
    await load()
  } catch (e) { message.error(e.message) }
}
</script>

<template>
  <div v-if="s && m">
    <div class="metrics">
      <StatCard v-for="x in metrics" :key="x.label" v-bind="x" />
    </div>

    <section class="card">
      <div class="card-head">
        <h2>加速模式</h2>
        <button class="btn sm" :class="s.running ? 'danger' : 'primary'" @click="toggleService">
          {{ s.running ? '停止服务' : '启动服务' }}
        </button>
      </div>
      <div class="card-body">
        <div class="seg">
          <button v-for="[v, l] in MODES" :key="v" :class="{ on: s.config_mode === v }" @click="setMode(v)">{{ l }}</button>
        </div>
        <p class="verdict">当前生效：{{ MODE_NAMES[s.effective_mode] || s.effective_mode }}
          <template v-if="st.verdict && st.verdict.reason"> · {{ st.verdict.reason }}</template>
          <template v-else> · 尚未评估，可到「加速源」页立即测速</template>
        </p>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>请求分布</h2></div>
      <div class="card-body">
        <div v-for="c in cats" :key="c.label" class="cat-row">
          <span class="cat-label"><i :style="{ background: c.color }"></i>{{ c.label }}</span>
          <span class="cat-bar"><span :style="{ width: c.pct + '%', background: c.color }"></span></span>
          <span class="cat-pct">{{ c.pct }}%</span>
        </div>
      </div>
    </section>

    <section class="card">
      <Fold head v-model:open="connectsOpen" title="接入方式" :count="5">
        <div class="host-pick">
          <button class="host-row" type="button" @click="pickOpen = true">
            <span class="hr-t">接入地址</span>
            <span class="hr-v">{{ accessHost }}</span>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7"/></svg>
          </button>
          <button class="btn sm" @click="saveExternal">保存</button>
        </div>
        <div v-for="it in connects" :key="it.tag" class="connect-item">
          <div class="ci-icon">{{ it.tag }}</div>
          <div class="ci-body">
            <strong>{{ it.title }}</strong>
            <code :title="it.code">{{ it.code }}</code>
            <small>{{ it.desc }}</small>
          </div>
          <button class="btn sm" @click="copyText(it.code)">复制</button>
        </div>
      </Fold>

      <Sheet :show="pickOpen" title="选择接入地址" @close="pickOpen = false">
        <div class="opt-list">
          <button v-for="o in hostOptions" :key="o.v" class="opt" :class="{ on: hostSel === o.v }" type="button" @click="pickOption(o)">
            <span class="opt-main">
              <strong>{{ o.t }}</strong>
              <small class="mono">{{ o.s }}</small>
            </span>
            <svg v-if="hostSel === o.v" class="opt-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 12.5l5 5 10-11"/></svg>
          </button>
        </div>
        <div v-if="hostSel === '__custom__'" class="custom-wrap">
          <input v-model="hostInput" placeholder="nas.example.com 或 IP" @keyup.enter="closePick">
          <button class="btn primary full" type="button" @click="closePick">确定</button>
        </div>
      </Sheet>
    </section>
  </div>

  <p v-else class="loading">正在加载…</p>
</template>

<style scoped>
.metrics { display: grid; grid-template-columns: 1fr 1fr; gap: var(--gap); }
@media (min-width: 700px) { .metrics { grid-template-columns: 1fr 1fr 1fr; } }
/* 大卡片之间的间距由全局 style.css 统一（.main .card + .card），所有页面同节奏 */
.verdict { margin: 0; font-size: 12px; color: var(--fg2); background: var(--card2); border-radius: var(--r-sm); padding: 10px 12px; line-height: 1.6; }
.cat-row { display: flex; align-items: center; gap: 10px; }
.cat-label { display: flex; align-items: center; gap: 6px; width: 88px; flex: none; font-size: 12px; color: var(--fg2); }
.cat-label i { width: 8px; height: 8px; border-radius: 3px; }
.cat-bar { flex: 1; height: 6px; border-radius: 3px; background: var(--card2); overflow: hidden; }
.cat-bar span { display: block; height: 100%; border-radius: 3px; transition: width 0.3s; }
.cat-pct { width: 38px; text-align: right; font-size: 12px; color: var(--fg2); }
/* 接入地址行：单行紧凑（标签左 / 地址右 / 箭头），点入 Sheet 选择（替代原生 select）。
 * 1.1.14 由两行版压成单行：此前框高约 45px，和下方第一条分隔线挤在一起。 */
.host-pick { display: flex; gap: 8px; align-items: center; }
.host-row {
  flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px;
  border: 0.5px solid var(--line); background: var(--card2); color: var(--fg);
  border-radius: 10px; padding: 7px 12px; cursor: pointer; text-align: left;
  transition: opacity 0.15s;
}
.host-row:active { opacity: 0.7; }
.host-row svg { width: 14px; height: 14px; color: var(--fg3); flex: none; }
.hr-t { flex: none; font-size: 12px; color: var(--fg3); }
.hr-v {
  flex: 1; min-width: 0; text-align: right;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px;
  color: var(--fg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
/* Sheet 内选项 */
.opt-list { display: flex; flex-direction: column; gap: 8px; }
.opt {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  border: 0.5px solid var(--line); background: var(--card2); color: var(--fg);
  border-radius: 12px; padding: 11px 14px; cursor: pointer; text-align: left;
  transition: opacity 0.15s;
}
.opt:active { opacity: 0.7; }
.opt.on { border-color: var(--accent); background: var(--accent-soft); }
.opt-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.opt-main strong { font-size: 14px; font-weight: 600; }
.opt-main small { font-size: 11px; color: var(--fg3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.opt-check { width: 16px; height: 16px; color: var(--accent); flex: none; }
.custom-wrap { display: flex; flex-direction: column; gap: 10px; margin-top: 14px; }
.custom-wrap input {
  border: 0.5px solid var(--line); background: var(--card2); color: var(--fg);
  border-radius: 10px; padding: 9px 11px; font-size: 14px; outline: none; width: 100%;
}
.custom-wrap input:focus { border-color: var(--accent); }
.connect-item { display: flex; align-items: flex-start; gap: 10px; padding: 12px 0; border-top: 0.5px solid var(--hairline); }
/* 首条紧贴地址框：旧 .connect-item:first-of-type 永远不中（第一个 div 兄弟是 .host-pick），
 * 导致 hairline 直接压在选择框下边。改相邻兄弟选择器：首条去线，与框留 14px 间距。 */
.host-pick + .connect-item { border-top: 0; padding-top: 0; margin-top: 14px; }
.ci-icon { flex: none; width: 46px; text-align: center; font-size: 10px; font-weight: 700; color: var(--accent); background: var(--accent-soft); border-radius: 8px; padding: 4px 0; }
.ci-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.ci-body strong { font-size: 13px; }
.ci-body code { font: 11px/1.5 ui-monospace, Menlo, monospace; color: var(--fg); background: var(--card2); border-radius: 6px; padding: 4px 8px; word-break: break-all; }
.ci-body small { font-size: 11px; color: var(--fg3); }
.loading { text-align: center; color: var(--fg2); padding: 40px 0; }
</style>
