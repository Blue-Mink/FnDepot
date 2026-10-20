<script setup>
/* 日志页：级别过滤 + 行数选择 + 实时跟随 + 复制 + 清屏；日志主体由 LogStream 负责。
 * 1.3.0：最新日志在最上面；新增行数选项与「复制」按钮。 */
import { ref } from 'vue'
import { useMessage } from 'naive-ui'
import LogStream from '../components/LogStream.vue'
import { useCopy } from '../useCopy'

const message = useMessage()
const copyText = useCopy()

const stream = ref(null)
const level = ref('')
const follow = ref(true)
const limit = ref(300)

const LEVELS = [['', '全部'], ['info', 'info'], ['warn', 'warn'], ['error', 'error'], ['debug', 'debug']]
const LINES = [100, 200, 300, 500, 1000, 2000]

function copyAll() {
  const t = stream.value && stream.value.asText ? stream.value.asText() : ''
  if (!t) { message.warning('当前没有可复制的日志'); return }
  copyText(t)
}
</script>

<template>
  <section class="card">
    <div class="card-head">
      <h2>运行日志</h2>
      <div class="head-actions">
        <button class="btn sm" @click="copyAll">复制</button>
        <button class="btn sm" @click="stream && stream.clear()">清屏</button>
      </div>
    </div>
    <div class="card-body">
      <div class="seg lv-seg">
        <button v-for="[v, l] in LEVELS" :key="v" :class="{ on: level === v }" @click="level = v">{{ l }}</button>
      </div>
      <div class="log-ctl">
        <label class="lines-pick">
          <span>行数</span>
          <select v-model.number="limit">
            <option v-for="n in LINES" :key="n" :value="n">{{ n }}</option>
          </select>
        </label>
        <label class="sw-follow">
          <span class="sw"><input type="checkbox" v-model="follow"><i></i></span>
          <span>跟随最新</span>
        </label>
      </div>
      <LogStream ref="stream" :level="level" :follow="follow" :limit="limit" />
    </div>
  </section>
</template>

<style scoped>
.head-actions { display: flex; align-items: center; gap: 6px; }
.lv-seg { width: 100%; }
.log-ctl { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.lines-pick { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--fg2); }
.lines-pick select {
  border: 0.5px solid var(--line); background: var(--card2); color: var(--fg);
  border-radius: 9px; padding: 6px 8px; font-size: 12px; outline: none;
}
.sw-follow { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--fg2); flex: none; }
</style>
