<script setup>
defineProps({
  label: { type: String, required: true },
  value: { type: String, required: true },
  sub: { type: String, default: '' },
})
</script>

<template>
  <div class="metric">
    <div class="label">{{ label }}</div>
    <div class="value">{{ value }}</div>
    <div v-if="sub" class="sub">{{ sub }}</div>
  </div>
</template>

<style scoped>
.metric {
  background: var(--card); border-radius: var(--r);
  box-shadow: var(--shadow); padding: 12px 14px;
  display: flex; flex-direction: column; gap: 3px;
}
.label { font-size: 12px; color: var(--fg2); }
.value { font-size: 20px; font-weight: 700; letter-spacing: -0.2px; }
.sub { font-size: 11px; color: var(--fg3); }
</style>
