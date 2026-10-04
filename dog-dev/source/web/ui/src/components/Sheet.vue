<script setup>
/* 统一弹层：移动端 = iOS 底部抽屉（圆角顶 + 拖拽把手），桌面（≥700px）= 居中卡片。
 * 用法：<Sheet :show="x" title="…" @close="x = false">…</Sheet> */
import { useKeyboardInset } from '../useKeyboard'

defineProps({
  show: { type: Boolean, default: false },
  title: { type: String, default: '' },
})
const emit = defineEmits(['close'])

/* 软键盘呼出时，遮罩底部让出键盘高度 → 抽屉整体上移到键盘上方，
 * 自定义输入等框体不再被键盘盖住（fixed 元素不随键盘自动上移）。 */
const kb = useKeyboardInset()
const maskStyle = () => (kb.value ? { paddingBottom: kb.value + 'px' } : {})
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="sheet-mask" :style="maskStyle()" @click.self="emit('close')">
      <div class="sheet" role="dialog" aria-modal="true">
        <div class="sheet-handle"></div>
        <div class="sheet-head">
          <h3>{{ title }}</h3>
          <button class="sheet-x" aria-label="关闭" @click="emit('close')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>
          </button>
        </div>
        <div class="sheet-body"><slot /></div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.sheet-mask {
  position: fixed; inset: 0; z-index: 80;
  background: rgba(0, 0, 0, 0.35);
  backdrop-filter: blur(3px); -webkit-backdrop-filter: blur(3px);
  display: flex; align-items: flex-end; justify-content: center;
}
.sheet {
  width: 100%; max-width: 560px; background: var(--card);
  border-radius: 20px 20px 0 0;
  max-height: 88dvh; display: flex; flex-direction: column;
  animation: sheet-up 0.32s cubic-bezier(0.32, 0.72, 0, 1);
}
@keyframes sheet-up { from { transform: translateY(60px); opacity: 0.4; } to { transform: none; opacity: 1; } }
.sheet-handle { width: 36px; height: 5px; border-radius: 3px; background: var(--line); margin: 8px auto 0; flex: none; }
.sheet-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 16px 8px; flex: none; }
.sheet-head h3 { margin: 0; font-size: 15px; font-weight: 700; }
.sheet-x {
  border: 0; background: var(--card2); color: var(--fg2);
  width: 28px; height: 28px; border-radius: 50%; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
}
.sheet-x svg { width: 14px; height: 14px; }
.sheet-body { padding: 8px 16px calc(22px + env(safe-area-inset-bottom)); overflow-y: auto; }
@media (min-width: 700px) {
  .sheet-mask { align-items: center; padding: 20px; }
  .sheet { max-width: 420px; border-radius: 20px; animation: sheet-pop 0.24s cubic-bezier(0.32, 0.72, 0, 1); }
  @keyframes sheet-pop { from { transform: scale(0.96) translateY(8px); opacity: 0; } to { transform: none; opacity: 1; } }
  .sheet-handle { display: none; }
  .sheet-body { padding-bottom: 20px; }
}
</style>
