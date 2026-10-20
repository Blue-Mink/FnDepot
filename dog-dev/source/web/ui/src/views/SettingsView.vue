<script setup>
/* 设置 v2：代理参数 / 决策参数 / 后台运行 / 证书 — 文案精简、按钮统一。
 * 1.3.0：控制台默认免登录，移除「修改密码」卡片。 */
import { computed, onMounted, reactive, ref } from 'vue'
import { useMessage, useDialog } from 'naive-ui'
import { api } from '../api'
import { appURL } from '../base'

const message = useMessage()
const dialog = useDialog()

// 证书安装向导与下载地址：经统一网关访问时要带上 /app/dog-dev 前缀。
const caPageURL = appURL('/ca')
const certDownloadURL = appURL('/api/cert/download')

const cfg = ref(null)
const cert = ref(null)
// 局域网各设备的证书信任状态：用于提示「还有哪些设备没装好证书」。
const certClients = ref([])

const proxy = reactive({ listen: '', connect_timeout_ms: 8000, read_timeout_ms: 30000, failover_threshold: 3, cooldown_seconds: 300 })
const auto = reactive({ probe_interval_minutes: 30, mirror_monitor_minutes: 5, direct_better_ratio: 1.5 })
const certForm = reactive({ pem: '', key: '' })
const busy = reactive({ proxy: false, auto: false, regen: false, import: false })

// 只提示「被拒过且还没成功」的设备；一旦握手成功会自动从提示里消失。
const untrustedClients = computed(() =>
  (certClients.value || []).filter((c) => !c.trusted && c.failures > 0)
)

async function load() {
  try {
    const [c, k, cl] = await Promise.all([
      api('/api/config'),
      api('/api/cert').catch(() => null),
      api('/api/cert/clients').catch(() => null),
    ])
    cfg.value = c
    cert.value = k
    certClients.value = (cl && cl.clients) || []
    if (c) {
      proxy.listen = (c.proxy && c.proxy.listen) || ''
      proxy.connect_timeout_ms = (c.proxy && c.proxy.connect_timeout_ms) || 8000
      proxy.read_timeout_ms = (c.proxy && c.proxy.read_timeout_ms) || 30000
      proxy.failover_threshold = (c.proxy && c.proxy.failover_threshold) || 3
      proxy.cooldown_seconds = (c.proxy && c.proxy.cooldown_seconds) || 300
      auto.probe_interval_minutes = (c.auto && c.auto.probe_interval_minutes) || 30
      auto.mirror_monitor_minutes = (c.auto && c.auto.mirror_monitor_minutes) || 5
      auto.direct_better_ratio = (c.auto && c.auto.direct_better_ratio) || 1.5
    }
  } catch { /* 401 由 App 处理 */ }
}
onMounted(load)

const certBrief = computed(() => {
  if (!cert.value) return ''
  if (!cert.value.available) return '证书不可用：' + (cert.value.error || '未知原因')
  if (!cert.value.fingerprint) return '已就绪'
  const until = new Date(cert.value.not_after)
  const p = (n) => String(n).padStart(2, '0')
  return '指纹 ' + cert.value.fingerprint.slice(0, 47) + '… · 有效期至 ' +
    until.getFullYear() + '-' + p(until.getMonth() + 1) + '-' + p(until.getDate())
})

async function saveProxy() {
  if (busy.proxy || !cfg.value) return
  busy.proxy = true
  try {
    await api('/api/config', {
      method: 'PUT',
      body: {
        mode: cfg.value.mode, auto: cfg.value.auto, hosts: cfg.value.hosts,
        proxy: {
          listen: proxy.listen || '0.0.0.0:37710',
          connect_timeout_ms: Number(proxy.connect_timeout_ms),
          read_timeout_ms: Number(proxy.read_timeout_ms),
          failover_threshold: Number(proxy.failover_threshold),
          cooldown_seconds: Number(proxy.cooldown_seconds),
        },
      },
    })
    message.success('已保存，监听地址修改后需重启生效')
    await load()
  } catch (e) { message.error(e.message) }
  finally { busy.proxy = false }
}

async function saveAuto() {
  if (busy.auto || !cfg.value) return
  busy.auto = true
  try {
    await api('/api/config', {
      method: 'PUT',
      body: {
        mode: cfg.value.mode, proxy: cfg.value.proxy, hosts: cfg.value.hosts,
        auto: {
          probe_interval_minutes: Number(auto.probe_interval_minutes),
          mirror_monitor_minutes: Number(auto.mirror_monitor_minutes),
          direct_better_ratio: Number(auto.direct_better_ratio),
          min_improve_ratio: (cfg.value.auto && cfg.value.auto.min_improve_ratio) || 1.3,
        },
      },
    })
    message.success('已保存')
    await load()
  } catch (e) { message.error(e.message) }
  finally { busy.auto = false }
}


async function setWatchdog(enabled) {
  try {
    await api('/api/watchdog', { method: 'PUT', body: { auto_restart: enabled } })
    message.success(enabled ? '已开启崩溃自动重启' : '已关闭崩溃自动重启')
    await load()
  } catch (e) { message.error(e.message) }
}

function regenCert() {
  dialog.warning({
    title: '重新生成证书',
    content: '之前安装过证书的所有设备都需要重新安装。',
    positiveText: '生成',
    negativeText: '取消',
    onPositiveClick: async () => {
      busy.regen = true
      try {
        await api('/api/cert/regenerate', { method: 'POST' })
        message.success('已生成新证书并热生效')
        cert.value = await api('/api/cert').catch(() => null)
      } catch (e) { message.error(e.message) }
      finally { busy.regen = false }
    },
  })
}

async function importCert() {
  if (busy.import) return
  const pem = certForm.pem.trim(), key = certForm.key.trim()
  if (!pem || !key) { message.error('请同时填写证书与私钥（PEM）'); return }
  busy.import = true
  try {
    await api('/api/cert/import', { method: 'POST', body: { cert_pem: pem, key_pem: key } })
    message.success('证书已导入并热生效')
    certForm.pem = ''; certForm.key = ''
    cert.value = await api('/api/cert').catch(() => null)
  } catch (e) { message.error(e.message) }
  finally { busy.import = false }
}
</script>

<template>
  <div v-if="cfg">
    <section class="card">
      <div class="card-head"><h2>代理参数</h2></div>
      <div class="card-body">
        <label class="field"><span>代理监听地址</span>
          <input v-model.trim="proxy.listen" placeholder="0.0.0.0:37710">
          <small>修改后需重启应用生效</small>
        </label>
        <div class="field-2col">
          <label class="field"><span>连接超时（ms）</span>
            <input v-model.number="proxy.connect_timeout_ms" type="number" min="1000" max="60000"></label>
          <label class="field"><span>读取超时（ms）</span>
            <input v-model.number="proxy.read_timeout_ms" type="number" min="5000" max="600000"></label>
        </div>
        <div class="field-2col">
          <label class="field"><span>失败摘除阈值</span>
            <input v-model.number="proxy.failover_threshold" type="number" min="1" max="20"></label>
          <label class="field"><span>摘除冷却（秒）</span>
            <input v-model.number="proxy.cooldown_seconds" type="number" min="10" max="3600"></label>
        </div>
        <button class="btn primary full" :disabled="busy.proxy" @click="saveProxy">保存代理参数</button>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>自动决策</h2></div>
      <div class="card-body">
        <div class="field-2col">
          <label class="field"><span>评估间隔（分钟）</span>
            <input v-model.number="auto.probe_interval_minutes" type="number" min="5" max="1440">
            <small>全量测速 + 优选 + 决策节拍</small>
          </label>
          <label class="field"><span>源监测间隔（分钟）</span>
            <input v-model.number="auto.mirror_monitor_minutes" type="number" min="1" max="1440">
            <small>刷新源健康排序</small>
          </label>
        </div>
        <label class="field"><span>直连优势倍数</span>
          <input v-model.number="auto.direct_better_ratio" type="number" step="0.1" min="1.1" max="10">
          <small>直连快于加速通道该倍数时回退直连</small>
        </label>
        <button class="btn primary full" :disabled="busy.auto" @click="saveAuto">保存决策参数</button>
      </div>
    </section>


    <section class="card">
      <div class="card-head">
        <h2>后台运行</h2>
        <label class="sw">
          <input type="checkbox" :checked="!!(cfg.watchdog && cfg.watchdog.auto_restart)" @change="setWatchdog($event.target.checked)"><i></i>
        </label>
      </div>
      <div class="card-body">
        <p class="notice">
          崩溃自动重启：进程异常退出时看门狗 3 秒内拉起；配合 setsid 脱离会话，关闭窗口与登出账号都不影响加速。
        </p>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>HTTPS 证书</h2>
        <span class="muted small cert-brief">{{ certBrief }}</span>
      </div>
      <div class="card-body">
        <div class="row">
          <a class="btn primary" :href="caPageURL" target="_blank" rel="noopener">安装向导</a>
          <a class="btn" :href="certDownloadURL">下载 .crt</a>
          <button class="btn" :disabled="busy.regen" @click="regenCert">{{ busy.regen ? '生成中…' : '重新生成' }}</button>
        </div>
        <p v-if="untrustedClients.length" class="notice warn">
          有 {{ untrustedClients.length }} 台设备访问 GitHub 时被证书拦住：{{ untrustedClients.map((c) => c.ip).join('、') }}。
          用那台设备打开上面的「安装向导」扫码安装根证书即可（装完会自动从此列表消失）。
        </p>
        <p class="notice">
          默认使用本地自动生成的免费证书（各客户端安装信任一次）。可导入自己的 CA 证书与私钥，立即热生效。
        </p>
        <label class="field"><span>CA 证书（PEM）</span>
          <textarea v-model="certForm.pem" rows="3" class="code-area" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
        </label>
        <label class="field"><span>CA 私钥（PEM）</span>
          <textarea v-model="certForm.key" rows="3" class="code-area" placeholder="-----BEGIN RSA PRIVATE KEY-----"></textarea>
          <small>私钥仅存本机（权限 0600），不会上传</small>
        </label>
        <button class="btn primary" :disabled="busy.import" @click="importCert">{{ busy.import ? '导入中…' : '导入并替换证书' }}</button>
      </div>
    </section>
  </div>
  <p v-else class="loading">正在加载…</p>
</template>

<style scoped>
.field-2col { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
@media (max-width: 560px) { .field-2col { grid-template-columns: 1fr; } }
.cert-brief { font-family: ui-monospace, Menlo, monospace; max-width: 55%; word-break: break-all; }
.row { display: flex; gap: 8px; }
.loading { text-align: center; color: var(--fg2); padding: 40px 0; }
</style>
