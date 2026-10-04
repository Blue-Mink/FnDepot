<script setup>
/* 应用加速 v2：状态三格 + 系统代理开关（含 CA 信任联动） + 四种方式单行化 */
import { computed, h, inject, onMounted, ref } from 'vue'
import { useMessage, useDialog } from 'naive-ui'
import { api } from '../api'

const message = useMessage()
const dialog = useDialog()
const goTab = inject('goTab', () => {})

const st = ref(null)
const cert = ref(null)
const applying = ref(false)
const caInstalling = ref(false)

async function load() {
  try { st.value = (await api('/api/status')).status } catch { /* 401 由 App 处理 */ }
}
async function loadCert() {
  try { cert.value = await api('/api/cert') } catch { /* 忽略：证书不可用时不显示信任提示 */ }
}
onMounted(() => { load(); loadCert() })

const items = computed(() => st.value ? [
  { label: 'hosts 加速', on: !!st.value.hosts_enabled },
  { label: 'Docker 加速', on: !!st.value.docker_enabled },
  { label: '系统代理', on: !!st.value.system_proxy_enabled },
] : [])

async function doSetSysProxy(enabled, installCA) {
  try {
    const body = { enabled }
    if (installCA !== null && installCA !== undefined) body.install_ca = installCA
    const res = await api('/api/sysproxy', { method: 'PUT', body })
    message.success((res && res.note) || (enabled ? '已开启系统代理' : '已关闭系统代理'))
    await load(); await loadCert()
  } catch (e) { message.error(e.message) }
}

function setSysProxy(enabled) {
  if (!enabled) {
    dialog.warning({
      title: '关闭系统代理',
      content: '新 shell 不再走代理，已运行的进程保留原环境变量直到重启。',
      positiveText: '关闭',
      negativeText: '取消',
      onPositiveClick: () => doSetSysProxy(false, null),
    })
    return
  }
  // 勾选框默认开：一次点击全速加速（git/ssh 的 HTTPS 也走 MITM）；
  // 用户可取消勾选，取消后流量走透传隧道（可用不加速），可随时一键补装。
  const installCA = ref(true)
  dialog.warning({
    title: '开启系统代理',
    content: () => h('div', { style: 'display:flex;flex-direction:column;gap:10px' }, [
      h('p', { style: 'margin:0;font-size:13px;line-height:1.6' },
        '新 shell 启动的软件（apt / git / pip / npm 等）默认走加速器。已运行的进程不受影响。'),
      h('label', { style: 'display:flex;gap:8px;align-items:flex-start;cursor:pointer;font-size:13px' }, [
        h('input', {
          type: 'checkbox',
          style: 'margin-top:3px',
          checked: installCA.value,
          onChange: (e) => { installCA.value = e.target.checked },
        }),
        h('span', null, '同时安装本地 CA 到系统信任（git / ssh 的 HTTPS 全速加速需要；不勾选则走透传隧道，可用但不加速）'),
      ]),
    ]),
    positiveText: '开启',
    negativeText: '取消',
    onPositiveClick: () => doSetSysProxy(true, installCA.value),
  })
}

async function installCA() {
  caInstalling.value = true
  try {
    const res = await api('/api/cert/install-system', { method: 'POST' })
    message.success((res && res.note) || '本地 CA 已装入系统信任')
    await loadCert()
  } catch (e) { message.error(e.message) }
  finally { caInstalling.value = false }
}

function applyDocker() {
  dialog.warning({
    title: '一键应用 Docker 配置',
    content: '将写入 /etc/docker/daemon.json 并重启 Docker，运行中的容器会中断几秒。',
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
  <div v-if="st">
    <section class="card">
      <div class="card-head"><h2>加速状态</h2></div>
      <div class="card-body">
        <div class="status-grid">
          <div v-for="x in items" :key="x.label" class="status-item">
            <div class="s-label">{{ x.label }}</div>
            <div class="s-value" :class="x.on ? 'on' : 'off'">{{ x.on ? '已开启' : '未开启' }}</div>
          </div>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>系统级 HTTP 代理</h2>
        <label class="sw">
          <input type="checkbox" :checked="!!st.system_proxy_enabled" @change="setSysProxy($event.target.checked)"><i></i>
        </label>
      </div>
      <div class="card-body">
        <p class="notice">
          开启后写入 <code>/etc/profile.d/ghpp-proxy.sh</code>，新 shell 的命令行工具默认走加速器。
          加速器只对 GitHub 域名走镜像、其他流量透传；加速器故障可能影响新 shell 联网。
        </p>
        <div v-if="st.system_proxy_enabled && cert && cert.available && !cert.system_trusted" class="notice warn ca-warn">
          <p>本地 CA 未装入系统信任：ssh / git 的 HTTPS 走透传隧道（可用但不走镜像加速）。</p>
          <button class="btn sm" :disabled="caInstalling" @click="installCA">
            {{ caInstalling ? '安装中…' : '安装本地 CA 到系统信任' }}
          </button>
        </div>
        <p v-else-if="st.system_proxy_enabled && cert && cert.available && cert.system_trusted" class="notice ok">
          本地 CA 已在系统信任中，git / ssh HTTPS 加速全速生效。
        </p>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>加速方式</h2><span class="muted">可叠加</span></div>
      <div class="card-body">
        <div class="method-item">
          <div class="m-icon">◎</div>
          <div class="m-body">
            <strong>hosts 加速</strong>
            <p>NAS 本机 GitHub 访问全局生效，无需任何软件配置。</p>
          </div>
          <button class="btn sm" @click="goTab('hosts')">去设置</button>
        </div>
        <div class="method-item">
          <div class="m-icon">⬢</div>
          <div class="m-body">
            <strong>Docker 加速</strong>
            <p>docker pull / compose pull 自动走加速器。</p>
          </div>
          <button class="btn sm primary" :disabled="applying" @click="applyDocker">{{ applying ? '应用中…' : '一键应用' }}</button>
        </div>
        <div class="method-item">
          <div class="m-icon">⌘</div>
          <div class="m-body">
            <strong>命令行工具</strong>
            <p>apt / git / pip / npm 等读 http_proxy，开启上方系统代理即可。</p>
          </div>
        </div>
        <div class="method-item">
          <div class="m-icon">⬡</div>
          <div class="m-body">
            <strong>Docker 容器内访问 GitHub</strong>
            <p>开启系统代理后，在飞牛 Docker 界面重启容器继承 http_proxy。</p>
          </div>
        </div>
        <p class="notice ok">不确定选哪个？三个开关全开最稳，覆盖全部场景且互不冲突。</p>
      </div>
    </section>
  </div>
  <p v-else class="loading">正在加载…</p>
</template>

<style scoped>
.status-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 10px; }
.status-item { background: var(--card2); border-radius: var(--r-sm); padding: 11px 12px; display: flex; flex-direction: column; gap: 4px; }
.s-label { font-size: 12px; color: var(--fg2); }
.s-value { font-size: 14px; font-weight: 700; }
.s-value.on { color: var(--green); }
.s-value.off { color: var(--fg3); }
.ca-warn { margin-top: 10px; display: flex; flex-direction: column; gap: 8px; }
.ca-warn p { margin: 0; }
.method-item { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-top: 0.5px solid var(--hairline); }
.method-item:first-child { border-top: 0; padding-top: 0; }
.m-icon { flex: none; width: 38px; height: 38px; display: flex; align-items: center; justify-content: center; font-size: 17px; color: var(--accent); background: var(--accent-soft); border-radius: 11px; }
.m-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.m-body strong { font-size: 13px; }
.m-body p { margin: 0; font-size: 12px; color: var(--fg2); line-height: 1.5; }
.loading { text-align: center; color: var(--fg2); padding: 40px 0; }
</style>
