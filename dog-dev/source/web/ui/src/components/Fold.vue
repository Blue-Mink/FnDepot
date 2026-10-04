<script setup>
/* 折叠 / 展开：标题行（标题 + 可选计数 + 右侧 meta + 箭头）+ 内容体。
 * 用法：
 *   <Fold v-model:open="x" title="引擎节点" :count="n">…内容…</Fold>   卡片内小节
 *   <Fold head v-model:open="x" title="…">…</Fold>                     整卡折叠（标题占 card-head 位） */
const props = defineProps({
  title: { type: String, required: true },
  count: { type: [String, Number], default: '' },
  open: { type: Boolean, default: false },
  head: { type: Boolean, default: false },
})
const emit = defineEmits(['update:open'])
function toggle() { emit('update:open', !props.open) }
</script>

<template>
  <div class="fold" :class="{ head }">
    <button type="button" class="fold-head" :class="{ open }" :aria-expanded="open" @click="toggle">
      <span class="fold-title">{{ title }}<span v-if="count !== ''" class="fold-count">（{{ count }}）</span></span>
      <span class="fold-meta"><slot name="meta" /></span>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>
    </button>
    <div v-show="open" class="fold-body"><slot /></div>
  </div>
</template>
