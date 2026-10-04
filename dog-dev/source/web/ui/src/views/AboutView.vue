<script setup>
/* 关于 v2：信息行 + 安全承诺 + 开源致谢（紧凑） */
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'

const about = ref(null)

async function load() {
  try { about.value = await api('/api/about') } catch { /* 401 由 App 处理 */ }
}
onMounted(load)

const info = computed(() => about.value ? [
  ['版本', about.value.version],
  ['作者', about.value.author],
  ['开发语言', about.value.language + '（' + about.value.go_version + '）'],
  ['开源协议', about.value.license],
] : [])
</script>

<template>
  <div v-if="about">
    <section class="card about-hero">
      <!-- 品牌规范：≤64px 用实色版（48px 下反白版负形糊成白团） -->
      <img src="/logo-solid.svg" alt="" class="about-logo">
      <h2 class="about-name">{{ about.name }}</h2>
      <p class="about-desc">{{ about.description }}</p>
      <div class="kv-list">
        <div class="kv-row" v-for="row in info" :key="row[0]">
          <span class="k">{{ row[0] }}</span>
          <span class="v mono">{{ row[1] }}</span>
        </div>
        <div class="kv-row" v-if="about.publisher_url">
          <span class="k">发布者</span>
          <span class="v"><a :href="about.publisher_url" target="_blank" rel="noopener">{{ about.publisher }}</a></span>
        </div>
        <div class="kv-row">
          <span class="k">仓库</span>
          <span class="v"><a :href="about.github" target="_blank" rel="noopener">GitHub</a> ·
            <a :href="about.repo" target="_blank" rel="noopener">源码</a></span>
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
.kv-row .v a { color: var(--accent); text-decoration: none; }
.safety-item { padding: 8px 0; border-top: 0.5px solid var(--hairline); }
.safety-item:first-child { border-top: 0; padding-top: 0; }
.safety-item strong { font-size: 13px; display: block; margin-bottom: 3px; }
.safety-item p { margin: 0; font-size: 12px; color: var(--fg2); line-height: 1.6; }
.refs { gap: 0; }
.ref-name { color: var(--accent); text-decoration: none; font-weight: 600; font-size: 13px; flex: none; max-width: 45%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.loading { text-align: center; color: var(--fg2); padding: 40px 0; }
</style>
