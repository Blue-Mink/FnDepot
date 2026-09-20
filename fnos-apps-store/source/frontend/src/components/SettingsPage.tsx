import React, { useState, useEffect, useRef, useCallback } from 'react';
import { fetchSettings, updateSettings, fetchStoreUpdate, checkMirrors, fetchMirrorHealth, fetchDockerMirrorHealth, testPanelLogin, fetchFpkDownloads, deleteFpkDownload, installFpkDownload, type MirrorOption, type MirrorCheckResult, type VolumeOption, type MirrorHealth, type FpkDownloadFile } from '../api/client';
import type { StoreUpdateInfo } from '../api/client';
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { ArrowLeft, ChevronDown, Database, FolderDown, Loader2, RefreshCw, SlidersHorizontal, Trash2, Zap } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from "@/lib/utils"
import SourceManager from './SourceManager'

type SettingsTab = 'system' | 'source';

const TABS: { key: SettingsTab; label: string; icon: React.ElementType }[] = [
  { key: 'system', label: '系统设置', icon: SlidersHorizontal },
  { key: 'source', label: '应用源设置', icon: Database },
];

interface SettingsPageProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onStoreUpdate?: () => void;
  /** 外部应用源变化后刷新应用目录 */
  onCatalogChanged?: () => void;
}

function formatBytes(bytes: number): string {
  if (bytes <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val >= 100 ? Math.round(val) : val.toFixed(1)} ${units[i]}`;
}

function latencyColor(result: MirrorCheckResult): string {
  if (result.status !== 'ok') return 'text-muted-foreground';
  if (result.latency_ms <= 300) return 'text-green-600';
  if (result.latency_ms <= 800) return 'text-yellow-600';
  return 'text-red-500';
}

function latencyText(result: MirrorCheckResult): string {
  if (result.status === 'timeout') return '超时';
  if (result.status === 'error') return '失败';
  return `${result.latency_ms}ms`;
}

/**
 * 加速源健康面板（GitHub / Docker 加速共用）：
 * 每源一行（状态点 + 标签 + 「当前」徽标 + 延迟/失败次数），
 * 顶部「立即测速」按钮 + 智能模式提示 + 最近一次自动切换横幅。
 * 列表 = 全部真实镜像（去掉 direct/auto；自定义仅在已配置时显示）。
 * 监测源列表可折叠（状态持久化），折叠时显示一行健康摘要。
 */
const MirrorHealthPanel: React.FC<{
  title: string;
  health: MirrorHealth | null;
  options: MirrorOption[];
  customConfigured: boolean;
  labelOf: (key: string) => string;
  refreshing: boolean;
  onRefresh: () => void;
}> = ({ title, health, options, customConfigured, labelOf, refreshing, onRefresh }) => {
  const [collapsed, setCollapsed] = React.useState<boolean>(() => {
    try {
      return localStorage.getItem(`health-panel-collapsed:${title}`) === 'true';
    } catch { return false; }
  });
  const toggleCollapsed = () => {
    setCollapsed(prev => {
      const next = !prev;
      try { localStorage.setItem(`health-panel-collapsed:${title}`, String(next)); } catch { /* ignore */ }
      return next;
    });
  };

  const rows = React.useMemo(() => {
    const statsByKey = new Map((health?.mirrors || []).map((s) => [s.key, s] as const));
    const base = options.filter((o) =>
      o.key !== 'direct' && o.key !== 'auto' && (o.key !== 'custom' || customConfigured)
    );
    return base.map((o) => {
      const st = statsByKey.get(o.key);
      return {
        key: o.key,
        label: o.label,
        status: st?.status || '',
        latency_ms: st?.latency_ms || 0,
        consec_fails: st?.consec_fails || 0,
      };
    });
  }, [options, health, customConfigured]);

  const okCount = rows.filter((r) => r.status === 'ok').length;
  const failCount = rows.filter((r) => r.status === 'fail').length;

  return (
    <div className="rounded-xl bg-muted/30 border border-border/20 px-3 py-3">
      <div className="flex items-center justify-between mb-1">
        <div className="flex items-baseline gap-2">
          <span className="text-[13px] font-medium">{title}</span>
          <span className="text-[11px] text-muted-foreground">
            每 {health?.interval_s ? Math.round(health.interval_s / 60) : 5} 分钟自动测速
          </span>
        </div>
        <div className="flex items-center gap-0.5">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={onRefresh}
            disabled={refreshing}
            title="立即测速"
            aria-label="立即测速"
          >
            <RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={toggleCollapsed}
            title={collapsed ? '展开监测的加速源列表' : '折叠监测的加速源列表'}
            aria-label={collapsed ? '展开监测的加速源列表' : '折叠监测的加速源列表'}
          >
            <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", collapsed && "-rotate-90")} />
          </Button>
        </div>
      </div>
      {collapsed ? (
        <p className="text-[11px] text-muted-foreground">
          已监测 {rows.length} 个源：{okCount} 正常{failCount > 0 ? `，${failCount} 失败` : ''}
          {health?.active && health.active !== 'direct' && `，当前：${labelOf(health.active)}`}
        </p>
      ) : (
        <>
          <p className="text-[11px] text-muted-foreground mb-2.5">
            下载时自动按健康度选路：快而稳的源优先，失败源自动降权。
            {health?.selected === 'auto' && '当前为智能模式（自动选最快稳定源）。'}
          </p>
          {health?.last_switch && (
            <div className="mb-2.5 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-[12px] leading-relaxed text-primary">
              {health.last_switch.reason}
              <span className="ml-1 whitespace-nowrap">
                （{labelOf(health.last_switch.from)} → {labelOf(health.last_switch.to)}）
              </span>
            </div>
          )}
          <div className="space-y-1.5">
            {rows.map((row) => (
              <div key={row.key} className="flex items-center gap-2 text-[13px]">
                <span
                  className={cn(
                    "h-2 w-2 rounded-full shrink-0",
                    row.status === 'ok' && "bg-emerald-500",
                    row.status === 'fail' && "bg-red-500",
                    !row.status && "bg-muted-foreground/30"
                  )}
                />
                <span className="flex-1 truncate">{row.label}</span>
                {row.key === health?.active && (
                  <span className="shrink-0 rounded-full bg-primary/10 px-1.5 h-5 flex items-center text-[10px] font-medium text-primary">
                    当前
                  </span>
                )}
                <span className="shrink-0 w-[72px] text-right text-[11px] tabular-nums text-muted-foreground">
                  {row.status === 'ok'
                    ? `${row.latency_ms}ms`
                    : row.status === 'fail'
                      ? row.consec_fails > 1 ? `失败×${row.consec_fails}` : '失败'
                      : '未测速'}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
};

/**
 * 设置页（整页展示，与单应用详情同构）：
 * - 移动端 = 整页（inset-0），桌面端 = 居中宽面板
 * - 页内左侧栏：两个 tab（系统设置 / 应用源设置）+ 展开/收起按钮（状态持久化，
 *   默认移动端收起、桌面端展开）
 * - 顶栏 ← 返回按钮回到应用列表
 */
const SettingsPage: React.FC<SettingsPageProps> = ({
  open,
  onOpenChange,
  onStoreUpdate,
  onCatalogChanged,
}) => {
  const [tab, setTab] = useState<SettingsTab>('system');

  // ── 系统设置字段 ───────────────────────────────────────────────────
  const [interval, setInterval] = useState<number>(24);
  const [mirror, setMirror] = useState<string>('gh-proxy');
  const [mirrorOptions, setMirrorOptions] = useState<MirrorOption[]>([]);
  const [dockerMirror, setDockerMirror] = useState<string>('daocloud');
  const [dockerMirrorOptions, setDockerMirrorOptions] = useState<MirrorOption[]>([]);
  const [customGithubMirror, setCustomGithubMirror] = useState<string>('');
  const [customDockerMirror, setCustomDockerMirror] = useState<string>('');
  const [installVolume, setInstallVolume] = useState<number>(0);
  const [volumeOptions, setVolumeOptions] = useState<VolumeOption[]>([]);
  // FPK 下载目录 + 已下载列表（设置页展示，可同步刷新）
  const [downloadDir, setDownloadDir] = useState<string>('');
  const [fpkFiles, setFpkFiles] = useState<FpkDownloadFile[]>([]);
  const [fpkDir, setFpkDir] = useState<string>('');
  const [fpkLoading, setFpkLoading] = useState(false);
  const [fpkRemoving, setFpkRemoving] = useState<string | null>(null);
  // 已下载列表折叠（本地持久化）
  const [fpkListCollapsed, setFpkListCollapsed] = useState<boolean>(() => {
    try { return localStorage.getItem('fpk-list-collapsed') === '1'; } catch { return false; }
  });
  const toggleFpkList = () => {
    setFpkListCollapsed((v) => {
      try { localStorage.setItem('fpk-list-collapsed', v ? '0' : '1'); } catch { /* ignore */ }
      return !v;
    });
  };
  // 直接安装某个已下载 FPK（SSE 进度）
  const [fpkInstalling, setFpkInstalling] = useState<string | null>(null);
  const [fpkInstallMsg, setFpkInstallMsg] = useState<string>('');
  const [storeInfo, setStoreInfo] = useState<StoreUpdateInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  // 官方应用中心直连（面板账号）
  const [panelEnabled, setPanelEnabled] = useState(false);
  // 默认预填 fnos：绝大多数机器面板账号即 fnos，加载设置后若有已存值会覆盖
  const [panelUsername, setPanelUsername] = useState('fnos');
  const [panelPassword, setPanelPassword] = useState('');
  const [panelBaseURL, setPanelBaseURL] = useState('');
  const [panelHasPassword, setPanelHasPassword] = useState(false);
  const [panelTesting, setPanelTesting] = useState(false);
  // 高级折叠（面板地址）：默认收起，只有面板不在本机/改端口才需要
  const [panelAdvancedOpen, setPanelAdvancedOpen] = useState<boolean>(() => {
    try {
      return localStorage.getItem('panel-advanced-open') === 'true';
    } catch { return false; }
  });
  const togglePanelAdvanced = () => {
    setPanelAdvancedOpen(prev => {
      const next = !prev;
      try { localStorage.setItem('panel-advanced-open', String(next)); } catch { /* ignore */ }
      return next;
    });
  };
  // 密码框是否被用户动过（API 不回传密码，未动过=保持原值，不能发 clear）
  const panelPasswordDirtyRef = useRef(false);

  // FPK 下载列表（打开设置/保存目录后同步刷新）
  const loadFpkFiles = useCallback(async () => {
    setFpkLoading(true);
    try {
      const res = await fetchFpkDownloads();
      setFpkFiles(res.files || []);
      setFpkDir(res.dir || '');
    } catch { /* 目录不存在等场景：显示空列表 */ }
    finally { setFpkLoading(false); }
  }, []);
  const handleFpkRemove = async (name: string) => {
    if (fpkRemoving) return;
    setFpkRemoving(name);
    try {
      await deleteFpkDownload(name);
      setFpkFiles((prev) => prev.filter((f) => f.name !== name));
      toast.success(`已删除 ${name}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '删除失败');
    } finally {
      setFpkRemoving(null);
    }
  };
  // 手动刷新已下载列表（带 toast 反馈，避免"点了没反应"的观感）
  const handleFpkRefresh = async () => {
    if (fpkLoading) return;
    setFpkLoading(true);
    try {
      const res = await fetchFpkDownloads();
      const files = res.files || [];
      setFpkFiles(files);
      setFpkDir(res.dir || '');
      toast.success(`已刷新：${files.length} 个 FPK`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '刷新失败');
    } finally {
      setFpkLoading(false);
    }
  };
  // 直接安装已下载的 FPK（走 SSE 进度；文件保留在缓存中）
  const handleFpkInstall = (name: string) => {
    if (fpkInstalling) return;
    setFpkInstalling(name);
    setFpkInstallMsg('准备安装...');
    installFpkDownload(name, (ev) => {
      if (ev.step === 'error' || ev.error) return;
      if (ev.message) setFpkInstallMsg(ev.message);
    }).promise
      .then(() => {
        toast.success(`已安装 ${name}`);
        onCatalogChanged?.();
        loadFpkFiles();
      })
      .catch((e: unknown) => {
        const msg = e instanceof Error ? e.message : '安装失败';
        toast.error(msg);
      })
      .finally(() => {
        setFpkInstalling(null);
        setFpkInstallMsg('');
      });
  };

  // 加速源健康监测（智能监测 + 自动切换提示）
  const [mirrorHealth, setMirrorHealth] = useState<MirrorHealth | null>(null);
  const [healthRefreshing, setHealthRefreshing] = useState(false);
  // 手动测速：触发后立即轮询 last_probe，等探测真正完成再给汇总 toast
  // （避免"点了没反应"的观感——旧实现 4s 后就停，探测往往还没跑完）。
  const runManualSpeedTest = useCallback(async (
    label: string,
    fetchFn: (refresh?: boolean) => Promise<MirrorHealth>,
    apply: (h: MirrorHealth) => void,
  ) => {
    const before = await fetchFn();
    apply(before);
    const probeAge = before.last_probe ? Date.now() - new Date(before.last_probe).getTime() : Number.POSITIVE_INFINITY;
    const summarize = (h: MirrorHealth) => {
      const ok = (h.mirrors || []).filter((m) => m.status === 'ok').length;
      const fail = (h.mirrors || []).filter((m) => m.status === 'fail').length;
      return `${ok} 个正常${fail > 0 ? `，${fail} 个失败` : ''}`;
    };
    if (Number.isFinite(probeAge) && probeAge < 60000) {
      toast.success(`${label}：${summarize(before)}（${Math.max(1, Math.round(probeAge / 1000))} 秒前刚测过）`);
      return;
    }
    await fetchFn(true); // ?refresh=1 触发后台立即探测
    const deadline = Date.now() + 45000;
    let latest = before;
    while (Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 2000));
      try {
        latest = await fetchFn();
        apply(latest);
      } catch { break; }
      if (latest.last_probe && latest.last_probe !== before.last_probe) break;
    }
    toast.success(`${label}完成：${summarize(latest)}`);
  }, []);
  const refreshHealth = useCallback(async () => {
    setHealthRefreshing(true);
    try {
      await runManualSpeedTest('GitHub 测速', fetchMirrorHealth, setMirrorHealth);
    } catch {
      toast.error('GitHub 测速失败，请稍后再试');
    } finally {
      setHealthRefreshing(false);
    }
  }, [runManualSpeedTest]);
  useEffect(() => {
    if (!open || tab !== 'system') return;
    let cancelled = false;
    const load = async () => {
      try {
        const h = await fetchMirrorHealth();
        if (!cancelled) setMirrorHealth(h);
      } catch { /* ignore */ }
    };
    load();
    // window.setInterval：组件内 state setter 名为 setInterval，会遮蔽全局函数
    const t = window.setInterval(load, 5000);
    return () => { cancelled = true; window.clearInterval(t); };
  }, [open, tab]);

  // Docker 镜像加速健康监测（智能监测 + 自动切换提示）
  const [dockerMirrorHealth, setDockerMirrorHealth] = useState<MirrorHealth | null>(null);
  const [dkHealthRefreshing, setDkHealthRefreshing] = useState(false);
  const refreshDkHealth = useCallback(async () => {
    setDkHealthRefreshing(true);
    try {
      // ?refresh=1 触发后台立即探测（GitHub/Docker 一起探，last_probe 同款轮询）
      await runManualSpeedTest('Docker 测速', fetchDockerMirrorHealth, setDockerMirrorHealth);
    } catch {
      toast.error('Docker 测速失败，请稍后再试');
    } finally {
      setDkHealthRefreshing(false);
    }
  }, [runManualSpeedTest]);
  useEffect(() => {
    if (!open || tab !== 'system') return;
    let cancelled = false;
    const load = async () => {
      try {
        const h = await fetchDockerMirrorHealth();
        if (!cancelled) setDockerMirrorHealth(h);
      } catch { /* ignore */ }
    };
    load();
    const t = window.setInterval(load, 5000);
    return () => { cancelled = true; window.clearInterval(t); };
  }, [open, tab]);

  // 测速状态 —— GitHub / Docker 各自独立
  const [ghChecking, setGhChecking] = useState(false);
  const [dkChecking, setDkChecking] = useState(false);
  const [ghLatency, setGhLatency] = useState<Map<string, MirrorCheckResult>>(new Map());
  const [dkLatency, setDkLatency] = useState<Map<string, MirrorCheckResult>>(new Map());

  // 记住最后一次非 direct 选择，切回 ON 时恢复
  const prevMirrorRef = useRef<string>('gh-proxy');
  const prevDockerMirrorRef = useRef<string>('daocloud');

  const githubEnabled = mirror !== 'direct';
  const dockerEnabled = dockerMirror !== 'direct';

  // 每次打开重置状态并拉取数据
  useEffect(() => {
    if (!open) return;
    setTab('system');
    setLoading(true);
    setGhLatency(new Map());
    setDkLatency(new Map());
    let cancelled = false;
    const loadData = async () => {
      try {
        const [settings, store] = await Promise.all([
          fetchSettings(),
          fetchStoreUpdate()
        ]);
        if (cancelled) return;
        setInterval(settings.check_interval_hours);
        const m = settings.mirror || 'gh-proxy';
        const dm = settings.docker_mirror || 'daocloud';
        setMirror(m);
        setMirrorOptions(settings.mirror_options || []);
        setDockerMirror(dm);
        setDockerMirrorOptions(settings.docker_mirror_options || []);
        setCustomGithubMirror(settings.custom_github_mirror || '');
        setCustomDockerMirror(settings.custom_docker_mirror || '');
        setInstallVolume(settings.install_volume || 0);
        setVolumeOptions(settings.volume_options || []);
        setDownloadDir(settings.download_dir || '');
        setPanelEnabled(!!settings.panel_enabled);
        setPanelUsername(settings.panel_username || 'fnos');
        setPanelBaseURL(settings.panel_base_url || '');
        setPanelHasPassword(!!settings.panel_has_password);
        setPanelPassword('');
        panelPasswordDirtyRef.current = false;
        if (m !== 'direct') prevMirrorRef.current = m;
        if (dm !== 'direct') prevDockerMirrorRef.current = dm;
        setStoreInfo(store);
      } catch (error) {
        if (cancelled) return;
        console.error('Failed to load settings:', error);
        toast.error('加载设置失败');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    loadData();
    loadFpkFiles();
    return () => { cancelled = true; };
  }, [open, loadFpkFiles]);

  const handleGhSpeedTest = async () => {
    setGhChecking(true);
    setGhLatency(new Map());
    try {
      const result = await checkMirrors('github');
      const gh = new Map<string, MirrorCheckResult>();
      for (const r of result.github_mirrors) gh.set(r.key, r);
      setGhLatency(gh);
    } catch (error) {
      console.error('GitHub speed test failed:', error);
      toast.error('GitHub 测速失败');
    } finally {
      setGhChecking(false);
    }
  };

  const handleDkSpeedTest = async () => {
    setDkChecking(true);
    setDkLatency(new Map());
    try {
      const result = await checkMirrors('docker');
      const dk = new Map<string, MirrorCheckResult>();
      for (const r of result.docker_mirrors) dk.set(r.key, r);
      setDkLatency(dk);
    } catch (error) {
      console.error('Docker speed test failed:', error);
      toast.error('Docker 测速失败');
    } finally {
      setDkChecking(false);
    }
  };

  const handleGithubToggle = (checked: boolean) => {
    if (checked) {
      setMirror(prevMirrorRef.current);
    } else {
      prevMirrorRef.current = mirror;
      setMirror('direct');
    }
  };

  const handleDockerToggle = (checked: boolean) => {
    if (checked) {
      setDockerMirror(prevDockerMirrorRef.current);
    } else {
      prevDockerMirrorRef.current = dockerMirror;
      setDockerMirror('direct');
    }
  };

  const handlePanelTest = async () => {
    setPanelTesting(true);
    try {
      // 表单里填了账号（含密码）→ 用表单值实测；否则用已保存的账号
      const hasFormCreds = panelUsername.trim() !== '' && panelPassword !== '';
      const r = hasFormCreds
        ? await testPanelLogin({ username: panelUsername, password: panelPassword, base_url: panelBaseURL })
        : await testPanelLogin();
      toast.success(`登录成功：官方目录 ${r.app_count} 个应用`);
    } catch (error) {
      console.error('Panel login test failed:', error);
      toast.error(error instanceof Error ? error.message : '登录测试失败');
    } finally {
      setPanelTesting(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSettings({
        check_interval_hours: interval,
        mirror,
        docker_mirror: dockerMirror,
        custom_github_mirror: customGithubMirror || undefined,
        custom_docker_mirror: customDockerMirror || undefined,
        install_volume: installVolume,
        download_dir: downloadDir,
        panel_enabled: panelEnabled,
        panel_username: panelUsername,
        panel_password: panelPassword || undefined,
        panel_base_url: panelBaseURL,
        // 只有用户清空过密码框才显式清除；未动过=保持服务端原值
        panel_clear_password: panelPasswordDirtyRef.current && panelPassword === '',
      });
      toast.success('设置已保存');
      loadFpkFiles();
      onOpenChange(false);
    } catch (error) {
      console.error('Failed to save settings:', error);
      toast.error('保存设置失败');
    } finally {
      setSaving(false);
    }
  };

  const githubSelectOptions = mirrorOptions.filter((opt) => opt.key !== 'direct');
  const dockerSelectOptions = dockerMirrorOptions.filter((opt) => opt.key !== 'direct');

  const mirrorLabel = (key: string) =>
    mirrorOptions.find((o) => o.key === key)?.label || (key === 'custom' ? '自定义' : key);
  const dockerMirrorLabel = (key: string) =>
    dockerMirrorOptions.find((o) => o.key === key)?.label || (key === 'custom' ? '自定义' : key);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* 移动端 = 整页（与单应用详情同构）；桌面端 = 居中宽面板。
          右上角 X 仅桌面端保留（移动端用顶栏 ← 返回）。 */}
      <DialogContent className="inset-0 w-full h-full max-w-none rounded-none translate-x-0 translate-y-0 flex flex-col !p-0 gap-0 overflow-hidden bg-background sm:inset-auto sm:left-[50%] sm:top-[50%] sm:h-[88vh] sm:max-w-3xl sm:translate-x-[-50%] sm:translate-y-[-50%] [&>button.absolute]:hidden sm:[&>button.absolute]:inline-flex">
        {/* 顶栏：← 返回 + 标题 */}
        <div className="flex items-center gap-1 border-b border-border/60 px-2 py-2 shrink-0">
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            onClick={() => onOpenChange(false)}
            aria-label="返回"
            title="返回"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <DialogTitle className="text-[15px] font-semibold tracking-tight">设置</DialogTitle>
        </div>

        <div className="flex-1 min-h-0 flex flex-col">
          {/* 顶部 tab：系统设置 / 应用源设置（分段控件，不占侧边空间，内容区全宽） */}
          <div className="shrink-0 px-4 sm:px-6 pt-3">
            <div className="inline-flex rounded-xl bg-muted/60 p-1" role="tablist">
              {TABS.map(({ key, label, icon: Icon }) => (
                <button
                  key={key}
                  role="tab"
                  aria-selected={tab === key}
                  onClick={() => setTab(key)}
                  className={cn(
                    "h-8 rounded-lg px-4 flex items-center gap-1.5 text-[13px] font-medium transition-colors focus:outline-none",
                    tab === key
                      ? "bg-card text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {label}
                </button>
              ))}
            </div>
          </div>

          {/* 内容区 */}
          <div className="flex-1 min-h-0 min-w-0 overflow-y-auto pt-3">
            {tab === 'source' ? (
              <div className="px-4 py-4 sm:px-6 sm:py-5">
                <SourceManager onCatalogChanged={onCatalogChanged} />
              </div>
            ) : loading ? (
              <div className="flex justify-center py-12">
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
              </div>
            ) : (
              <div className="px-3 py-4 sm:px-6 sm:py-5 space-y-4">
                {/* 常规 */}
                <div className="bg-card rounded-[18px] border border-border/20 shadow-appstore px-4 py-4 space-y-4">
                  <div className="space-y-2">
                    <label className="text-sm font-medium leading-none">
                      自动检查更新间隔
                    </label>
                    <Select value={interval.toString()} onValueChange={(value) => setInterval(Number(value))}>
                      <SelectTrigger>
                        <SelectValue placeholder="选择间隔" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="1">1 小时</SelectItem>
                        <SelectItem value="3">3 小时</SelectItem>
                        <SelectItem value="6">6 小时</SelectItem>
                        <SelectItem value="12">12 小时</SelectItem>
                        <SelectItem value="24">24 小时</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {volumeOptions.length > 0 && (
                    <>
                      <Separator />
                      <div className="space-y-2">
                        <label className="text-sm font-medium leading-none">
                          应用安装位置
                        </label>
                        <Select
                          value={installVolume.toString()}
                          onValueChange={(value) => setInstallVolume(Number(value))}
                        >
                          <SelectTrigger>
                            <SelectValue placeholder="选择存储空间" />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="0">系统默认</SelectItem>
                            {volumeOptions.map((vol) => (
                              <SelectItem key={vol.index} value={vol.index.toString()}>
                                <span className="flex items-center gap-2">
                                  <span>存储空间 {vol.index}</span>
                                  {vol.total_bytes > 0 && (
                                    <span className="text-xs text-muted-foreground">
                                      {formatBytes(vol.free_bytes)} 可用 / {formatBytes(vol.total_bytes)}
                                    </span>
                                  )}
                                </span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <p className="text-xs text-muted-foreground">
                          选择应用安装到哪个存储空间，默认使用系统指定的存储空间
                        </p>
                      </div>
                    </>
                  )}

                </div>

                {/* FPK 下载目录 + 已下载列表（独立卡片，不与常规设置混在一起） */}
                <div className="bg-card rounded-[18px] border border-border/20 shadow-appstore px-4 py-4">
                  <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <label className="text-sm font-medium leading-none flex items-center gap-1.5">
                        <FolderDown className="h-3.5 w-3.5 text-muted-foreground" />
                        FPK 下载目录
                      </label>
                      {/* 顺序与 GitHub/Docker 加速源健康面板一致：刷新在前、折叠在后 */}
                      <div className="flex items-center gap-0.5">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6"
                          onClick={handleFpkRefresh}
                          disabled={fpkLoading}
                          title="刷新已下载 FPK 列表"
                          aria-label="刷新已下载列表"
                        >
                          <RefreshCw className={`h-3.5 w-3.5 ${fpkLoading ? 'animate-spin text-primary' : ''}`} />
                        </Button>
                        {fpkFiles.length > 0 && (
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                            onClick={toggleFpkList}
                            title={fpkListCollapsed ? '展开已下载列表' : '折叠已下载列表'}
                            aria-label={fpkListCollapsed ? '展开已下载列表' : '折叠已下载列表'}
                          >
                            <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", fpkListCollapsed && "-rotate-90")} />
                          </Button>
                        )}
                      </div>
                    </div>
                    <Input
                      value={downloadDir}
                      onChange={(e) => setDownloadDir(e.target.value)}
                      placeholder="留空使用系统默认目录"
                      className="h-9 text-xs font-mono"
                    />
                    {fpkDir && (
                      <p className="truncate font-mono text-[11px] text-muted-foreground" title={fpkDir}>
                        当前生效：{fpkDir}
                      </p>
                    )}
                    <p className="text-xs text-muted-foreground">
                      安装/更新与「下载 fpk」的 FPK 都缓存在此目录，已下载列表可在这里查看与管理
                    </p>
                    {fpkFiles.length > 0 && (fpkListCollapsed ? (
                      <button
                        type="button"
                        onClick={toggleFpkList}
                        className="w-full rounded-lg border border-border/40 px-3 py-2 text-left text-[11px] text-muted-foreground hover:text-foreground"
                      >
                        已下载 {fpkFiles.length} 个 FPK · 点击展开
                      </button>
                    ) : (
                      <div className="max-h-44 overflow-y-auto rounded-lg border border-border/40 divide-y divide-border/40">
                        {fpkFiles.map((f) => (
                          <div key={f.name} className="flex items-center gap-2 px-3 py-1.5">
                            <div className="min-w-0 flex-1">
                              <div className="truncate text-xs font-medium" title={f.name}>
                                {f.name}
                              </div>
                              <div className="text-[11px] text-muted-foreground">
                                {fpkInstalling === f.name && fpkInstallMsg
                                  ? <span className="text-primary">{fpkInstallMsg}</span>
                                  : `${formatBytes(f.size)} · ${f.mod_at ? new Date(f.mod_at).toLocaleString() : ''}`
                                }
                              </div>
                            </div>
                            {fpkInstalling === f.name ? (
                              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-primary" />
                            ) : f.installed ? (
                              <span
                                className="shrink-0 rounded-full bg-muted/80 px-2 h-6 inline-flex items-center text-[11px] font-medium text-muted-foreground"
                                title="该应用当前已安装"
                              >
                                已安装
                              </span>
                            ) : (
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-6 shrink-0 px-2 text-xs text-primary hover:bg-primary/10"
                                onClick={() => handleFpkInstall(f.name)}
                                disabled={!!fpkInstalling}
                                title="直接安装该 FPK（不重新下载）"
                              >
                                安装
                              </Button>
                            )}
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-6 w-6 shrink-0 p-0 text-muted-foreground hover:text-red-500"
                              onClick={() => handleFpkRemove(f.name)}
                              disabled={fpkRemoving === f.name || !!fpkInstalling}
                              title="删除该 FPK 缓存"
                              aria-label={`删除 ${f.name}`}
                            >
                              {fpkRemoving === f.name
                                ? <Loader2 className="h-3 w-3 animate-spin" />
                                : <Trash2 className="h-3 w-3" />}
                            </Button>
                          </div>
                        ))}
                      </div>
                    ))}
                  </div>
                </div>

                {/* 下载加速 */}
                <div className="bg-card rounded-[18px] border border-border/20 shadow-appstore px-4 py-4 space-y-5">
                  <div className="space-y-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium leading-none whitespace-nowrap">
                          GitHub 下载加速
                        </span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground"
                          onClick={handleGhSpeedTest}
                          disabled={ghChecking}
                          title="测速"
                          aria-label="GitHub 测速"
                        >
                          {ghChecking ? (
                            <Loader2 className="h-3 w-3 animate-spin sm:mr-1" />
                          ) : (
                            <Zap className="h-3 w-3 sm:mr-1" />
                          )}
                          <span className="hidden sm:inline">测速</span>
                        </Button>
                      </div>
                      <Switch checked={githubEnabled} onCheckedChange={handleGithubToggle} />
                    </div>
                    {githubEnabled && (
                      <>
                        <Select value={mirror} onValueChange={(value) => setMirror(value)}>
                          <SelectTrigger>
                            <SelectValue placeholder="选择镜像" />
                          </SelectTrigger>
                          <SelectContent>
                            {githubSelectOptions.map((opt) => {
                              const result = ghLatency.get(opt.key);
                              return (
                                <SelectItem key={opt.key} value={opt.key}>
                                  <span className="flex items-center justify-between w-full gap-2">
                                    <span>{opt.label}</span>
                                    {result && (
                                      <span className={`text-[11px] tabular-nums ${latencyColor(result)}`}>
                                        {latencyText(result)}
                                      </span>
                                    )}
                                  </span>
                                </SelectItem>
                              );
                            })}
                          </SelectContent>
                        </Select>
                        {mirror === 'custom' && (
                          <Input
                            placeholder="https://your-proxy.example.com/"
                            value={customGithubMirror}
                            onChange={(e) => setCustomGithubMirror(e.target.value)}
                          />
                        )}
                      </>
                    )}
                    <p className="text-xs text-muted-foreground">
                      {githubEnabled ? '使用镜像加速从 GitHub 下载应用安装包' : '直接从 GitHub 下载，不使用加速'}
                    </p>
                    <MirrorHealthPanel
                      title="GitHub 加速源健康"
                      health={mirrorHealth}
                      options={mirrorOptions}
                      customConfigured={customGithubMirror !== ''}
                      labelOf={mirrorLabel}
                      refreshing={healthRefreshing}
                      onRefresh={refreshHealth}
                    />
                  </div>

                  <Separator />

                  <div className="space-y-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium leading-none whitespace-nowrap">
                          Docker 镜像加速
                        </span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground"
                          onClick={handleDkSpeedTest}
                          disabled={dkChecking}
                          title="测速"
                          aria-label="Docker 测速"
                        >
                          {dkChecking ? (
                            <Loader2 className="h-3 w-3 animate-spin sm:mr-1" />
                          ) : (
                            <Zap className="h-3 w-3 sm:mr-1" />
                          )}
                          <span className="hidden sm:inline">测速</span>
                        </Button>
                      </div>
                      <Switch checked={dockerEnabled} onCheckedChange={handleDockerToggle} />
                    </div>
                    {dockerEnabled && (
                      <>
                        <Select value={dockerMirror} onValueChange={(value) => setDockerMirror(value)}>
                          <SelectTrigger>
                            <SelectValue placeholder="选择镜像" />
                          </SelectTrigger>
                          <SelectContent>
                            {dockerSelectOptions.map((opt) => {
                              const result = dkLatency.get(opt.key);
                              return (
                                <SelectItem key={opt.key} value={opt.key}>
                                  <span className="flex items-center justify-between w-full gap-2">
                                    <span>{opt.label}</span>
                                    {result && (
                                      <span className={`text-[11px] tabular-nums ${latencyColor(result)}`}>
                                        {latencyText(result)}
                                      </span>
                                    )}
                                  </span>
                                </SelectItem>
                              );
                            })}
                          </SelectContent>
                        </Select>
                        {dockerMirror === 'custom' && (
                          <Input
                            placeholder="your-mirror.example.com/"
                            value={customDockerMirror}
                            onChange={(e) => setCustomDockerMirror(e.target.value)}
                          />
                        )}
                      </>
                    )}
                    <p className="text-xs text-muted-foreground">
                      {dockerEnabled ? 'Docker 类应用拉取镜像时使用的加速源' : '直接从 Docker Hub 拉取，不使用加速'}
                    </p>
                    <MirrorHealthPanel
                      title="Docker 加速源健康"
                      health={dockerMirrorHealth}
                      options={dockerMirrorOptions}
                      customConfigured={customDockerMirror !== ''}
                      labelOf={dockerMirrorLabel}
                      refreshing={dkHealthRefreshing}
                      onRefresh={refreshDkHealth}
                    />
                  </div>
                </div>

                {/* 官方应用中心 */}
                <div className="bg-card rounded-[18px] border border-border/20 shadow-appstore px-4 py-4 space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium leading-none">官方应用中心</span>
                    <Switch checked={panelEnabled} onCheckedChange={setPanelEnabled} />
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    开启后直连本机系统官方应用中心，可浏览并安装全部官方应用。
                  </p>
                  {panelEnabled && (
                    <>
                      <Separator />
                      <div className="space-y-2">
                        <label className="text-sm font-medium leading-none">
                          面板账号
                        </label>
                        <Input
                          placeholder="Web 面板登录账号，如 fnos"
                          value={panelUsername}
                          onChange={(e) => setPanelUsername(e.target.value)}
                        />
                      </div>
                      <div className="space-y-2">
                        <label className="text-sm font-medium leading-none">
                          面板密码
                        </label>
                        <Input
                          type="password"
                          placeholder={panelHasPassword ? '已设置，留空保持不变' : '面板登录密码'}
                          value={panelPassword}
                          onChange={(e) => {
                            setPanelPassword(e.target.value);
                            panelPasswordDirtyRef.current = true;
                          }}
                        />
                      </div>
                      <p className="text-xs text-muted-foreground">
                        仅浏览/安装官方应用时需要，填写一次自动记住
                      </p>
                      <div className="rounded-lg border border-border/30">
                        <button
                          onClick={togglePanelAdvanced}
                          className="w-full flex items-center justify-between px-3 py-2 text-xs text-muted-foreground hover:text-foreground transition-colors"
                        >
                          <span>高级（面板地址，一般不用填）</span>
                          <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", panelAdvancedOpen && "rotate-180")} />
                        </button>
                        {panelAdvancedOpen && (
                          <div className="px-3 pb-3 space-y-2">
                            <Input
                              placeholder="留空 = 本机面板（http://127.0.0.1:5666）"
                              value={panelBaseURL}
                              onChange={(e) => setPanelBaseURL(e.target.value)}
                            />
                          </div>
                        )}
                      </div>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handlePanelTest}
                        disabled={panelTesting}
                        className="rounded-full px-3.5 h-7 text-xs font-medium"
                      >
                        {panelTesting ? (
                          <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Zap className="mr-1 h-3.5 w-3.5" />
                        )}
                        测试登录
                      </Button>
                    </>
                  )}
                </div>

                {/* 商店 */}
                <div className="bg-card rounded-[18px] border border-border/20 shadow-appstore px-4 py-4 space-y-3">
                  <h3 className="text-sm font-medium text-muted-foreground">商店版本</h3>
                  <div className="flex items-center justify-between gap-2 flex-wrap">
                    <div className="flex items-center gap-2 text-sm flex-wrap">
                      <span className="text-muted-foreground">当前版本:</span>
                      <span className="font-medium">{storeInfo?.current_version || '未知'}</span>
                      {storeInfo?.has_update && (
                        <Badge variant="secondary" className="bg-primary/10 text-primary border-0 font-medium px-1.5 h-5 text-[11px] rounded-full">
                          v{storeInfo.available_version} 可用
                        </Badge>
                      )}
                    </div>
                    {storeInfo?.has_update && onStoreUpdate && (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => { onOpenChange(false); onStoreUpdate(); }}
                        className="rounded-full px-3.5 h-7 text-xs font-medium border-primary text-primary hover:bg-primary/10 shrink-0"
                      >
                        <RefreshCw className="mr-1 h-3.5 w-3.5" />
                        更新商店
                      </Button>
                    )}
                  </div>
                </div>

                {/* 保存：底部悬浮 dock（参考 fn-knock FloatingActionDock：
                    磨砂玻璃圆角条 + 阴影 + safe-area，悬浮在内容上方不挤占布局） */}
                <div className="sticky bottom-0 z-10 -mx-1 mt-2 px-1 pb-[calc(env(safe-area-inset-bottom)+0.5rem)] pt-2">
                  <div className="pointer-events-none mx-auto w-full sm:w-72">
                    <div className="pointer-events-auto rounded-2xl border border-border/40 bg-card/85 p-2 shadow-2xl shadow-black/10 backdrop-blur-xl">
                      <Button
                        className="w-full h-10 rounded-xl"
                        onClick={handleSave}
                        disabled={saving}
                      >
                        {saving && <Loader2 className="-ml-1 mr-2 h-4 w-4 animate-spin" />}
                        保存
                      </Button>
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default SettingsPage;
