<script setup>
/* 关于 v2：信息行 + 安全承诺 + 开源致谢（紧凑）。
 * 1.3.0：作者 → 开发者（可跳转项目主页）；发布者与开源协议互换位置；去掉「仓库」行。 */
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { appURL } from '../base'

// 品牌规范：≤64px 用实色版（48px 下反白版负形糊成白团）。
const logoURL = appURL('/logo-solid.svg')

const about = ref(null)

async function load() {
  try { about.value = await api('/api/about') } catch { /* 后端未就绪时静默 */ }
}
onMounted(load)

/* 信息行（顺序即展示顺序）：
 * 版本 → 开发者 → 开发语言 → 发布者 → 开源协议
 * href 存在时值渲染为可点击链接。 */
const rows = computed(() => {
  const a = about.value
  if (!a) return []
  return [
    { k: '版本', v: a.version, mono: true },
    { k: '开发者', v: a.author, href: a.author_url },
    { k: '开发语言', v: a.language + '（' + a.go_version + '）', mono: true },
    { k: '发布者', v: a.publisher, href: a.publisher_url },
    { k: '开源协议', v: a.license, mono: true },
  ]
})
</script>

<template>
  <div v-if="about">
    <section class="card about-hero">
      <!-- 品牌规范：≤64px 用实色版（48px 下反白版负形糊成白团） -->
      <img :src="logoURL" alt="" class="about-logo">
      <h2 class="about-name">{{ about.name }}</h2>
      <p class="about-desc">{{ about.description }}</p>
      <div class="kv-list">
        <div class="kv-row" v-for="row in rows" :key="row.k">
          <span class="k">{{ row.k }}</span>
          <a v-if="row.href" class="v link" :href="row.href" target="_blank" rel="noopener">{{ row.v }}</a>
          <span v-else class="v" :class="{ mono: row.mono }">{{ row.v }}</span>
        </div>
      </div>
    </section>

    <section v-if="about.safety && about.safety.length" class="card">
      <div class="card-head"><h2>网络安全承诺</h2></div>
      <div class="card-body">
        <div v-for="(s, i) in about.safety" :key="i" class="safety-item">
          <strong>{{ s.point }}</strong>
          <p>{{ s.detail }}</p>
        </div>
      </div>
    </section>

    <section v-if="about.references && about.references.length" class="card">
      <div class="card-head"><h2>开源致谢</h2></div>
      <div class="card-body refs">
        <div v-for="r in about.references" :key="r.url" class="lrow">
          <a class="ref-name" :href="r.url" target="_blank" rel="noopener">{{ r.name }}</a>
          <span class="lrow-s">{{ r.note }}</span>
        </div>
      </div>
    </section>
  </div>
  <p v-else class="loading">正在加载…</p>
</template>

<style scoped>
.about-hero { display: flex; flex-direction: column; align-items: flex-start; padding: 20px 16px 16px; gap: 6px; }
/* 品牌标自由轮廓，不加 border-radius（规范禁止裁切） */
.about-logo { width: 48px; height: 48px; }
.about-name { margin: 6px 0 0; font-size: 18px; font-weight: 700; }
.about-desc { margin: 0; font-size: 12px; color: var(--fg2); line-height: 1.6; }
.kv-list { margin-top: 10px; width: 100%; }
.kv-row { display: flex; justify-content: space-between; gap: 12px; padding: 7px 0; border-top: 0.5px solid var(--hairline); font-size: 13px; }
.kv-row .k { color: var(--fg2); flex: none; }
.kv-row .v { word-break: break-all; text-align: right; }
.mono { font-family: ui-monospace, Menlo, monospace; font-size: 12px; }
.kv-row .v.link { color: var(--accent); text-decoration: none; }
.kv-row .v.link:active { opacity: 0.6; }
.safety-item { padding: 8px 0; border-top: 0.5px solid var(--hairline); }
.safety-item:first-child { border-top: 0; padding-top: 0; }
.safety-item strong { font-size: 13px; display: block; margin-bottom: 3px; }
.safety-item p { margin: 0; font-size: 12px; color: var(--fg2); line-height: 1.6; }
.refs { gap: 0; }
.ref-name { color: var(--accent); text-decoration: none; font-weight: 600; font-size: 13px; flex: none; max-width: 45%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.loading { text-align: center; color: var(--fg2); padding: 40px 0; }
</style>
