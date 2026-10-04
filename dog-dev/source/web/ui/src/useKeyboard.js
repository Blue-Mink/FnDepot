/* 共享键盘高度跟踪（移动端软键盘）。
 *
 * 背景：Dock 与 Sheet 都是 position:fixed 锚在布局视口底部。软键盘呼出时
 * 可视视口（visualViewport）收缩，但 fixed 元素不会自动上移——Dock 会压在
 * 输入框上、Sheet 里的输入框被键盘盖住（用户反馈的「折叠输入框」）。
 *
 * 做法：监听 visualViewport 的 resize，键盘高 = innerHeight - vv.height，
 * 导出为模块级共享 ref，TabBar（Dock）与 Sheet 各自抬升对应高度。
 *
 * 阈值 120px：浏览器地址栏收起/展开只引起 ≤60px 的可视视口变化，排除误报；
 * 真实软键盘普遍 ≥250px。桌面端无软键盘，恒为 0，行为不变。
 *
 * 兼容：个别 WebView 键盘呼出会连布局视口一起缩小（innerHeight 同步减小），
 * 此时差值为 0——那种情况下 fixed 底部本来就在键盘上方，不需要抬升，语义正确。
 */
import { ref } from 'vue'

const kb = ref(0)
let started = false

function calc() {
  const vv = window.visualViewport
  if (!vv || !vv.height) { kb.value = 0; return }
  const h = window.innerHeight - vv.height
  kb.value = h >= 120 ? Math.round(h) : 0
}

export function useKeyboardInset() {
  if (!started && typeof window !== 'undefined' && window.visualViewport) {
    started = true
    window.visualViewport.addEventListener('resize', calc)
    window.addEventListener('resize', calc)
    window.addEventListener('orientationchange', () => setTimeout(calc, 150))
  }
  return kb
}
