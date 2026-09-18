import { useEffect, useState } from 'react';

/**
 * 防抖值：输入框每次按键只更新廉价的本地状态，debounced 值在停止输入
 * delayMs 毫秒后才变化 —— 列表/筛选等昂贵计算只依赖它，避免 WebView 里
 * 每个字符都重渲染数百张应用卡片导致输入延迟。
 */
export function useDebouncedValue<T>(value: T, delayMs = 150): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(t);
  }, [value, delayMs]);
  return debounced;
}

/**
 * 键盘布局状态 —— 底部 dock 的"钉在屏幕底边"方案（对齐 iOS App Store 观感）。
 *
 * 返回：
 *  - offsetPx：键盘高度（px）。dock 用 `bottom: -offsetPx` 下移这么多，
 *    即始终钉在**物理屏幕底边**：键盘弹出时 dock 停在屏幕最底、被键盘盖住
 *    （不跟随键盘上移）；键盘收起动画里 dock 保持不动，最后在视口底边
 *    自然露出（和 App Store tab bar 收回键盘时同步现身的观感一致）。
 *    全程不卸载/重挂载 dock —— 没有"弹回来"的位移。
 *  - hidden：仅 pan 型壳（把整个 WebView 平移上移、视口完全不变）的兜底：
 *    此时无法算出键盘高度，聚焦输入框期间直接隐藏 dock，失焦恢复。
 *
 * 键盘高度测量（取两者较大）：
 *  - 视口收缩（adjustResize 型，飞牛 app 等原生壳）：innerHeight 相对基线
 *    的收缩量
 *  - 可视视口差值（浏览器型/iframe 裁剪型）：visualViewport 相对基线的增量
 *
 * 基线维护：
 *  - 视口变高（地址栏收合）→ 抬高基线
 *  - 宽度变化 >80px（旋转屏幕）→ 重建基线
 *  - 键盘收起后若 WebView 没完全回到原高度（壳布局微调），残留收缩稳定
 *    400ms 后把基线校准到"新常态"，防止 dock 被永久压到视口外
 *
 * 壳模式自学习：输入框聚焦时记录视口参照；收缩 >40px ⇒ resize 型
 * （offset 权威）；1.5s 无变化 ⇒ pan 型（聚焦隐/失焦现）；pan 误判后
 * 出现大幅收缩会自动纠正回 resize。
 */
export interface KeyboardDockState {
  /** 键盘高度（px），0 = 键盘未开。dock 以 bottom: -offsetPx 钉在屏幕底边 */
  offsetPx: number;
  /** pan 型壳兜底：聚焦输入框期间整体隐藏 dock */
  hidden: boolean;
}

export function useKeyboardDock(
  deadbandPx = 8,
  keyboardThresholdPx = 220,
  settleMs = 400
): KeyboardDockState {
  const [state, setState] = useState<KeyboardDockState>({ offsetPx: 0, hidden: false });
  useEffect(() => {
    const vv = window.visualViewport;
    // 初始固有差（iframe 高于可视区、地址栏等）
    let vvBaseline = vv ? window.innerHeight - vv.height : 0;
    let baseH = window.innerHeight;
    let baseW = window.innerWidth;
    const isEditable = (el: EventTarget | null) =>
      el instanceof HTMLElement &&
      (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable);
    let focusedEditable = false;
    let shellMode: 'unknown' | 'resize' | 'pan' = 'unknown';
    let modeProbeTimer: ReturnType<typeof setTimeout> | undefined;
    let settleTimer: ReturnType<typeof setTimeout> | undefined;
    let focusRefH = window.innerHeight;
    let focusRefVv = vv ? vv.height : 0;
    let keyboardActive = false; // 视口收缩超过阈值 = 键盘开着

    const setBoth = (offsetPx: number, hidden: boolean) =>
      setState(prev => (prev.offsetPx === offsetPx && prev.hidden === hidden ? prev : { offsetPx, hidden }));

    const measure = () => {
      const h = window.innerHeight;
      const w = window.innerWidth;
      if (Math.abs(w - baseW) > 80) {
        // 旋转/大幅布局变化：重建基线，不当作键盘
        baseH = h;
        baseW = w;
        if (vv) vvBaseline = window.innerHeight - vv.height;
      } else if (h > baseH + 8) {
        baseH = h; // 视口变高（地址栏收合等）→ 抬高基线
      }
      const innerShrink = baseH - h;
      const vvShrink = vv ? window.innerHeight - vv.height - vvBaseline : 0;
      const rawOffset = Math.max(innerShrink, vvShrink);

      // 壳模式自学习：聚焦期间视口相对参照收缩 >40px ⇒ 立即判定 resize 型
      if (shellMode === 'unknown' && focusedEditable) {
        const dh = Math.abs(window.innerHeight - focusRefH);
        const dv = vv ? Math.abs(vv.height - focusRefVv) : 0;
        if (dh > 40 || dv > 40) {
          shellMode = 'resize';
          clearTimeout(modeProbeTimer);
        }
      }

      if (
        shellMode === 'pan' &&
        focusedEditable &&
        rawOffset > keyboardThresholdPx
      ) {
        // 慢弹键盘边界：1.5s 内没等到变化被误判 pan，随后视口才大幅收缩 ——
        // pan 壳的视口从不这样动，纠正为 resize（改走 offset，解除焦点隐藏）
        shellMode = 'resize';
        clearTimeout(modeProbeTimer);
      }

      if (rawOffset > keyboardThresholdPx) {
        // 键盘打开：offset 全程跟踪（dock 钉在屏幕底边、被键盘盖住）
        keyboardActive = true;
        clearTimeout(settleTimer);
      } else if (rawOffset > deadbandPx) {
        // 阈值以下的小残留：键盘已收起（可能 WebView 没回到原高度 / 壳
        // 布局微调）。稳定 settleMs 后把基线校准到新常态，避免 dock 被
        // 永久压到视口外
        if (keyboardActive) keyboardActive = false; // 收起确认
        clearTimeout(settleTimer);
        settleTimer = setTimeout(() => {
          const cur = Math.max(baseH - window.innerHeight, vv ? window.innerHeight - vv.height - vvBaseline : 0);
          if (!keyboardActive && cur > deadbandPx) {
            baseH = window.innerHeight;
            if (vv) vvBaseline = window.innerHeight - vv.height;
            measure();
          }
        }, settleMs);
      } else {
        keyboardActive = false;
        clearTimeout(settleTimer);
      }

      const offsetPx = rawOffset > deadbandPx ? Math.round(rawOffset) : 0;
      const hidden = shellMode === 'pan' && focusedEditable;
      setBoth(offsetPx, hidden);
    };

    const onFocusIn = () => {
      if (!isEditable(document.activeElement)) return;
      focusedEditable = true;
      focusRefH = window.innerHeight;
      focusRefVv = vv ? vv.height : 0;
      // 1.5s 内视口毫无变化 ⇒ pan 型（resize 型由 measure 抢先判定）
      if (shellMode === 'unknown') {
        clearTimeout(modeProbeTimer);
        modeProbeTimer = setTimeout(() => {
          if (shellMode !== 'unknown') return;
          shellMode = 'pan';
          measure();
        }, 1500);
      }
      measure();
    };

    const onFocusOut = () => {
      // focusout 时新焦点可能尚未落定，延迟一拍再判定
      setTimeout(() => {
        if (!isEditable(document.activeElement)) {
          focusedEditable = false;
          measure();
        }
      }, 0);
    };

    measure();
    window.addEventListener('resize', measure);
    vv?.addEventListener('resize', measure);
    document.addEventListener('focusin', onFocusIn);
    document.addEventListener('focusout', onFocusOut);
    return () => {
      clearTimeout(modeProbeTimer);
      clearTimeout(settleTimer);
      window.removeEventListener('resize', measure);
      vv?.removeEventListener('resize', measure);
      document.removeEventListener('focusin', onFocusIn);
      document.removeEventListener('focusout', onFocusOut);
    };
  }, [deadbandPx, keyboardThresholdPx, settleMs]);
  return state;
}
