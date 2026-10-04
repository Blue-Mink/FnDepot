<script setup>
/* DNS 优选 v2：列表行替代表格，hosts 文件内容默认折叠，文案精简 */
import { computed, onMounted, ref } from 'vue'
import { useMessage, useDialog } from 'naive-ui'
import { api, fmtLatency, fmtTime } from '../api'
import Fold from '../components/Fold.vue'

const message = useMessage()
const dialog = useDialog()

const data = ref(null)
const cfg = ref(null)
const status = ref(null)
const dnsHosts = ref([])
const syncing = ref(false)
const fileOpen = ref(false)

async function load() {
  try {
    const [h, c, st, d] = await Promise.all([
      api('/api/hosts'), api('/api/config'), api('/api/status'), api('/api/dns'),
    ])
    data.value = h
    cfg.value = c
    status.value = st
    dnsHosts.value = (d && d.hosts) || []
  } catch { /* 401 由 App 处理 */ }
}
onMounted(load)

const hostsCfg = computed(() => (cfg.value && cfg.value.hosts) || {})

const notice = computed(() => {
  if (!data.value) return ''
  if (!data.value.writable)
    return { cls: 'err', text: '无权限写入 ' + data.value.file + '，请以 root 运行或手动授权。' }
  if (!hostsCfg.value.enabled)
    return { cls: '', text: 'hosts 加速已关闭。开启后自动把 GitHub 域名解析到最快 IP。' }
  const last = fmtTime(status.value && status.value.status && status.value.status.last_hosts_sync)
  return { cls: 'ok', text: '已启用 · 每 ' + (hostsCfg.value.refresh_minutes || 60) + ' 分钟重新优选 · 最后更新 ' + last }
})

const best = computed(() => (data.value && data.value.best) || {})

// /api/dns 的 hosts 是对象 {Host, Category, TLS}（注意大写 Host）
const CAT_LABEL = { web: '网页与 API', raw: '文件下载', clone: '仓库克隆' }
const rows = computed(() => {
  const list = dnsHosts.value.length ? dnsHosts.value : Object.keys(best.value).map((h) => ({ Host: h }))
  const rs = list.map((h) => {
    const host = h.Host || h.host || ''
    const b = best.value[host] || {}
    return {
      host,
      cat: CAT_LABEL[h.Category || h.category] || '',
      ok: !!b.ok, ip: b.ip || '',
      ms: b.ok ? (b.total_ms || 0) : 0,
      total: b.ok ? fmtLatency(b.total_ms) : '待优选',
    }
  })
  // 自动优选排序：已优选的按实测耗时升序（最快在前），未优选/失败的沉底（组内保持原序）。
  // 与「加速源」页同一口径：列表始终按质量呈现，无需手动整理。
  rs.sort((a, b) => {
    if (a.ok !== b.ok) return a.ok ? -1 : 1
    if (a.ok && b.ok) return (a.ms || 0) - (b.ms || 0)
    return 0
  })
  return rs
})
const testedCount = computed(() => rows.value.filter((r) => r.ok).length)
const untestedCount = computed(() => rows.value.length - testedCount.value)
const refreshMin = computed(() => (hostsCfg.value.refresh_minutes || 60))
const entries = computed(() => (data.value && data.value.entries) || [])

async function setEnabled(enabled) {
  try {
    await api('/api/hosts', { method: 'PUT', body: { enabled } })
    message.success(enabled ? '已启用 hosts 加速' : '已关闭并清理 hosts')
    await load()
  } catch (e) { message.error(e.message) }
}

async function sync() {
  syncing.value = true
  try {
    await api('/api/hosts', { method: 'POST' })
    message.success('已开始 DNS 优选')
    setTimeout(() => load().catch(() => {}), 25000)
  } catch (e) { message.error(e.message) }
  finally { syncing.value = false }
}

function clearHosts() {
  dialog.warning({
    title: '清理 hosts',
    content: '移除 hosts 中的全部加速记录？',
    positiveText: '清理',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await api('/api/hosts', { method: 'DELETE' })
        message.success('已清理 hosts')
        await load()
      } catch (e) { message.error(e.message) }
    },
  })
}
</script>

<template>
  <div v-if="data">
    <section class="card">
      <div class="card-head">
        <h2>DNS 优选</h2>
        <div class="head-actions">
          <button class="btn sm" :disabled="syncing" @click="sync">{{ syncing ? '优选中…' : '立即优选' }}</button>
          <button class="btn sm" @click="clearHosts">清理</button>
        </div>
      </div>
      <div class="card-body">
        <div class="toggle-row">
          <div>
            <strong class="toggle-t">启用 hosts 加速</strong>
            <small class="toggle-s">NAS 本机访问 GitHub 时优先走实测最快 IP</small>
          </div>
          <label class="sw">
            <input type="checkbox" :checked="!!hostsCfg.enabled" @change="setEnabled($event.target.checked)"><i></i>
          </label>
        </div>
        <p class="explain">
          优选过程：先用多个公共 DNS 渠道解析出每个域名的候选 IP，再逐个实测
          「TCP 连接 + TLS 握手」耗时，把最快的写入 NAS 本机 hosts。
          之后本机浏览器、git、Docker 访问这些域名都会优先走最快 IP，
          每 {{ refreshMin }} 分钟自动重新优选。
        </p>
        <p v-if="notice.text" class="notice" :class="notice.cls">{{ notice.text }}</p>
        <div class="hlist">
          <div class="hhead">
            <span class="h1">域名</span>
            <span class="h2">最快 IP</span>
            <span class="h3">实测耗时</span>
          </div>
          <p v-if="rows.length" class="sort-hint">已按实测耗时自动排序 · 未优选的沉底</p>
          <p v-if="!rows.length" class="empty">暂无受管域名</p>
          <div v-for="r in rows" :key="r.host" class="lrow" :class="{ dim: !r.ok }">
            <div class="lrow-main">
              <div class="lrow-t mono host" :title="r.host">{{ r.host }}</div>
              <span v-if="r.cat" class="lrow-s">{{ r.cat }}</span>
            </div>
            <span class="mono ip" :class="{ muted: !r.ok }">{{ r.ok ? r.ip : '未实测' }}</span>
            <span class="badge" :class="r.ok ? 'ok' : ''">{{ r.total }}</span>
          </div>
          <p v-if="rows.length" class="hfoot">
            {{ testedCount ? '已优选 ' + testedCount + ' / ' + rows.length + ' 个' : '尚未优选' }}
            <template v-if="untestedCount"> · 点「立即优选」补齐其余</template>
          </p>
        </div>
      </div>
    </section>

    <section class="card">
      <Fold head v-model:open="fileOpen" title="hosts 文件内容" :count="entries.length">
        <div class="hlist">
          <p v-if="!entries.length" class="empty">hosts 中暂无托管记录</p>
          <div v-for="(e, i) in entries" :key="i" class="lrow">
            <span class="mono ip">{{ e.IP || e.ip }}</span>
            <span class="lrow-s host" :title="e.Host || e.host">{{ e.Host || e.host }}</span>
          </div>
        </div>
        <p class="muted small path">{{ data.file }}</p>
      </Fold>
    </section>
  </div>
  <p v-else class="loading">正在加载…</p>
</template>

<style scoped>
.head-actions { display: flex; align-items: center; gap: 6px; }
.toggle-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.toggle-t { font-size: 14px; font-weight: 600; display: block; }
.toggle-s { font-size: 11px; color: var(--fg3); }
.hlist { display: flex; flex-direction: column; }
.hlist .host { word-break: break-all; white-space: normal; }
.hlist .ip { font-size: 12px; color: var(--fg); flex: none; width: 122px; text-align: right; }
.hlist .ip.muted { color: var(--fg3); }
.hlist .badge { flex: none; width: 78px; justify-content: center; }
.hlist .lrow.dim { opacity: 0.55; }
.explain { margin: 0; font-size: 12px; line-height: 1.7; color: var(--fg2); }
.hhead { display: flex; align-items: center; gap: 10px; padding: 10px 0 0; font-size: 11px; color: var(--fg3); }
.sort-hint { margin: 6px 0 0; font-size: 11px; color: var(--fg3); }
.hhead .h1 { flex: 1; min-width: 0; }
.hhead .h2 { width: 122px; flex: none; text-align: right; }
.hhead .h3 { width: 78px; flex: none; text-align: center; }
.hfoot { margin: 2px 0 0; font-size: 11px; color: var(--fg3); }
.lrow-s.host { white-space: normal; word-break: break-all; flex: 1; min-width: 0; }
.path { margin: 0; font-family: ui-monospace, Menlo, monospace; word-break: break-all; }
.loading { text-align: center; color: var(--fg2); padding: 40px 0; }
</style>
