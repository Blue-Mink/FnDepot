import React, { useState, useEffect, useCallback, useRef, useMemo, Suspense } from 'react';
import { useDebouncedValue, useKeyboardDock } from './lib/hooks';
import { LayoutGrid, CheckCircle2, RefreshCw, Settings, MessageCircle, ChevronsLeft, ChevronsRight, Search, X, Film, ArrowDownToLine, BookOpen, Wrench, Globe, Loader2, CircleX, CircleCheck, WifiOff, ExternalLink, Compass, Brain, Clapperboard, Network, ChevronsUpDown, Check, ChevronDown } from 'lucide-react';
import { Button } from './components/ui/button';
import { Input } from './components/ui/input';
import { Badge } from './components/ui/badge';
import AppList from './components/AppList';
import AppIcon from './components/AppIcon';
import ProgressOverlay from './components/ProgressOverlay';
import BackgroundTasksIndicator from './components/BackgroundTasksIndicator';
import RecommendedAppCard from './components/RecommendedAppCard';
import FeaturedShowcase from './components/FeaturedShowcase';
// 重型对话框懒加载：首屏 bundle 只保留列表/导航核心，设置页(1089行)/详情
// (975行，含 SourceManager)/向导/面板安装/失败报告按需下载（LAN 内瞬时），
// 首屏 JS 解析编译时间减半（对齐参照实现的轻量首屏）。
const AppDetailDialog = React.lazy(() => import('./components/AppDetailDialog'));
const SettingsPage = React.lazy(() => import('./components/SettingsPage'));
const WizardDialog = React.lazy(() => import('./components/WizardDialog'));
import ThemeToggle from './components/ThemeToggle';
import MobileDock from './components/MobileDock';
import AppRowList from './components/AppRowList';
import { fetchApps, triggerCheck, installApp, updateApp, uninstallApp, fetchStatus, fetchStoreUpdate, triggerStoreUpdate, reloadApps, ignoreUpdate, unignoreUpdate, fetchRecommended, fetchWizard, controlApp, appWebUrl, fetchPanelDetail, sourceLabel, effectiveMaintainer } from './api/client';
import { connectFnOSBridge, openAppInShell } from './lib/fnos-bridge';
import { alphaInitial } from './lib/pinyin';
import type { AppInfo, AppOperation, SSECallback, RecommendedApp, AppWizard, WizardParam, PanelDetailResponse, PanelInstallParams } from './api/client';
import { toast } from "sonner"
import { Toaster } from "@/components/ui/sonner"
// 重型对话框懒加载（见上方说明）：按需分包，首屏只加载列表/导航核心。
const PanelInstallDialog = React.lazy(() => import('./components/PanelInstallDialog'));
const ReportFailureDialog = React.lazy(() => import('./components/ReportFailureDialog').then(m => ({ default: m.ReportFailureDialog })));
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

const CATEGORIES = [
  { key: 'ai', label: 'AI', icon: Brain },
  { key: 'media', label: '媒体服务', icon: Film },
  { key: 'automation', label: '媒体自动化', icon: Clapperboard },
  { key: 'download', label: '下载传输', icon: ArrowDownToLine },
  { key: 'content', label: '内容管理', icon: BookOpen },
  { key: 'network', label: '网络工具', icon: Network },
  { key: 'system', label: '系统工具', icon: Wrench },
  { key: 'browser', label: '浏览器', icon: Globe },
] as const;

type CategoryKey = typeof CATEGORIES[number]['key'];

type SortKey = 'default' | 'downloads' | 'name' | 'alpha' | 'updated';

const SORT_OPTIONS: { value: SortKey; label: string }[] = [
  { value: 'default', label: '随机' },
  { value: 'alpha', label: 'A-Z' },
  { value: 'downloads', label: '下载量' },
  { value: 'name', label: '名称' },
  { value: 'updated', label: '最近更新' },
];

const App: React.FC = () => {
  const [apps, setApps] = useState<AppInfo[]>([]);
  // false on fnOS builds whose update path destroys the app (see backend
  // platform.UpgradeCapability). Surfaced so the UI does not offer an update
  // button that can only ever fail.
  const [upgradeAllowed, setUpgradeAllowed] = useState(true);
  const [loadStatus, setLoadStatus] = useState<'loading' | 'loaded' | 'retrying' | 'failed'>('loading');
  const [loadMessages, setLoadMessages] = useState<{text: string; status: 'info' | 'success' | 'error'}[]>([]);
  const [checking, setChecking] = useState<boolean>(false);
  const [lastCheck, setLastCheck] = useState<string>('');
  const [reportTarget, setReportTarget] = useState<{ app: string; step: string; error: string } | null>(null);

  const [appOperations, setAppOperations] = useState<Map<string, AppOperation>>(new Map());
  const [selfUpdateActive, setSelfUpdateActive] = useState(false);
  const [selfUpdateState, setSelfUpdateState] = useState<{message: string; progress: number; speed?: number; downloaded?: number; total?: number} | null>(null);
  // selfUpdateActiveRef tracks whether the self-update OVERLAY should be shown.
  // It can be set optimistically (handleStoreUpdate sets it BEFORE the SSE opens).
  // selfUpdateRestartSeenRef tracks whether the backend has actually emitted the
  // 'self_update' SSE event - meaning the server is committed to killing itself
  // and a subsequent connection drop is EXPECTED, not a failure.
  // Catch blocks must read selfUpdateRestartSeenRef (not selfUpdateActiveRef)
  // when deciding whether to suppress the error toast, otherwise pre-SSE failures
  // (HTTP 409, network errors) get silently swallowed.
  const selfUpdateActiveRef = useRef(false);
  const selfUpdateRestartSeenRef = useRef(false);
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reloadHandleRef = useRef<{ cancel: () => void } | null>(null);
  const appOperationsRef = useRef<Map<string, AppOperation>>(new Map());
  // Keep ref synced with state so guards in event handlers see current value
  // without forcing them to depend on appOperations (which would re-create
  // them on every progress update and trigger child re-renders).
  appOperationsRef.current = appOperations;

  const [settingsVisible, setSettingsVisible] = useState(false);
  const [storeHasUpdate, setStoreHasUpdate] = useState(false);
  const [activeFilter, setActiveFilter] = useState<'all' | 'installed' | 'update_available' | 'recommended'>('all');
  const [recommendedApps, setRecommendedApps] = useState<RecommendedApp[]>([]);
  const [searchInput, setSearchInput] = useState('');
  const [searchExpanded, setSearchExpanded] = useState(false);
  const searchInputRef = useRef<HTMLInputElement>(null);
  // 收起 = 立即（无过渡，1.14.24 用户要求恢复原来的生硬但干脆的行为）。
  // 收起触发源：Enter / 点外部(blur) / × 清除 / 收起按钮 / 键盘收起（无论有无输入，
  // 有输入时搜索词保留、列表保持过滤）。
  // 注意：**不做**输入停顿自动收起 —— 中文输入停顿思考时会被误收（1.14.23 修复）。
  const collapseSearch = () => {
    setSearchExpanded(false);
    searchInputRef.current?.blur();
  };
  const expandSearch = () => {
    setSearchExpanded(true);
  };
  // 搜索防抖：按键即时更新输入框（廉价），筛选/列表只依赖 150ms 后的
  // searchQuery —— WebView 里打字不再每个字符都重渲染数百张卡片。
  const searchQuery = useDebouncedValue(searchInput, 150);
  // 底部 dock 键盘处理（钉在屏幕底边方案，对齐 iOS App Store 观感）：
  //   offsetPx = 键盘高度 → dock 用 bottom:-offset 下移，键盘弹出时 dock
  //   停在物理屏幕底边被键盘盖住（不跟键盘上移、不重挂载、无回弹位移），
  //   收起时已就位于视口底边；hidden 仅 pan 型壳（整个 WebView 被平移、
  //   视口不变）兜底用 —— 聚焦输入框期间整体隐藏。
  // 兼容：浏览器型（visualViewport 差值）、飞牛 app 等 adjustResize
  // WebView（innerHeight 收缩）、iframe 裁剪型。
  const { offsetPx: dockOffsetPx, hidden: dockHidden } = useKeyboardDock();
  // 键盘收起自动收搜索框：展开态下键盘从弹出到收起（offsetPx 开→关沿）即收起 ——
  // 飞牛 app 点 IME 完成/收起按钮不触发 blur，需键盘收起信号兜底。
  // 无论有无输入：空=直接收起；有输入=搜索词保留、列表保持过滤。
  const prevKbOpenRef = useRef(false);
  useEffect(() => {
    const open = dockOffsetPx > 0;
    if (prevKbOpenRef.current && !open && searchExpanded) {
      collapseSearch();
    }
    prevKbOpenRef.current = open;
  }, [dockOffsetPx, searchExpanded]);
  // 源 / 开发者 / 发布者 多选筛选：点徽章 = 把词条跳进搜索框（与原行为一致），
  // 多个徽章词条在搜索框内叠加（空格分隔、AND 组合）；再点同一徽章移除该词条；
  // 搜索框后的 × 一次性清空全部词条。无独立筛选 chip 行。
  const applyTextFilter = (term: string) => {
    const t = (term || '').trim();
    if (!t) return;
    if (activeFilter === 'recommended') switchFilter('all');
    setSearchInput(prev => {
      const terms = prev.split(/\s+/).filter(Boolean);
      if (terms.includes(t)) return terms.filter(x => x !== t).join(' ');
      return terms.length ? `${terms.join(' ')} ${t}` : t;
    });
  };
  // 搜索框内当前生效的徽章/搜索词条（徽章选中态 + 计数联动用）
  const activeSearchTerms = useMemo(
    () => searchInput.trim().split(/\s+/).filter(Boolean),
    [searchInput]
  );
  const [activeCategory, setActiveCategory] = useState<CategoryKey | null>(null);
  const [pendingUninstallApp, setPendingUninstallApp] = useState<AppInfo | null>(null);
  // Apps can declare an install-time form (fnos/wizard/install). When one
  // exists we ask first, then install with the answers — matching what the
  // native App Center does. Previously the store silently accepted defaults,
  // so an app needing a token or password came up misconfigured.
  const [wizardApp, setWizardApp] = useState<AppInfo | null>(null);
  const [wizardDef, setWizardDef] = useState<AppWizard | null>(null);
  const [wizardLoading, setWizardLoading] = useState(false);
  // 官方通道：用户在体积/依赖弹窗里确认的参数，向导弹窗确认后随安装一起提交
  // （FPK 通道恒为 null）。
  const [panelWizardParams, setPanelWizardParams] = useState<PanelInstallParams | null>(null);
  // 官方应用中心（fnos-official）安装：详情+依赖弹窗
  const [panelApp, setPanelApp] = useState<AppInfo | null>(null);
  const [panelDetail, setPanelDetail] = useState<PanelDetailResponse | null>(null);
  const [panelLoading, setPanelLoading] = useState(false);
  const [detailApp, setDetailApp] = useState<AppInfo | null>(null);
  const [successInfo, setSuccessInfo] = useState<{app: AppInfo; operation: 'install' | 'update'} | null>(null);
  // App Store large title：内容滚动后标题收缩、头部转毛玻璃
  const [mainScrolled, setMainScrolled] = useState(false);
  const [sortBy, setSortBy] = useState<SortKey>('default');
  // 「全部」视图随机展示：每次数据加载后重新洗牌（不再按 fnos-apps 等来源分组置顶），
  // 同一次会话内顺序稳定（切 tab 返回不重洗，记住的滚动位置仍落在同一应用上）。
  const [shuffleTick, setShuffleTick] = useState(0);
  const [sortMenuOpen, setSortMenuOpen] = useState(false);
  // 发现页「探索推荐」区：默认折叠，点「展开」显示推荐网格
  const [exploreExpanded, setExploreExpanded] = useState(false);
  // 排序菜单位置：pill 行是 overflow-x-auto 滚动容器（会同时裁剪 y 轴），
  // 菜单必须 fixed 定位逃出裁剪，坐标在打开时按触发钮实测位置计算。
  const [sortMenuPos, setSortMenuPos] = useState<{ left: number; top: number } | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() =>
    localStorage.getItem('sidebar-collapsed') === 'true'
  );

  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed(prev => {
      const next = !prev;
      localStorage.setItem('sidebar-collapsed', String(next));
      return next;
    });
  }, []);

  useEffect(() => {
    loadApps();
    fetchStoreUpdate().then(info => setStoreHasUpdate(info.has_update)).catch(() => {});
    fetchRecommended().then(data => setRecommendedApps(data.apps)).catch(() => {});
    // 预握手 fnOS Web UI 壳窗口（postmate 子端协议）：内嵌时"应用设置"
    // 才能秒开「设置→应用」面板；独立打开时静默失败无副作用。
    connectFnOSBridge().catch(() => {});
    // large title 收缩：滚动容器是 window（实测 main 的 overflow-y-auto
    // 不生效，整页随窗口滚动、sticky 头部吸顶），监听 window scroll
    const onScroll = () => setMainScrolled(window.scrollY > 24);
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => {
      window.removeEventListener('scroll', onScroll);
      // Cleanup on unmount: cancel pending poll timer and any in-flight reload SSE
      if (pollTimerRef.current) {
        clearTimeout(pollTimerRef.current);
        pollTimerRef.current = null;
      }
      reloadHandleRef.current?.cancel();
      reloadHandleRef.current = null;
    };
  }, []);

  // 底部 dock / 侧栏切换 tab：记住每个 tab 的滚动位置，切回时恢复
  //（实际滚动容器是 window —— 实测 main 的 overflow-y-auto 不生效，整页随窗口滚动）
  const scrollPosRef = useRef<Record<string, number>>({});
  const activeFilterRef = useRef(activeFilter);
  // 持续记录当前 tab 的滚动位置。不能在切换后的 effect 里读 window.scrollY：
  // React 提交新列表后文档高度突变，浏览器会先把 scrollY 钳制到新列表的
  // 最大值，effect 再读就已经是被钳制过的错误值（深滚动切短列表必丢位置）。
  useEffect(() => {
    const onScroll = () => { scrollPosRef.current[activeFilterRef.current] = window.scrollY; };
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener('scroll', onScroll);
  }, []);
  // tab 切换（dock / 侧栏 / 返回按钮 / 搜索跳转统一走这里）：
  // 点击时 DOM 还没换、浏览器也还没钳制 scrollY，此刻保存的才是旧 tab 的真实
  // 位置；并同步把 activeFilterRef 指向新 tab，使 DOM 切换期间浏览器因文档
  // 高度突变发出的 scroll 事件记到新 tab 名下，不会覆盖旧 tab 的位置。
  const switchFilter = useCallback((next: 'all' | 'installed' | 'update_available' | 'recommended') => {
    const prev = activeFilterRef.current;
    scrollPosRef.current[prev] = window.scrollY;
    activeFilterRef.current = next;
    setActiveFilter(next);
    setActiveCategory(null);
  }, []);
  // 切回某 tab 时恢复其滚动位置：等新列表提交布局；目标超出当前文档高度时
  // 逐帧重试（列表还在渲染），最多 10 帧后钳制到当前最大值兜底。
  useEffect(() => {
    const target = scrollPosRef.current[activeFilter] ?? 0;
    let tries = 0;
    let raf = 0;
    const attempt = () => {
      const max = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
      if (target <= max) { window.scrollTo(0, target); return; }
      if (tries++ < 10) { raf = requestAnimationFrame(attempt); }
      else { window.scrollTo(0, max); }
    };
    requestAnimationFrame(() => requestAnimationFrame(attempt));
    return () => cancelAnimationFrame(raf);
  }, [activeFilter]);

  const setAppOp = useCallback((appname: string, op: AppOperation | null) => {
    setAppOperations(prev => {
      const next = new Map(prev);
      if (op === null) {
        next.delete(appname);
      } else {
        next.set(appname, op);
      }
      return next;
    });
  }, []);

  const pollForRestart = useCallback(() => {
    // Dedup: createSSEHandler and handleStoreUpdate may both call this.
    if (pollTimerRef.current) return;

    let retries = 0;
    const poll = async () => {
      pollTimerRef.current = null;
      try {
        await fetchStatus();
        window.location.reload();
      } catch {
        retries++;
        if (retries > 30) {
          setSelfUpdateState({ message: '重启超时，请手动刷新页面', progress: 100 });
          return;
        }
        setSelfUpdateState({ message: '正在重启...', progress: 100 });
        pollTimerRef.current = setTimeout(poll, 2000);
      }
    };
    pollTimerRef.current = setTimeout(poll, 2000);
  }, []);

  const translateStep = (step?: string) => {
      switch(step) {
          case 'downloading': return '正在下载...';
          case 'pulling': return '正在拉取镜像...';
          case 'installing': return '正在安装...';
          case 'verifying': return '正在验证...';
          case 'starting': return '正在启动...';
          case 'uninstalling': return '正在卸载...';
          default: return '处理中...';
      }
  };

  const loadApps = async (autoReload = true) => {
    try {
      const data = await fetchApps();
      setApps(data.apps);
      setShuffleTick(t => t + 1); // 新数据 → 重新洗牌（随机展示）
      setUpgradeAllowed(data.upgrade_allowed !== false);
      setLastCheck(data.last_check);
      if (data.apps.length > 0) {
        setLoadStatus('loaded');
      } else if (!data.last_check && autoReload) {
        triggerReload();
      } else {
        setLoadStatus('loaded');
      }
    } catch (error) {
      console.error('Failed to load apps:', error);
      triggerReload();
    }
  };

  // 启动/停用已安装应用（与 fnOS 应用中心同步）
  const [controlling, setControlling] = useState<string | null>(null);
  // loadApps 每次渲染都是新函数；handleControl 要稳定（供 memo 化列表使用），
  // 故经 ref 间接调用，避免把 loadApps 身份带进依赖数组。
  const loadAppsRef = useRef<(autoReload?: boolean) => Promise<void>>(() => Promise.resolve());
  loadAppsRef.current = loadApps;

  const handleControl = useCallback(async (app: AppInfo, action: 'start' | 'stop') => {
    setControlling(app.appname);
    try {
      await controlApp(app.appname, action);
      toast.success(action === 'start' ? `已启动「${app.display_name}」` : `已停用「${app.display_name}」`);
      await loadAppsRef.current();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : `${action === 'start' ? '启动' : '停用'}失败`);
    } finally {
      setControlling(null);
    }
  }, []);

  // 打开已安装应用的 Web UI（与 fnOS 应用中心"打开"按钮同机制）：
  // 内嵌 fnOS Web UI（microApp 桥可用）时走壳窗口 openApp(serviceName)
  // 在壳内任务标签页打开 —— 原生应用中心同款行为；
  // 独立打开 :38011（桥不可用）时降级为新浏览器标签打开应用 URL。
  const handleOpenApp = useCallback(async (app: AppInfo) => {
    // App Store 风格单一"打开"按钮：应用未运行时先自动启动，轮询到 running 再打开。
    // 注意：应用停止时 daemon 不下发 web 字段（web_port/web_service_name 均为空），
    // 启动成功后必须用重新拉取的最新记录解析打开目标。
    let target = app;
    if (app.status && app.status !== 'running' && app.status !== 'nostart' && (app.start_stop ?? true)) {
      try {
        toast.info(`「${app.display_name}」尚未运行，正在启动后打开…`);
        await controlApp(app.appname, 'start');
        const t0 = Date.now();
        let fresh: AppInfo | undefined;
        let running = false;
        while (Date.now() - t0 < 30000) {
          await new Promise(r => setTimeout(r, 2000));
          try {
            const res = await fetchApps();
            fresh = (res.apps || []).find(a => a.appname === app.appname);
            if (fresh) {
              if (fresh.status === 'running') { running = true; break; }
              if (fresh.status === 'stopped') break; // 启动后又回落到 stopped，视为失败
            }
          } catch { /* 单次拉取失败继续轮询 */ }
        }
        await loadAppsRef.current();
        if (!running) {
          toast.error(`「${app.display_name}」未能及时运行，请稍后重试`);
          return;
        }
        if (fresh) target = fresh;
      } catch (e) {
        toast.error(e instanceof Error ? e.message : '启动失败，无法打开');
        return;
      }
    }
    if (target.web_service_name) {
      const ok = await openAppInShell(target.web_service_name);
      if (ok) return;
    }
    const url = appWebUrl(target);
    if (!url) {
      toast.error(`「${target.display_name}」没有可打开的 Web 界面`);
      return;
    }
    window.open(url, '_blank', 'noopener');
  }, []);

  const triggerReload = () => {
    // Cancel any in-flight reload SSE before starting a new one.
    reloadHandleRef.current?.cancel();

    setLoadStatus('retrying');
    setLoadMessages([]);

    const handle = reloadApps((data) => {
      if (data.step === 'trying') {
        setLoadMessages(prev => [...prev, { text: data.message || '', status: 'info' }]);
      } else if (data.step === 'failed') {
        setLoadMessages(prev => {
          const updated = [...prev];
          if (updated.length > 0) {
            updated[updated.length - 1] = { text: data.message || '', status: 'error' };
          }
          return updated;
        });
      } else if (data.step === 'success') {
        setLoadMessages(prev => {
          const updated = [...prev];
          if (updated.length > 0) {
            updated[updated.length - 1] = { text: data.message || '', status: 'success' };
          }
          return updated;
        });
      } else if (data.step === 'done') {
        setLoadMessages(prev => [...prev, { text: data.message || '', status: 'success' }]);
        loadApps(false);
      } else if (data.step === 'error') {
        setLoadMessages(prev => [...prev, { text: data.message || '', status: 'error' }]);
        setLoadStatus('failed');
      }
    });
    reloadHandleRef.current = handle;

    handle.promise.catch(() => {
      setLoadStatus('failed');
      setLoadMessages(prev => [...prev, { text: '网络连接失败', status: 'error' }]);
    }).finally(() => {
      if (reloadHandleRef.current === handle) {
        reloadHandleRef.current = null;
      }
    });
  };

  const createSSEHandler = useCallback((app: AppInfo, operation: 'install' | 'update' | 'uninstall'): SSECallback => (data) => {
    const appname = app.appname;

    if (data.step === 'self_update') {
      setSelfUpdateActive(true);
      selfUpdateActiveRef.current = true;
      selfUpdateRestartSeenRef.current = true;
      setSelfUpdateState({ message: data.message || '商店正在更新，请稍候...', progress: 100 });
      pollForRestart();
      return;
    }

    if (data.step === 'error') {
      const lastStep = appOperationsRef.current.get(appname)?.step || 'starting';
      toast.error(data.message || '发生未知错误', {
        action: {
          label: '上报',
          onClick: () => setReportTarget({ app: appname, step: lastStep, error: data.message || '发生未知错误' })
        }
      });
      setAppOp(appname, null);
      loadApps();
      return;
    }

    if (data.step === 'done') {
      setAppOp(appname, null);
      loadApps();

      if (operation === 'uninstall') {
        toast.success(`${app.display_name} 已卸载`);
      } else {
        setSuccessInfo({ app, operation });
      }
      return;
    }

    setAppOp(appname, {
      step: data.step || 'processing',
      progress: data.progress || 0,
      message: data.message || translateStep(data.step),
      speed: data.speed,
      downloaded: data.downloaded,
      total: data.total,
    });
  }, [setAppOp]);

  const handleCheck = async () => {
    setChecking(true);
    try {
      await triggerCheck();
      await loadApps();
    } catch (error) {
      console.error('Check failed:', error);
      toast.error('检查更新失败');
    } finally {
      setChecking(false);
    }
  };

  const runInstall = useCallback(async (app: AppInfo, wizard?: WizardParam[], panel?: PanelInstallParams) => {
    const appname = app.appname;
    // Guard: prevent double-trigger overwriting an in-flight operation's cancel handle.
    if (appOperationsRef.current.has(appname)) return;

    const handler = createSSEHandler(app, 'install');
    const handle = installApp(appname, handler, wizard, panel);
    setAppOp(appname, {
      step: 'starting',
      progress: 0,
      message: `正在安装 ${app.display_name}...`,
      cancel: handle.cancel,
    });

    try {
      await handle.promise;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        toast.info('已取消');
        setAppOp(appname, null);
        return;
      }
      // Self-update path: backend kills itself; SSE drop is expected ONLY
      // after we have actually seen the 'self_update' event.
      // pollForRestart() (started by self_update event) handles recovery.
      if (selfUpdateRestartSeenRef.current) {
        return;
      }
      console.error(error);
      toast.error('安装请求失败', {
        action: {
          label: '上报',
          onClick: () => setReportTarget({ app: app.appname, step: 'request', error: error instanceof Error ? error.message : String(error) })
        }
      });
    } finally {
      const hadOperation = appOperationsRef.current.has(appname);
      setAppOperations(prev => {
        if (!prev.has(appname)) return prev;
        const next = new Map(prev);
        next.delete(appname);
        return next;
      });
      // Skip loadApps during self-update: the server is restarting and the
      // poll-then-reload flow will refresh the entire page anyway.
      if (hadOperation && !selfUpdateActiveRef.current) {
        loadApps();
      }
    }
  }, [createSSEHandler, setAppOp]);

  const handleInstall = useCallback(async (app: AppInfo) => {
    if (appOperationsRef.current.has(app.appname)) return;

    // 官方应用中心源：走面板 cloud 通道。先拉详情+依赖（依赖弹窗数据），
    // 失败必须提示而不是静默回退（回退到 FPK 通道会 404/死循环，无意义）。
    if (app.source === 'fnos-official') {
      setPanelApp(app);
      setPanelLoading(true);
      setPanelDetail(null);
      try {
        const d = await fetchPanelDetail(app.appname);
        setPanelDetail(d);
      } catch (err) {
        setPanelApp(null);
        setPanelLoading(false);
        toast.error(err instanceof Error ? err.message : '获取官方应用详情失败');
        return;
      }
      setPanelLoading(false);
      return;
    }

    // Probe for an install form first. A lookup failure must never block
    // installing, so anything unexpected falls through to a plain install.
    setWizardApp(app);
    setWizardLoading(true);
    setWizardDef(null);
    setPanelWizardParams(null);
    try {
      const w = await fetchWizard(app.appname);
      if (w.has_wizard && (w.content?.length ?? 0) > 0) {
        setWizardDef(w);
        setWizardLoading(false);
        return;
      }
    } catch {
      // ignore — fall through and install with defaults
    }
    setWizardApp(null);
    setWizardLoading(false);
    void runInstall(app);
  }, [runInstall]);

  const handleUpdate = useCallback(async (app: AppInfo) => {
    const appname = app.appname;
    if (appOperationsRef.current.has(appname)) return;

    const handler = createSSEHandler(app, 'update');
    const handle = updateApp(appname, handler);
    setAppOp(appname, {
      step: 'starting',
      progress: 0,
      message: `正在更新 ${app.display_name}...`,
      cancel: handle.cancel,
    });

    try {
      await handle.promise;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        toast.info('已取消');
        setAppOp(appname, null);
        return;
      }
      if (selfUpdateRestartSeenRef.current) {
        return;
      }
      console.error(error);
      toast.error('更新请求失败', {
        action: {
          label: '上报',
          onClick: () => setReportTarget({ app: app.appname, step: 'request', error: error instanceof Error ? error.message : String(error) })
        }
      });
    } finally {
      const hadOperation = appOperationsRef.current.has(appname);
      setAppOperations(prev => {
        if (!prev.has(appname)) return prev;
        const next = new Map(prev);
        next.delete(appname);
        return next;
      });
      if (hadOperation && !selfUpdateActiveRef.current) {
        loadApps();
      }
    }
  }, [createSSEHandler, setAppOp]);

  const handleUninstall = useCallback((app: AppInfo) => {
    setPendingUninstallApp(app);
  }, []);

  const confirmUninstall = useCallback(async () => {
    if (!pendingUninstallApp) return;
    const app = pendingUninstallApp;
    setPendingUninstallApp(null);

    const appname = app.appname;
    const handler = createSSEHandler(app, 'uninstall');

    setAppOp(appname, {
      step: 'uninstalling',
      progress: 0,
      message: `正在卸载 ${app.display_name}...`,
    });

    const handle = uninstallApp(appname, handler);

    try {
      await handle.promise;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        toast.info('已取消');
        setAppOp(appname, null);
        return;
      }
      console.error(error);
      toast.error('卸载请求失败', {
        action: {
          label: '上报',
          onClick: () => setReportTarget({ app: app.appname, step: 'request', error: error instanceof Error ? error.message : String(error) })
        }
      });
    } finally {
      const hadOperation = appOperationsRef.current.has(appname);
      setAppOperations(prev => {
        if (!prev.has(appname)) return prev;
        const next = new Map(prev);
        next.delete(appname);
        return next;
      });
      if (hadOperation) {
        loadApps();
      }
    }
  }, [pendingUninstallApp, createSSEHandler, setAppOp]);

  const handleCancelOp = useCallback((app: AppInfo) => {
    const op = appOperations.get(app.appname);
    if (op?.cancel) {
      op.cancel();
      toast.info('已取消');
      setAppOp(app.appname, null);
      loadApps();
    }
  }, [appOperations, setAppOp]);

  const handleIgnoreUpdate = useCallback(async (app: AppInfo) => {
    try {
      await ignoreUpdate(app.appname);
      await loadApps();
      toast.success(`${app.display_name} 已忽略更新`);
    } catch {
      toast.error('忽略更新失败');
    }
  }, []);

  const handleUnignoreUpdate = useCallback(async (app: AppInfo) => {
    try {
      await unignoreUpdate(app.appname);
      await loadApps();
      toast.success(`${app.display_name} 已取消忽略更新`);
    } catch {
      toast.error('取消忽略更新失败');
    }
  }, []);

  const handleStoreUpdate = useCallback(async () => {
    setSelfUpdateActive(true);
    selfUpdateActiveRef.current = true;
    setSelfUpdateState({ message: '正在更新商店...', progress: 0 });

    const handle = triggerStoreUpdate((data) => {
      if (data.step === 'self_update') {
        selfUpdateRestartSeenRef.current = true;
        setSelfUpdateState({ message: '商店正在重启...', progress: 100 });
        pollForRestart();
        return;
      }
      if (data.step === 'error') {
        toast.error(data.message || '商店更新失败');
        setSelfUpdateActive(false);
        selfUpdateActiveRef.current = false;
        setSelfUpdateState(null);
        return;
      }
      setSelfUpdateState({
        message: data.message || '正在更新商店...',
        progress: data.progress || 0,
        speed: data.speed,
        downloaded: data.downloaded,
        total: data.total,
      });
    });

    try {
      await handle.promise;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return;
      // Only suppress the toast if the backend actually emitted the self_update
      // event - otherwise pre-SSE failures (HTTP 409, network errors) would be
      // silently swallowed and the overlay would be stuck at 0% forever.
      if (selfUpdateRestartSeenRef.current) {
        return;
      }
      console.error(error);
      toast.error('商店更新失败');
      setSelfUpdateActive(false);
      selfUpdateActiveRef.current = false;
      setSelfUpdateState(null);
    }
  }, []);

  // 随机排序权重：Fisher-Yates 洗牌当前应用列表，key → 随机位次
  const appsRef = useRef<AppInfo[]>(apps);
  appsRef.current = apps;
  const shuffledRank = useMemo(() => {
    const arr = appsRef.current.map(a => a.key || a.appname);
    for (let i = arr.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [arr[i], arr[j]] = [arr[j], arr[i]];
    }
    const rank = new Map<string, number>();
    arr.forEach((n, idx) => rank.set(n, idx));
    return rank;
    // 故意只依赖 shuffleTick：洗牌时机 = 每次 loadApps 成功（见 setShuffleTick）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shuffleTick]);

  const filteredApps = useMemo(() => apps.filter(app => {
    if (activeFilter === 'installed' && !app.installed) return false;
    if (activeFilter === 'update_available' && !app.has_update) return false;
    if (activeCategory && app.category !== activeCategory) return false;
    if (searchQuery.trim()) {
      // 多词条 AND：搜索框里的每个空格分隔词条都必须命中（徽章词条叠加多选即走这里）
      const terms = searchQuery.toLowerCase().split(/\s+/).filter(Boolean);
      const name = (app.display_name || '').toLowerCase();
      const appname = (app.appname || '').toLowerCase();
      const desc = (app.description || '').toLowerCase();
      // 源名/开发者走显示名（官方→飞牛应用中心源、内置→fnos-store/conversun），
      // 与徽章点击填入搜索框的词条一致
      const source = sourceLabel(app).toLowerCase();
      const author = effectiveMaintainer(app).toLowerCase();
      const distributor = (app.distributor || '').toLowerCase();
      const hay = [name, appname, desc, source, author, distributor];
      for (const q of terms) {
        if (!hay.some(h => h.includes(q))) return false;
      }
    }

    return true;
  }).sort((a, b) => {
    switch (sortBy) {
      case 'downloads':
        return (b.download_count ?? 0) - (a.download_count ?? 0);
      case 'name':
        return (a.display_name || '').localeCompare(b.display_name || '');
      case 'alpha': {
        // 首字母 A-Z：字母优先（1Panel→P）、纯数字名 0-9、中文按拼音首字母
        const ia = alphaInitial(a.display_name || a.appname);
        const ib = alphaInitial(b.display_name || b.appname);
        if (ia !== ib) {
          if (ia === '#') return 1;
          if (ib === '#') return -1;
          return ia.localeCompare(ib);
        }
        return (a.display_name || '').localeCompare(b.display_name || '', 'zh-Hans-CN');
      }
      case 'updated':
        return (b.updated_at || '').localeCompare(a.updated_at || '');
      default:
        // 随机展示：「全部」视图按洗牌顺序（不按来源分组）；其他 tab 保持后端顺序
        if (activeFilter === 'all') {
          const ra = shuffledRank.get(a.key || a.appname) ?? Number.MAX_SAFE_INTEGER;
          const rb = shuffledRank.get(b.key || b.appname) ?? Number.MAX_SAFE_INTEGER;
          return ra - rb;
        }
        return 0;
    }
  }), [apps, activeFilter, activeCategory, searchQuery, sortBy, shuffledRank]);

  const counts = useMemo(() => ({
      all: apps.length,
      installed: apps.filter(a => a.installed).length,
      update_available: apps.filter(a => a.has_update).length,
      recommended: recommendedApps.length
  }), [apps, recommendedApps]);

  const categoryCounts = useMemo(() => CATEGORIES.reduce((acc, cat) => {
    acc[cat.key] = apps.filter(a => a.category === cat.key).length;
    return acc;
  }, {} as Record<string, number>), [apps]);

  // 「全部」复合 pill：pill 主体 = 选择全部分类；右侧 ▾ = 排序菜单（折叠在全部里）
  const allCategoryPill = (
    // z-50：菜单打开时固定覆盖层（z-40）挡住页面其余部分，但 pill 本体要
    // 保持在覆盖层之上，用户才能再点 pill/▾ 收起菜单。
    <div className="relative z-50 shrink-0">
      <button
        onClick={() => setActiveCategory(null)}
        className={cn(
          "relative z-[60] flex items-center gap-0.5 shrink-0 h-8 pl-3.5 pr-1.5 rounded-full text-[13px] font-medium whitespace-nowrap",
          activeCategory === null ? "bg-primary text-primary-foreground" : "bg-muted/60 text-foreground"
        )}
      >
        全部
        <span
          role="button"
          aria-label="排序"
          title="排序"
          onClick={(e) => {
            e.stopPropagation();
            if (sortMenuOpen) {
              setSortMenuOpen(false);
              return;
            }
            // 菜单左缘与「全部」pill 左缘对齐（不用 ▾ 的位置）
            const r = ((e.currentTarget as HTMLElement).parentElement as HTMLElement).getBoundingClientRect();
            const menuW = 160;
            const menuH = 190;
            const left = Math.max(8, Math.min(r.left, window.innerWidth - menuW - 8));
            const top = Math.max(8, Math.min(r.bottom + 4, window.innerHeight - menuH - 8));
            setSortMenuPos({ left, top });
            setSortMenuOpen(true);
          }}
          className={cn("flex items-center justify-center h-6 w-6 rounded-full transition-colors", sortMenuOpen && "bg-black/10")}
        >
          <ChevronsUpDown className="h-3.5 w-3.5" />
        </span>
      </button>
      {sortMenuOpen && sortMenuPos && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setSortMenuOpen(false)} />
          <div
            className="fixed z-50 w-40 rounded-xl border border-border bg-popover p-1 shadow-lg"
            style={{ left: sortMenuPos.left, top: sortMenuPos.top }}
          >
            <p className="px-2.5 py-1.5 text-[11px] font-medium text-muted-foreground">排序</p>
            {SORT_OPTIONS.map(o => (
              <button
                key={o.value}
                onClick={() => { setSortBy(o.value); setSortMenuOpen(false); }}
                className="flex w-full items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 text-[13px] hover:bg-muted"
              >
                {o.label}
                {sortBy === o.value && <Check className="h-3.5 w-3.5 shrink-0 text-primary" />}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );

  return (
    <div className="min-h-dvh bg-background text-foreground flex flex-col md:flex-row">
      <aside className={cn(
        "hidden md:flex flex-col bg-card/70 backdrop-blur-xl border-r border-border/50 h-dvh sticky top-0 transition-all duration-300 overflow-hidden shrink-0",
        sidebarCollapsed ? "w-[68px]" : "w-64"
      )}>
        <TooltipProvider delayDuration={0}>
         <div className={cn("border-b border-border shrink-0", sidebarCollapsed ? "p-3 flex items-center justify-center" : "p-6")}>
           {sidebarCollapsed ? (
             <Button variant="ghost" size="icon" className="h-8 w-8" onClick={toggleSidebar}>
               <ChevronsRight className="h-4 w-4" />
             </Button>
           ) : (
             <div className="flex items-start justify-between gap-2">
               <div className="min-w-0">
                 <h1 className="text-xl font-semibold tracking-tight whitespace-nowrap">New Store</h1>
                 <p className="text-sm text-muted-foreground mt-1.5 whitespace-nowrap">
                    上次检查: {lastCheck ? new Date(lastCheck).toLocaleString() : '从未'}
                 </p>
               </div>
               <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0 -mr-2 -mt-1" onClick={toggleSidebar}>
                 <ChevronsLeft className="h-4 w-4" />
               </Button>
             </div>
           )}
         </div>

         <div className="flex-1 overflow-y-auto">
          <nav className={cn("space-y-1", sidebarCollapsed ? "p-2" : "p-4")}>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={activeFilter === 'recommended' ? 'default' : 'ghost'}
                  className={cn("w-full h-10 shadow-none rounded-lg font-medium", sidebarCollapsed ? "justify-center px-0" : "justify-start px-3")}
                  onClick={() => { switchFilter('recommended'); }}
                >
                  <Compass className={cn("h-4 w-4 shrink-0", !sidebarCollapsed && "mr-3")} />
                  {!sidebarCollapsed && (
                    <>
                      <span className="flex-1 text-left whitespace-nowrap">发现</span>
                      <span className="ml-auto text-xs opacity-80 tabular-nums">{counts.recommended}</span>
                    </>
                  )}
                </Button>
              </TooltipTrigger>
              {sidebarCollapsed && <TooltipContent side="right">发现 ({counts.recommended})</TooltipContent>}
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={activeFilter === 'all' ? 'default' : 'ghost'}
                  className={cn("w-full h-10 shadow-none rounded-lg font-medium", sidebarCollapsed ? "justify-center px-0" : "justify-start px-3")}
                  onClick={() => { switchFilter('all'); }}
                >
                  <LayoutGrid className={cn("h-4 w-4 shrink-0", !sidebarCollapsed && "mr-3")} />
                  {!sidebarCollapsed && (
                    <>
                      <span className="flex-1 text-left whitespace-nowrap">全部</span>
                      <span className={cn("ml-auto text-xs tabular-nums", activeFilter === 'all' ? "text-white/80" : "text-muted-foreground")}>{counts.all}</span>
                    </>
                  )}
                </Button>
              </TooltipTrigger>
              {sidebarCollapsed && <TooltipContent side="right">全部 ({counts.all})</TooltipContent>}
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={activeFilter === 'installed' ? 'default' : 'ghost'}
                  className={cn("w-full h-10 shadow-none rounded-lg font-medium", sidebarCollapsed ? "justify-center px-0" : "justify-start px-3")}
                  onClick={() => { switchFilter('installed'); }}
                >
                  <CheckCircle2 className={cn("h-4 w-4 shrink-0", !sidebarCollapsed && "mr-3")} />
                  {!sidebarCollapsed && (
                    <>
                      <span className="flex-1 text-left whitespace-nowrap">已安装</span>
                      <span className={cn("ml-auto text-xs tabular-nums", activeFilter === 'installed' ? "text-white/80" : "text-muted-foreground")}>{counts.installed}</span>
                    </>
                  )}
                </Button>
              </TooltipTrigger>
              {sidebarCollapsed && <TooltipContent side="right">已安装 ({counts.installed})</TooltipContent>}
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={activeFilter === 'update_available' ? 'default' : 'ghost'}
                  className={cn("w-full h-10 shadow-none rounded-lg font-medium", sidebarCollapsed ? "justify-center px-0" : "justify-start px-3")}
                  onClick={() => { switchFilter('update_available'); }}
                >
                  <div className="relative shrink-0">
                    <RefreshCw className={cn("h-4 w-4", !sidebarCollapsed && "mr-3")} />
                    {sidebarCollapsed && counts.update_available > 0 && (
                      <span className="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-destructive" />
                    )}
                  </div>
                  {!sidebarCollapsed && (
                    <>
                      <span className="flex-1 text-left whitespace-nowrap">有更新</span>
                      {counts.update_available > 0 ? (
                        <Badge
                          variant={activeFilter === 'update_available' ? 'secondary' : 'destructive'}
                          className={cn("ml-auto shrink-0", activeFilter === 'update_available' && "bg-white/25 text-white border-0")}
                        >
                          {counts.update_available}
                        </Badge>
                      ) : (
                        <span className="ml-auto text-xs text-muted-foreground tabular-nums">0</span>
                      )}
                    </>
                  )}
                </Button>
              </TooltipTrigger>
              {sidebarCollapsed && (
                <TooltipContent side="right">有更新 ({counts.update_available})</TooltipContent>
              )}
            </Tooltip>
          </nav>
          
         </div>

         <div className={cn("mt-auto border-t border-border space-y-1", sidebarCollapsed ? "p-2" : "p-4")}>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  className={cn(
                    "w-full h-10 shadow-none text-muted-foreground hover:text-foreground",
                    sidebarCollapsed ? "justify-center px-0" : "justify-start px-3"
                  )}
                  onClick={() => window.open('https://github.com/conversun/fnos-apps/issues/new?template=bug-report.yml', '_blank')}
                >
                  <MessageCircle className={cn("h-4 w-4 shrink-0", !sidebarCollapsed && "mr-3")} />
                  {!sidebarCollapsed && <span className="flex-1 text-left whitespace-nowrap">问题反馈</span>}
                </Button>
              </TooltipTrigger>
              {sidebarCollapsed && <TooltipContent side="right">问题反馈</TooltipContent>}
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  className={cn(
                    "w-full h-10 shadow-none text-muted-foreground hover:text-foreground",
                    sidebarCollapsed ? "justify-center px-0" : "justify-start px-3"
                  )}
                  onClick={() => setSettingsVisible(true)}
                 >
                  <div className="relative shrink-0">
                    <Settings className={cn("h-4 w-4", !sidebarCollapsed && "mr-3")} />
                    {storeHasUpdate && (
                      <span className="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-destructive" />
                    )}
                  </div>
                  {!sidebarCollapsed && <span className="flex-1 text-left whitespace-nowrap">设置</span>}
                </Button>
              </TooltipTrigger>
              {sidebarCollapsed && <TooltipContent side="right">设置{storeHasUpdate ? ' (有更新)' : ''}</TooltipContent>}
            </Tooltip>
         </div>
        </TooltipProvider>
       </aside>

      <div className="flex-1 flex flex-col min-h-0 md:min-h-dvh min-w-0">
        <div className={cn(
            "md:hidden bg-card/70 backdrop-blur-xl border-b border-border/50 px-4 pt-4 pb-3 sticky top-0 z-20 flex flex-col gap-3 transition-[box-shadow,border-color] duration-300",
            searchExpanded && "shadow-lg border-b-transparent"
          )}>
            {searchExpanded ? (
              /* 展开态：与原搜索框等长的全宽搜索框，悬浮在页面上方（sticky header + 阴影）；
                 收起立即（无过渡，1.14.24 用户要求） */
              <div className="search-expand-anim relative">
                <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
                <Input
                  ref={searchInputRef}
                  autoFocus
                  type="text"
                  placeholder="搜索应用..."
                  value={searchInput}
                  onChange={(e) => {
                    setSearchInput(e.target.value);
                    /* 从"发现"页搜索时切到应用列表，保证有结果区。
                       输入不再触发任何自动收起（输入途中保持展开） */
                    if (activeFilter === 'recommended' && e.target.value) switchFilter('all');
                  }}
                  onKeyDown={(e) => {
                    /* 按回车 → 收起（搜索词保留，列表保持过滤） */
                    if (e.key === 'Enter') collapseSearch();
                  }}
                  onBlur={() => collapseSearch()}
                  className="w-full pl-9 pr-16 h-9 shadow-none rounded-full border-0 bg-muted/60 focus-visible:ring-primary/40"
                />
                {searchInput && (
                  <button
                    /* preventDefault 保住输入框焦点，让清除点击生效（否则 blur 先收起）；
                       清空 → 带动画收起 */
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => { setSearchInput(''); collapseSearch(); }}
                    className="absolute right-[52px] top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-4 w-4" />
                  </button>
                )}
                <button
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => collapseSearch()}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 h-8 px-1.5 text-[13px] font-medium text-primary"
                >
                  收起
                </button>
              </div>
            ) : (
              /* 收起态：标题 + 紧凑搜索药丸 + 三个按钮（间距加大防误触） */
              <div className="flex items-center justify-between gap-2">
                {/* 收起态：加长药丸；有搜索词时显示内容 + × 清除 */}
                <button
                  onClick={() => expandSearch()}
                  className="flex items-center gap-1.5 h-9 w-[150px] pl-3.5 pr-2.5 rounded-full bg-muted/60 hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                  aria-label="搜索"
                  title="搜索"
                >
                  <Search className="h-[18px] w-[18px] shrink-0" />
                  {searchInput ? (
                    <>
                      <span className="min-w-0 flex-1 truncate text-left text-[13px]">{searchInput}</span>
                      <span
                        role="button"
                        aria-label="清除搜索"
                        title="清除搜索"
                        onMouseDown={(e) => { e.preventDefault(); e.stopPropagation(); }}
                        onClick={(e) => {
                          e.stopPropagation();
                          setSearchInput('');
                        }}
                        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-black/10 text-muted-foreground hover:text-foreground"
                      >
                        <X className="h-3 w-3" />
                      </span>
                    </>
                  ) : (
                    <span className="text-[13px]">搜索</span>
                  )}
                </button>
                {/* 搜索栏后常显应用数：无搜索词=目录总数（目录刷新后实时更新），
                    有搜索词=过滤结果数 */}
                <span className="shrink-0 text-[12px] tabular-nums text-muted-foreground" title="当前目录应用总数">
                  {searchInput ? filteredApps.length : apps.length} 个应用
                </span>
                <div className="flex items-center gap-3 shrink-0">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-9 w-9"
                    onClick={() => setSettingsVisible(true)}
                    aria-label="设置"
                    title="设置"
                  >
                    <div className="relative">
                      <Settings className="h-[18px] w-[18px]" />
                      {storeHasUpdate && (
                        <span className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-destructive" />
                      )}
                    </div>
                  </Button>
                  <ThemeToggle className="h-9 w-9" />
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-9 w-9"
                    onClick={handleCheck}
                    disabled={checking}
                    aria-label="检查更新"
                    title="检查更新"
                  >
                    <RefreshCw className={cn("h-[18px] w-[18px]", checking && "animate-spin")} />
                  </Button>
                </div>
              </div>
            )}
            {/* 分类 pill 行：与上方搜索框左缘对齐（header px-4），不再贴屏幕边 */}
            {activeFilter !== 'recommended' && (
              <div className="flex gap-2 overflow-x-auto no-scrollbar">
                {allCategoryPill}
                {CATEGORIES.map(cat => (
                  <button
                    key={cat.key}
                    onClick={() => setActiveCategory(cat.key)}
                    className={cn(
                      "shrink-0 h-8 px-3.5 rounded-full text-[13px] font-medium whitespace-nowrap",
                      activeCategory === cat.key ? "bg-primary text-primary-foreground" : "bg-muted/60 text-foreground"
                    )}
                  >
                    {cat.label}
                  </button>
                ))}
              </div>
            )}
        </div>

        <header className={cn(
            "hidden md:flex px-8 justify-between items-center sticky top-0 z-10 transition-all duration-300",
            mainScrolled ? "bg-card/70 backdrop-blur-xl border-b border-border/50 py-2" : "bg-transparent border-b border-transparent py-4"
          )}>
           <div className="flex items-center gap-2 shrink-0">
           <h2 className={cn("font-bold tracking-tight shrink-0 transition-all duration-300", mainScrolled ? "text-lg" : "text-[32px] leading-[1.2]")}>
              {activeFilter === 'recommended' && '发现'}
              {activeFilter === 'all' && '应用'}
              {activeFilter === 'installed' && '已安装'}
              {activeFilter === 'update_available' && '可用更新'}
              {activeFilter !== 'recommended' && activeCategory && (
                <span className={cn("text-muted-foreground font-normal transition-all duration-300", mainScrolled ? "text-sm" : "text-xl")}>{' · '}{CATEGORIES.find(c => c.key === activeCategory)?.label}</span>
              )}
           </h2>
           </div>
           <div className="flex items-center gap-3">
               {activeFilter !== 'recommended' && (
                 <>
                   <div className="relative">
                     <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
                     <Input
                       type="text"
                       placeholder="搜索应用..."
                       value={searchInput}
                       onChange={(e) => setSearchInput(e.target.value)}
                       className="w-56 md:w-64 pl-9 pr-8 h-9 shadow-none rounded-full border-0 bg-muted/60 focus-visible:ring-primary/40"
                     />
                     {searchInput && (
                       <button
                         onClick={() => setSearchInput('')}
                         className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                       >
                         <X className="h-4 w-4" />
                       </button>
                     )}
                   </div>
                   {/* 搜索框后常显应用数：无搜索词=目录总数（目录刷新后实时更新），
                       有搜索词=过滤结果数 */}
                   <span className="shrink-0 text-[12px] tabular-nums text-muted-foreground" title="当前目录应用总数">
                     {searchInput ? filteredApps.length : apps.length} 个应用
                   </span>
                 </>
               )}
               <ThemeToggle />
               <Button 
                 onClick={handleCheck} 
                 disabled={checking}
                 className="rounded-full"
               >
                 {checking ? (
                   <>
                     <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                     检查中...
                   </>
                 ) : (
                   <>
                     <RefreshCw className="mr-2 h-4 w-4" />
                     立即检查
                   </>
                 )}
               </Button>
           </div>
        </header>

        <main className="flex-grow p-4 pb-24 md:p-8 md:pb-8 overflow-y-auto">
          {activeFilter === 'recommended' ? (
            <div className="space-y-10">
              {apps.length > 0 && (
                <FeaturedShowcase apps={apps} onDetail={setDetailApp} />
              )}
              {recommendedApps.length > 0 ? (
                <section>
                  <div className="flex items-center gap-2 mb-3">
                    <h2 className="text-lg font-bold tracking-tight">探索推荐</h2>
                    <button
                      type="button"
                      onClick={() => setExploreExpanded(v => !v)}
                      aria-expanded={exploreExpanded}
                      className="inline-flex items-center gap-1 h-7 px-3 rounded-full bg-muted/60 hover:bg-muted text-xs font-medium text-foreground transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary"
                    >
                      {exploreExpanded ? '收起' : '展开'}
                      <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", exploreExpanded && "rotate-180")} />
                    </button>
                  </div>
                  {exploreExpanded && (
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                      {recommendedApps.map(app => (
                        <RecommendedAppCard key={app.name} app={app} />
                      ))}
                    </div>
                  )}
                </section>
              ) : (
                <div className="flex flex-col items-center justify-center h-64">
                  <Compass className="h-12 w-12 text-muted-foreground/50 mb-4" />
                  <p className="text-muted-foreground font-medium">暂无推荐应用</p>
                  <p className="text-sm text-muted-foreground mt-1">请稍后再来看看</p>
                </div>
              )}
            </div>
          ) : loadStatus === 'loaded' ? (
            <>
              {/* 分类筛选条（App Store 风格横排 pill，桌面端；移动端在顶部 header） */}
              <div className="hidden md:flex items-center gap-2 overflow-x-auto no-scrollbar mb-5">
                {allCategoryPill}
                {CATEGORIES.map(cat => (
                  <button
                    key={cat.key}
                    onClick={() => setActiveCategory(cat.key)}
                    className={cn(
                      "shrink-0 h-8 px-3.5 rounded-full text-[13px] font-medium whitespace-nowrap transition-colors",
                      activeCategory === cat.key ? "bg-primary text-primary-foreground" : "bg-muted/60 text-foreground hover:bg-muted"
                    )}
                  >
                    {cat.label}
                    <span className="ml-1 text-xs opacity-60 tabular-nums">{categoryCounts[cat.key] ?? 0}</span>
                  </button>
                ))}
              </div>

              <div className="hidden md:block">
                <AppList
                   apps={filteredApps}
                   loading={false}
                   onInstall={handleInstall}
                   onUpdate={handleUpdate}
                   onUninstall={handleUninstall}
                   onDetail={setDetailApp}
                   onCancelOp={handleCancelOp}
                   upgradeAllowed={upgradeAllowed}
                   filterType={activeFilter}
                   appOperations={appOperations}
                   searchQuery={searchQuery}
                   onSourceFilter={applyTextFilter}
                   onAuthorFilter={applyTextFilter}
                   onDistributorFilter={applyTextFilter}
                   activeTerms={activeSearchTerms}
                   onControl={handleControl}
                   controlling={controlling}
                   onOpenApp={handleOpenApp}
                />
              </div>
              <div className="md:hidden">
                <AppRowList
                  apps={filteredApps}
                  onInstall={handleInstall}
                  onUpdate={handleUpdate}
                  onUninstall={handleUninstall}
                  onDetail={setDetailApp}
                  onCancelOp={handleCancelOp}
                  appOperations={appOperations}
                  searchQuery={searchQuery}
                  filterType={activeFilter}
                  upgradeAllowed={upgradeAllowed}
                  onSourceFilter={applyTextFilter}
                  onAuthorFilter={applyTextFilter}
                  onDistributorFilter={applyTextFilter}
                  activeTerms={activeSearchTerms}
                  onControl={handleControl}
                  controlling={controlling}
                  onOpenApp={handleOpenApp}
                />
              </div>
            </>
          ) : loadStatus === 'loading' ? (
            <div className="flex flex-col items-center justify-center h-64">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground mb-4" />
              <p className="text-sm text-muted-foreground">正在加载应用列表...</p>
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center h-64 max-w-sm mx-auto">
              {loadStatus === 'retrying' && (
                <Loader2 className="h-8 w-8 animate-spin text-primary mb-6" />
              )}
              {loadStatus === 'failed' && (
                <WifiOff className="h-8 w-8 text-muted-foreground mb-6" />
              )}
              <div className="w-full space-y-2 mb-6">
                {loadMessages.map((msg, i) => (
                  <div key={i} className="flex items-center gap-2 text-sm">
                    {msg.status === 'info' && i === loadMessages.length - 1 ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-primary shrink-0" />
                    ) : msg.status === 'success' ? (
                      <CircleCheck className="h-3.5 w-3.5 text-emerald-500 shrink-0" />
                    ) : msg.status === 'error' ? (
                      <CircleX className="h-3.5 w-3.5 text-destructive shrink-0" />
                    ) : (
                      <div className="h-3.5 w-3.5 shrink-0" />
                    )}
                    <span className={cn(
                      msg.status === 'error' ? 'text-destructive' :
                      msg.status === 'success' ? 'text-emerald-500' :
                      'text-muted-foreground'
                    )}>{msg.text}</span>
                  </div>
                ))}
              </div>
              {loadStatus === 'failed' && (
                <div className="flex gap-2">
                  <Button size="sm" onClick={triggerReload}>
                    <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
                    重试
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setSettingsVisible(true)}>
                    <Settings className="mr-1.5 h-3.5 w-3.5" />
                    更换加速节点
                  </Button>
                </div>
              )}
            </div>
          )}
        </main>
      </div>

      {/* 移动端底部 dock（iOS App Store 标签栏）：
          始终挂载，键盘弹出时靠 bottomOffset 下移钉在物理屏幕底边
          （被键盘盖住、不跟键盘上移、收起无回弹）；仅 pan 型壳兜底
          时整体隐藏。 */}
      {!dockHidden && (
        <MobileDock
          active={activeFilter}
          onSelect={(key) => { switchFilter(key); }}
          updateCount={counts.update_available}
          bottomOffset={dockOffsetPx}
        />
      )}

      {selfUpdateActive && selfUpdateState && (
        <ProgressOverlay
          visible={true}
          message={selfUpdateState.message}
          progress={selfUpdateState.progress}
          speed={selfUpdateState.speed}
          downloaded={selfUpdateState.downloaded}
          total={selfUpdateState.total}
        />
      )}

      {/* 后台任务进度（退出应用后继续跑的安装/更新，轮询展示） */}
      <BackgroundTasksIndicator />

      <Suspense fallback={null}>
        <SettingsPage
          open={settingsVisible}
          onOpenChange={setSettingsVisible}
          onStoreUpdate={handleStoreUpdate}
          onCatalogChanged={() => setTimeout(() => loadApps(), 2500)}
        />
      </Suspense>

      {wizardApp && (
        <Suspense fallback={null}>
        <WizardDialog
          appDisplayName={wizardApp.display_name}
          wizard={wizardDef}
          loading={wizardLoading}
          onCancel={() => {
            setWizardApp(null);
            setWizardDef(null);
            setWizardLoading(false);
            setPanelWizardParams(null);
          }}
          onConfirm={(params) => {
            const app = wizardApp;
            const panelParams = panelWizardParams;
            setWizardApp(null);
            setWizardDef(null);
            setWizardLoading(false);
            setPanelWizardParams(null);
            void runInstall(app, params, panelParams ?? undefined);
          }}
        />
        </Suspense>
      )}

      {panelApp && panelDetail && (
        <Suspense fallback={null}>
        <PanelInstallDialog
          detail={panelDetail}
          loading={panelLoading}
          onCancel={() => {
            setPanelApp(null);
            setPanelDetail(null);
            setPanelLoading(false);
          }}
          onConfirm={(params: PanelInstallParams) => {
            const app = panelApp;
            setPanelApp(null);
            setPanelDetail(null);
            setPanelLoading(false);
            // 官方通道：体积/依赖确认后先静默预取向导（后端会先下载包再取
            // install/info，安装时直接复用）。带向导则弹向导，否则直接装。
            // 预取失败不阻塞安装（与 FPK 通道同一契约）。
            setPanelWizardParams(params);
            setWizardApp(app);
            setWizardLoading(true);
            setWizardDef(null);
            fetchWizard(app.appname)
              .then((w) => {
                if (w.has_wizard && (w.content?.length ?? 0) > 0) {
                  setWizardDef(w);
                  setWizardLoading(false);
                  return;
                }
                setWizardApp(null);
                setWizardLoading(false);
                setPanelWizardParams(null);
                void runInstall(app, undefined, params);
              })
              .catch(() => {
                setWizardApp(null);
                setWizardLoading(false);
                setPanelWizardParams(null);
                void runInstall(app, undefined, params);
              });
          }}
        />
        </Suspense>
      )}

      <AlertDialog open={!!pendingUninstallApp} onOpenChange={(open) => !open && setPendingUninstallApp(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认卸载</AlertDialogTitle>
            <AlertDialogDescription>
              确定要卸载 {pendingUninstallApp?.display_name} 吗？此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmUninstall}>
              确认卸载
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Suspense fallback={null}>
        <AppDetailDialog
          app={detailApp}
          open={!!detailApp}
          onOpenChange={(open) => !open && setDetailApp(null)}
          onInstall={handleInstall}
          onUpdate={handleUpdate}
          onIgnoreUpdate={handleIgnoreUpdate}
          onUnignoreUpdate={handleUnignoreUpdate}
          onUninstall={handleUninstall}
          operation={detailApp ? appOperations.get(detailApp.appname) : undefined}
          onSourceFilter={applyTextFilter}
          onAuthorFilter={applyTextFilter}
          onDistributorFilter={applyTextFilter}
          activeTerms={activeSearchTerms}
          onOpenApp={handleOpenApp}
          onControl={handleControl}
          controlling={controlling}
        />
      </Suspense>

      {successInfo && (
        <Dialog open={!!successInfo} onOpenChange={(open) => !open && setSuccessInfo(null)}>
          <DialogContent className="sm:max-w-sm rounded-[18px] border-border/20 shadow-appstore bg-card">
            <DialogHeader>
              <div className="flex items-center gap-3">
                <AppIcon
                  app={successInfo.app}
                  className="w-12 h-12 rounded-xl shrink-0"
                  iconClassName="h-6 w-6"
                />
                <div className="flex-1 min-w-0">
                  <DialogTitle className="text-base">{successInfo.app.display_name}</DialogTitle>
                  <div className="flex items-center gap-1.5 mt-1">
                    <CircleCheck className="h-3.5 w-3.5 text-emerald-500" />
                    <span className="text-sm text-emerald-600">
                      {successInfo.operation === 'install' ? '安装成功' : '更新成功'}
                    </span>
                  </div>
                </div>
              </div>
            </DialogHeader>

            {successInfo.operation === 'install' && successInfo.app.post_install_note && (
              <div className="bg-muted/50 rounded-lg p-3 text-sm text-muted-foreground leading-relaxed">
                {successInfo.app.post_install_note}
              </div>
            )}

            <div className="flex justify-end gap-2 pt-1">
              {successInfo.app.service_port && (
                <Button
                  variant="outline"
                  size="sm"
                  className="rounded-full px-4"
                  onClick={() => {
                    window.open(`${window.location.protocol}//${window.location.hostname}:${successInfo.app.service_port}`, '_blank');
                    setSuccessInfo(null);
                  }}
                >
                  <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
                  打开
                </Button>
              )}
              <Button
                size="sm"
                className="rounded-full px-4"
                onClick={() => setSuccessInfo(null)}
              >
                确定
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}

      <Suspense fallback={null}>
        <ReportFailureDialog
          open={!!reportTarget}
          onClose={() => setReportTarget(null)}
          app={reportTarget?.app || ''}
          step={reportTarget?.step || ''}
          errorMessage={reportTarget?.error || ''}
        />
      </Suspense>
      <Toaster />
    </div>
  );
};

export default App;
