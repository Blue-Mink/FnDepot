// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  ensureThemeTransitionStyles,
  runThemeToggleTransition,
  toggleThemeWithTransition,
} from './themeTransition';

const STYLE_ID = 'new-store-theme-transition-style';

/** startViewTransition 测试桩的最小签名（真实 API 超集）。 */
type StartVTStub = (
  cb: () => void | Promise<void>,
) => { finished: Promise<void> };

type ThemeSetter = (mode: 'light' | 'dark') => void;

const countStyles = () =>
  document.querySelectorAll(`style#${STYLE_ID}`).length;

/** 把 html 上与本模块相关的所有状态清干净。 */
const cleanDocument = () => {
  document.documentElement.classList.remove('dark');
  delete document.documentElement.dataset.themeTransition;
  delete document.documentElement.dataset.themeTransitioning;
  // 清掉前面测试挂到 document 上的 startViewTransition 桩（jsdom 环境跨用例共享）
  delete (
    document as unknown as { startViewTransition?: StartVTStub }
  ).startViewTransition;
  document
    .querySelectorAll(`style#${STYLE_ID}`)
    .forEach((el) => el.remove());
};

const setStartVT = (fn: StartVTStub) => {
  (
    document as unknown as { startViewTransition?: StartVTStub }
  ).startViewTransition = fn;
};

/** jsdom 无 matchMedia 实现，按查询串返回固定结果。 */
const stubMatchMedia = (reducedMotion = false) => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((q: string) => ({
      matches: reducedMotion && q === '(prefers-reduced-motion: reduce)',
      media: q,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );
};

describe('themeTransition（fn-knock 风格圆形展开切换）', () => {
  beforeEach(() => {
    cleanDocument();
    stubMatchMedia(false);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    cleanDocument();
  });

  it('浏览器无 View Transitions API：直接切换、不留过渡标记', async () => {
    const setTheme = vi.fn<ThemeSetter>();
    await runThemeToggleTransition('dark', () => setTheme('dark'));
    expect(setTheme).toHaveBeenCalledTimes(1);
    expect(setTheme).toHaveBeenCalledWith('dark');
    expect(document.documentElement.dataset.themeTransitioning).toBeUndefined();
    expect(document.documentElement.dataset.themeTransition).toBeUndefined();
  });

  it('用户开启「减少动态效果」：即使有 startViewTransition 也直接切换', async () => {
    stubMatchMedia(true);
    const startVT = vi.fn<StartVTStub>(
      () => ({ finished: Promise.resolve() }),
    );
    setStartVT(startVT);
    const setTheme = vi.fn<ThemeSetter>();
    await runThemeToggleTransition('light', () => setTheme('light'));
    expect(setTheme).toHaveBeenCalledTimes(1);
    expect(startVT).not.toHaveBeenCalled();
  });

  it('View Transition 路径：设置/清理过渡标记，等 <html> 类翻转后收尾，样式只注入一次', async () => {
    let releaseFinished: () => void = () => undefined;
    const startVT = vi.fn<StartVTStub>((cb) => {
      return {
        finished: new Promise<void>((resolve) => {
          releaseFinished = resolve;
          void Promise.resolve(cb()).then(() => {
            // 模拟 next-themes：setTheme 之后异步把 .dark 落到 <html>
            document.documentElement.classList.add('dark');
          });
        }),
      };
    });
    setStartVT(startVT);

    const seenDuringCallback: string[] = [];
    const setTheme = vi.fn<ThemeSetter>(() => {
      seenDuringCallback.push(
        String(document.documentElement.dataset.themeTransition),
        String(document.documentElement.dataset.themeTransitioning),
      );
    });

    const done = runThemeToggleTransition('dark', () => setTheme('dark'));
    // 手动放行走完整流程
    releaseFinished();
    await done;

    expect(startVT).toHaveBeenCalledTimes(1);
    expect(setTheme).toHaveBeenCalledTimes(1);
    expect(seenDuringCallback).toEqual(['to-dark', '']);
    // 收尾后标记全部清除
    expect(document.documentElement.dataset.themeTransition).toBeUndefined();
    expect(document.documentElement.dataset.themeTransitioning).toBeUndefined();
    // 过渡样式已注入，且幂等
    expect(countStyles()).toBe(1);
    ensureThemeTransitionStyles();
    expect(countStyles()).toBe(1);
    // 时长策略：桌面 1s；移动端（窄屏/触屏）0.45s
    const styleText = document
      .querySelector(`style#${STYLE_ID}`)
      ?.textContent ?? '';
    expect(styleText).toContain('--new-store-theme-transition-duration: 1s');
    expect(styleText).toContain('(max-width: 768px), (pointer: coarse)');
    expect(styleText).toContain('--new-store-theme-transition-duration: 0.45s');
  });

  it('callback 会等到 <html> 类翻转（finished 依赖 callback，模拟真实 API 时序）', async () => {
    // 当前 dark（.dark 在 <html> 上），目标 light
    document.documentElement.classList.add('dark');
    const startVT = vi.fn<StartVTStub>((cb) => ({
      // 真实浏览器：callback 解析（含 React 重渲染落类）后才拍新快照、播动画
      finished: Promise.resolve(cb()).then(
        () => new Promise<void>((r) => setTimeout(r, 10)),
      ),
    }));
    setStartVT(startVT);

    // 模拟 next-themes：setTheme 调用后 ~60ms 才把 .dark 移除
    const setTheme = vi.fn<ThemeSetter>(() => {
      setTimeout(() => document.documentElement.classList.remove('dark'), 60);
    });

    const t0 = Date.now();
    await runThemeToggleTransition('light', () => setTheme('light'));
    // 若没等类翻转，总耗时 ≈ 10ms（只有动画段）；等了则 ≈ 70ms
    expect(Date.now() - t0).toBeGreaterThanOrEqual(60);
    expect(setTheme).toHaveBeenCalledTimes(1);
  });

  it('过渡进行中连点：第二次点击被忽略，只切换一次', async () => {
    let releaseFinished: () => void = () => undefined;
    const startVT = vi.fn<StartVTStub>((cb) => {
      return {
        finished: new Promise<void>((resolve) => {
          releaseFinished = resolve;
          void Promise.resolve(cb()).then(() => {
            document.documentElement.classList.add('dark');
          });
        }),
      };
    });
    setStartVT(startVT);

    const setTheme = vi.fn<ThemeSetter>();
    const first = toggleThemeWithTransition('light', setTheme);
    const second = toggleThemeWithTransition('light', setTheme);
    expect(first).toBeDefined();
    expect(second).toBe(first); // 复用进行中那次

    releaseFinished();
    await first;
    expect(setTheme).toHaveBeenCalledTimes(1);
    expect(setTheme).toHaveBeenCalledWith('dark');
  });

  it('当前为 dark 时切换目标是 light', async () => {
    const setTheme = vi.fn<ThemeSetter>();
    const p = toggleThemeWithTransition('dark', setTheme);
    await p;
    expect(setTheme).toHaveBeenCalledTimes(1);
    expect(setTheme).toHaveBeenCalledWith('light');
  });
});
