<script setup>
/* 日志页：级别过滤 + 实时跟随 + 清屏；日志主体由 LogStream 负责 */
import { ref } from 'vue'
import LogStream from '../components/LogStream.vue'

const stream = ref(null)
const level = ref('')
const follow = ref(true)

const LEVELS = [['', '全部'], ['info', 'info'], ['warn', 'warn'], ['error', 'error'], ['debug', 'debug']]
</script>

<template>
  <section class="card">
    <div class="card-head">
      <h2>运行日志</h2>
      <button class="btn sm" @click="stream && stream.clear()">清屏</button>
    </div>
    <div class="card-body">
      <div class="log-ctl">
        <div class="seg lv-seg">
          <button v-for="[v, l] in LEVELS" :key="v" :class="{ on: level === v }" @click="level = v">{{ l }}</button>
        </div>
        <label class="sw-follow">
          <span class="sw"><input type="checkbox" v-model="follow"><i></i></span>
          <span>跟随</span>
        </label>
      </div>
      <LogStream ref="stream" :level="level" :follow="follow" />
    </div>
  </section>
</template>

<style scoped>
.log-ctl { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.lv-seg { flex: 1; max-width: 380px; }
.sw-follow { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--fg2); flex: none; }
</style>
