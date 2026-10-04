/* 复制文本：优先剪贴板 API（安全上下文），http 非安全上下文回落 execCommand */
import { useMessage } from 'naive-ui'

export function useCopy() {
  const message = useMessage()
  return async function copyText(text) {
    if (!text) return
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text)
        message.success('已复制')
        return
      }
    } catch { /* 落到 fallback */ }
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.cssText = 'position:fixed;opacity:0'
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy'); message.success('已复制') }
    catch { message.error('复制失败，请手动选择复制') }
    ta.remove()
  }
}
