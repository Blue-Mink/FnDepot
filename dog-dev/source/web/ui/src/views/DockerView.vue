<script setup>
/* Docker 加速 v2：KSpeeder 依赖应用（节点折叠）+ 拉取加速（上游/注册表折叠）+ 系统自身片段（折叠）
 * 移动端优先，长文案全部精简。 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useMessage, useDialog } from 'naive-ui'
import { api, fmtBytes, fmtLatency, fmtTime } from '../api'
import Fold from '../components/Fold.vue'
import { useCopy } from '../useCopy'

const message = useMessage()
const dialog = useDialog()
const copyText = useCopy()

const ks = ref(null)
const ksDownload = ref('')
const ksRecMin = ref('0.8.2')
const nodes = ref([])
const nodesError = ref('')
const d = ref(null)
const newUpstream = ref('')
const probing = ref(false)
const applying = ref(false)
const saving = ref(false)

/* 折叠状态：节点默认收起（长列表），上游默认展开（少而关键），其余收起 */
const nodesOpen = ref(false)
const upstreamOpen = ref(true)
const regOpen = ref(false)
const sysOpen = ref(false)

let timer = null

async function load() {
  try {
    const [k, dd] = await Promise.all([api('/api/kspeeder'), api('/api/docker')])
    ks.value = (k && k.status) || {}
    ksDownload.value = (k && k.download_url) || ''
    ksRecMin.value = (k && k.recommended_engine) || '0.8.2'
    nodes.value = (k && k.nodes) || []
    nodesError.value = (k && k.nodes_error) || ''
    d.value = dd
  } catch { /* 401 由 App 处理 */ }
}
onMounted(() => {
  load()
  timer = setInterval(load, 10000)
})
onUnmounted(() => { if (timer) clearInterval(timer) })

const KS_MODE = { running: '运行中', stopped: '已安装（未启动）', absent: '未安装' }
const ksBadge = computed(() => {
  const mode = ks.value && ks.value.mode
  const cls = mode === 'running' ? 'ok' : mode === 'stopped' ? '' : 'err'
  return { text: KS_MODE[mode] || mode || '检测中', cls }
})
const ksUrl = computed(() => (ks.value && ks.value.url) || '')

const proxyPort = computed(() => ((d.value && d.value.proxy_url) || '').split(':').pop() || '37710')
const base = computed(() => {
  const u = d.value && d.value.proxy_url
  if (!u) return 'http://NAS-IP:' + proxyPort.value
  return u.replace(/^https?:\/\//, '').replace(/:\d+$/, '') === ''
    ? 'http://NAS-IP:' + proxyPort.value
    : u
})
const snippets = computed(() => {
  const b = base.value
  return {
    git: 'git config --global url."' + b + '/https://github.com/".insteadOf "https://github.com/"\n# 取消：git config --global --unset url."' + b + '/https://github.com/".insteadOf',
    curl: 'wget ' + b + '/https://github.com/用户/仓库/releases/download/v1.0/文件.zip',
    proxy: 'export http_proxy=' + b + '   # 仅加速 GitHub，其他域名直连',
  }
})

const upstreamRows = computed(() => ((d.value && d.value.upstream_list) || []).map((u) => {
  const cfgToken = u.official ? 'official' : (u.url || '').replace(/^https?:\/\//, '').replace(/\/$/, '')
  return Object.assign({}, u, {
    cfgToken,
    deletable: !u.builtin_engine,
  })
}))

async function addUpstream() {
  const u = newUpstream.value.trim()
  if (!u) { message.warning('请先输入上游地址'); return }
  if (saving.value) return
  const cur = (d.value && d.value.upstreams) || []
  const norm = u.replace(/^https?:\/\//, '').replace(/\/$/, '')
  if (cur.some((x) => x.replace(/^https?:\/\//, '').replace(/\/$/, '') === norm)) {
    message.warning('该上游已在列表中')
    return
  }
  saving.value = true
  try {
    const data = await api('/api/docker', { method: 'PUT', body: { enabled: !!d.value.enabled, upstreams: cur.concat([u]) } })
    d.value = data
    newUpstream.value = ''
    message.success('已添加上游')
  } catch (e) { message.error(e.message) }
  finally { saving.value = false }
}

function delUpstream(row) {
  if (!row || !row.deletable || saving.value) return
  dialog.warning({
    title: '移除上游',
    content: '确定移除「' + (row.name || row.url) + '」？',
    positiveText: '移除',
    negativeText: '取消',
    onPositiveClick: async () => {
      const cur = (d.value && d.value.upstreams) || []
      const next = cur.filter((x) => x.replace(/^https?:\/\//, '').replace(/\/$/, '') !== row.cfgToken)
      if (next.length === cur.length) { message.warning('未找到对应的配置项'); return }
      saving.value = true
      try {
        const data = await api('/api/docker', { method: 'PUT', body: { enabled: !!d.value.enabled, upstreams: next } })
        d.value = data
        message.success('已移除上游')
      } catch (e) { message.error(e.message) }
      finally { saving.value = false }
    },
  })
}

async function setMonitor(on) {
  if (!d.value || saving.value) return
  saving.value = true
  try {
    d.value = await api('/api/docker', { method: 'PUT', body: { enabled: !!d.value.enabled, auto_monitor: on } })
    message.success(on ? '自动监测已开启' : '自动监测已关闭')
  } catch (e) {
    message.error(e.message)
    load()
  } finally { saving.value = false }
}

const monitor = computed(() => (d.value && d.value.monitor) || null)
const monitorLast = computed(() => {
  if (!monitor.value) return '尚未监测'
  return monitor.value.last ? '最近监测 ' + fmtTime(monitor.value.last) : '尚未监测'
})

async function probeUpstreams() {
  if (probing.value) return
  probing.value = true
  try {
    await api('/api/docker/test', { method: 'POST' })
    await load()
    message.success('全部上游探活完成')
  } catch (e) { message.error(e.message) }
  finally { probing.value = false }
}

const nodeProfiles = computed(() => {
  const map = {}
  for (const n of nodes.value) {
    if (!map[n.profile]) map[n.profile] = []
    map[n.profile].push(n)
  }
  return map
})

function nodeSpeed(v) {
  if (!v || v <= 0) return '—'
  return fmtBytes(v) + '/s'
}

function applyDocker() {
  dialog.warning({
    title: '一键应用到系统',
    content: '将写入 /etc/docker/daemon.json（保留其他配置）并重启 Docker，运行中的容器会中断几秒。',
    positiveText: '应用',
    negativeText: '取消',
    onPositiveClick: async () => {
      applying.value = true
      try {
        const res = await api('/api/docker/apply', { method: 'POST' })
        message.success((res && res.message) || '已应用并重启 Docker')
        await load()
      } catch (e) { message.error(e.message) }
      finally { applying.value = false }
    },
  })
}
</script>

<template>
  <div>
    <section class="card">
      <div class="card-head">
        <h2>KSpeeder 应用（依赖）</h2>
        <span class="badge" :class="ksBadge.cls">{{ ksBadge.text }}</span>
      </div>
      <div class="card-body">
        <div class="ks-grid">
          <div class="kv"><span class="k">应用版本</span><strong>{{ ks && ks.app_version ? ks.app_version : '—' }}</strong></div>
          <div class="kv"><span class="k">引擎版本</span><strong>{{ ks && ks.engine_version ? ks.engine_version : '—' }}</strong></div>
          <div class="kv"><span class="k">Registry</span><strong class="mono small">{{ ks && ks.registry ? ks.registry : '—' }}</strong></div>
          <div class="kv"><span class="k">管理接口</span><strong class="mono small">{{ ks && ks.admin ? ks.admin : '—' }}</strong></div>
        </div>
        <div class="copy-row">
          <input :value="ksUrl" readonly placeholder="kspeeder 运行后显示本机 Docker 接入地址">
          <button class="btn sm" @click="copyText(ksUrl)">复制</button>
        </div>
        <p v-if="ks && ks.reason" class="muted small">{{ ks.reason }}</p>
        <p v-if="ks && ks.engine_below_recommended" class="muted small">
          引擎 {{ ks.engine_version }} 低于推荐版本 {{ ksRecMin }}（分段下载 + 节点竞速，单节点中断自动换源），建议升级 kspeeder 应用。
        </p>
        <p v-if="ks && ks.mode === 'absent' && ksDownload" class="muted small">
          安装 kspeeder 应用后无需任何配置，Docker 拉取自动经其加速：
          <a :href="ksDownload" target="_blank" rel="noopener">下载 kspeeder</a>
        </p>
        <Fold v-if="nodes.length" v-model:open="nodesOpen" title="加速节点" :count="nodes.length">
          <div v-for="(list, profile) in nodeProfiles" :key="profile" class="node-group">
            <h3 class="node-title">{{ profile === 'docker:dockerhub' ? 'Docker Hub 节点' : profile }}</h3>
            <div class="nlist">
              <div v-for="n in list" :key="n.profile + n.node_id" class="lrow" :class="{ dim: !n.alive }">
                <div class="lrow-main">
                  <div class="lrow-t">
                    <span class="name">{{ n.name }}</span>
                    <span v-if="n.is_internal" class="badge warn">内置</span>
                    <span class="badge" :class="n.alive && n.state === 'healthy' ? 'ok' : (n.alive ? '' : 'err')">{{ n.state || 'unknown' }}</span>
                  </div>
                  <div class="lrow-s">P{{ n.priority }} · 实时 {{ nodeSpeed(n.live_speed_bps) }} · 累计 {{ n.live_bytes_total > 0 ? fmtBytes(n.live_bytes_total) : '—' }}</div>
                </div>
              </div>
            </div>
          </div>
          <p class="muted small">节点自动切换：传输中断或连续失败会被临时禁用，流量落到其余节点（10 秒自动刷新）。</p>
        </Fold>
        <p v-else-if="nodesError" class="muted small">节点状态暂不可用：{{ nodesError }}</p>
      </div>
    </section>

    <section v-if="d" class="card">
      <div class="card-head">
        <h2>Docker 拉取加速</h2>
        <div class="head-actions">
          <span class="badge" :class="d.applied ? 'ok' : ''">{{ d.applied ? '已应用' : '未应用' }}</span>
          <button class="btn sm" :disabled="probing" @click="probeUpstreams">{{ probing ? '探活中…' : '探活' }}</button>
        </div>
      </div>
      <div class="card-body">
        <div class="toggle-row">
          <div>
            <strong class="toggle-t">启用 Docker 加速</strong>
            <small class="toggle-s">docker pull 自动走加速器，无需手动填镜像源</small>
          </div>
          <label class="sw">
            <input type="checkbox" :checked="!!d.enabled" @change="d.enabled = $event.target.checked"><i></i>
          </label>
        </div>
        <div class="toggle-row">
          <div>
            <strong class="toggle-t">自动监测</strong>
            <small class="toggle-s">周期性探活上游，让「最优在前、失效沉底」的排序持续保鲜</small>
          </div>
          <label class="sw">
            <input type="checkbox" :checked="!!(monitor && monitor.enabled)" :disabled="saving" @change="setMonitor($event.target.checked)"><i></i>
          </label>
        </div>
        <p v-if="monitor" class="muted small">{{ monitorLast }} · 间隔 {{ monitor.minutes }} 分钟（与加速源监测共用，设置页可调）</p>
        <button class="btn primary full" :disabled="applying" @click="applyDocker">
          {{ applying ? '应用中…' : '一键应用到系统（重启 Docker）' }}
        </button>
        <div class="copy-row">
          <input :value="d.registry_mirror || ''" readonly placeholder="加速地址">
          <button class="btn sm" @click="copyText(d.registry_mirror || '')">复制</button>
        </div>
        <Fold v-model:open="upstreamOpen" title="上游列表" :count="upstreamRows.length">
          <div class="ulist">
            <div v-for="u in upstreamRows" :key="u.url" class="lrow" :class="{ dim: !u.ok && u.probed }">
              <span class="rank" :class="{ top: u.rank === 1 }">{{ u.rank }}</span>
              <div class="lrow-main">
                <div class="lrow-t">
                  <span class="name">{{ u.name }}</span>
                  <span v-if="u.builtin_engine" class="badge ac">KSpeeder</span>
                  <span v-else-if="u.official" class="badge">官方兜底</span>
                </div>
                <div class="lrow-s" :title="u.error">{{ u.url }}</div>
              </div>
              <span class="mono lat">{{ fmtLatency(u.latency_ms) }}</span>
              <span class="badge" :class="!u.probed ? '' : (u.ok ? 'ok' : 'err')" :title="u.error">{{ !u.probed ? '未探活' : (u.ok ? '正常' : '不可用') }}</span>
              <button v-if="u.deletable" class="btn sm danger" :disabled="saving" @click="delUpstream(u)">移除</button>
            </div>
          </div>
          <div class="add-row">
            <input v-model.trim="newUpstream" placeholder="添加上游，如 https://docker.1ms.run" @keyup.enter="addUpstream">
            <button class="btn sm primary" :disabled="saving" @click="addUpstream">添加</button>
          </div>
        </Fold>
        <Fold v-if="d.supported_registries && d.supported_registries.length" v-model:open="regOpen" title="支持的注册表" :count="d.supported_registries.length">
          <div class="rlist">
            <div v-for="r in d.supported_registries" :key="r.registry" class="lrow">
              <span class="mono ip">{{ r.registry }}</span>
              <span class="lrow-s reg-note">{{ r.note }}</span>
            </div>
          </div>
        </Fold>
      </div>
    </section>

    <section class="card">
      <Fold head v-model:open="sysOpen" title="高级：飞牛系统自身加速">
        <p class="notice">
          hosts 模式开启时，系统自身的 GitHub 访问（应用商店、更新检查）已自动加速。以下片段仅供高级用户。
        </p>
        <label class="field"><span>Git 全局代理（NAS 本机执行一次）</span>
          <textarea :value="snippets.git" rows="2" readonly class="code-area"></textarea>
          <div class="copy-row"><button class="btn sm" @click="copyText(snippets.git)">复制</button></div>
        </label>
        <label class="field"><span>wget / curl 前缀下载</span>
          <textarea :value="snippets.curl" rows="2" readonly class="code-area"></textarea>
          <div class="copy-row"><button class="btn sm" @click="copyText(snippets.curl)">复制</button></div>
        </label>
        <label class="field"><span>局域网设备设 HTTP 代理</span>
          <textarea :value="snippets.proxy" rows="2" readonly class="code-area"></textarea>
          <div class="copy-row"><button class="btn sm" @click="copyText(snippets.proxy)">复制</button></div>
        </label>
      </Fold>
    </section>
  </div>
</template>

<style scoped>
.head-actions { display: flex; align-items: center; gap: 6px; }
/* minmax(0,1fr)：1fr 的轨道最小宽度默认取内容宽，长 URL 不可断行时会把第二列撑出卡片（窄屏横向溢出） */
.ks-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
@media (max-width: 560px) { .ks-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
.kv { display: flex; flex-direction: column; gap: 3px; background: var(--card2); border-radius: var(--r-sm); padding: 10px 12px; min-width: 0; }
.kv .k { font-size: 11px; color: var(--fg2); }
.kv strong { font-size: 13px; overflow-wrap: anywhere; }
.toggle-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.toggle-t { font-size: 14px; font-weight: 600; display: block; }
.toggle-s { font-size: 11px; color: var(--fg3); }
.node-group { display: flex; flex-direction: column; gap: 6px; }
.node-group + .node-group { margin-top: 10px; }
.node-title { margin: 0; font-size: 12px; color: var(--fg2); font-weight: 600; }
.nlist, .ulist, .rlist { display: flex; flex-direction: column; }
.lrow-t .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.lrow.dim .lrow-t .name { color: var(--fg3); }
.lat { flex: none; width: 48px; text-align: right; color: var(--fg2); font-size: 12px; }
.rank {
  flex: none; width: 38px; text-align: center;
  font-family: ui-monospace, Menlo, monospace; font-size: 11px; font-weight: 700;
  color: var(--fg2); background: var(--card2); border-radius: 7px; padding: 3px 0;
}
.rank.top { background: var(--green-soft); color: var(--green); }
.add-row { display: flex; gap: 8px; margin-top: 10px; }
.add-row input { flex: 1; }
.add-row input, .copy-row input {
  border: 0.5px solid var(--line); background: var(--card2); color: var(--fg);
  border-radius: 10px; padding: 9px 11px; font-size: 13px; outline: none;
}
.rlist .ip { flex: none; font-size: 12px; }
.reg-note { flex: 1; white-space: normal; word-break: break-all; }
</style>
