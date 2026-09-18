import React, { useState, useEffect, useRef, useCallback } from 'react';
import { fetchSettings, updateSettings, fetchStoreUpdate, checkMirrors, fetchMirrorHealth, fetchDockerMirrorHealth, type MirrorOption, type MirrorCheckResult, type VolumeOption, type MirrorHealth } from '../api/client';
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
import { ArrowLeft, ChevronsLeft, ChevronsRight, Database, Loader2, RefreshCw, SlidersHorizontal, Zap, MessageCircle } from 'lucide-react'
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

  return (
    <div className="rounded-xl bg-muted/30 border border-border/20 px-3 py-3">
      <div className="flex items-center justify-between mb-1">
        <div className="flex items-baseline gap-2">
          <span className="text-[13px] font-medium">{title}</span>
          <span className="text-[11px] text-muted-foreground">
            每 {health?.interval_s ? Math.round(health.interval_s / 60) : 5} 分钟自动测速
          </span>
        </div>
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
      </div>
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
  // ── 页内侧栏：两 tab + 展开/收起（持久化） ────────────────────────────
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem('settings-sidebar-collapsed');
      if (saved === 'true') return true;
      if (saved === 'false') return false;
    } catch { /* ignore */ }
    // 默认：移动端收起（68px 图标栏）、桌面端展开
    return window.innerWidth < 768;
  });
  const toggleSidebar = () => {
    setSidebarCollapsed(prev => {
      const next = !prev;
      try { localStorage.setItem('settings-sidebar-collapsed', String(next)); } catch { /* ignore */ }
      return next;
    });
  };
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
  const [storeInfo, setStoreInfo] = useState<StoreUpdateInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  // 加速源健康监测（智能监测 + 自动切换提示）
  const [mirrorHealth, setMirrorHealth] = useState<MirrorHealth | null>(null);
  const [healthRefreshing, setHealthRefreshing] = useState(false);
  const refreshHealth = useCallback(async () => {
    setHealthRefreshing(true);
    try {
      // ?refresh=1 触发后台立即探测（30s 防抖）
      setMirrorHealth(await fetchMirrorHealth(true));
      // 探测需数秒完成，稍后重取一次拿到新结果
      setTimeout(async () => {
        try { setMirrorHealth(await fetchMirrorHealth()); } catch { /* ignore */ }
      }, 4000);
    } catch { /* ignore */ } finally {
      setHealthRefreshing(false);
    }
  }, []);
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
      // ?refresh=1 触发后台立即探测（30s 防抖，GitHub/Docker 一起探）
      setDockerMirrorHealth(await fetchDockerMirrorHealth(true));
      // 探测需数秒完成，稍后重取一次拿到新结果
      setTimeout(async () => {
        try { setDockerMirrorHealth(await fetchDockerMirrorHealth()); } catch { /* ignore */ }
      }, 4000);
    } catch { /* ignore */ } finally {
      setDkHealthRefreshing(false);
    }
  }, []);
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
    return () => { cancelled = true; };
  }, [open]);

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
      });
      toast.success('设置已保存');
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

        <div className="flex-1 min-h-0 flex">
          {/* 页内左侧栏：系统设置 / 应用源设置 两 tab + 展开收起按钮 */}
          <div className={cn(
            "shrink-0 border-r border-border/50 bg-card/40 flex flex-col overflow-hidden transition-all duration-300",
            sidebarCollapsed ? "w-[68px]" : "w-36 sm:w-56"
          )}>
            <div className={cn("p-2 flex items-center shrink-0", sidebarCollapsed ? "justify-center" : "justify-start pl-4")}>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                onClick={toggleSidebar}
                aria-label={sidebarCollapsed ? '展开菜单' : '收起菜单'}
                title={sidebarCollapsed ? '展开菜单' : '收起菜单'}
              >
                {sidebarCollapsed
                  ? <ChevronsRight className="h-4 w-4" />
                  : <ChevronsLeft className="h-4 w-4" />}
              </Button>
            </div>
            <nav className="flex-1 space-y-1 px-2 pb-4">
              {TABS.map(({ key, label, icon: Icon }) => (
                <button
                  key={key}
                  onClick={() => setTab(key)}
                  className={cn(
                    "w-full h-10 rounded-lg flex items-center text-[13px] font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary",
                    sidebarCollapsed ? "justify-center px-0" : "justify-start px-3",
                    tab === key ? "bg-primary text-primary-foreground" : "text-foreground hover:bg-muted"
                  )}
                  aria-label={label}
                  title={label}
                >
                  <Icon className={cn("h-4 w-4 shrink-0", !sidebarCollapsed && "mr-3")} />
                  {!sidebarCollapsed && (
                    <span className="flex-1 text-left whitespace-nowrap">{label}</span>
                  )}
                </button>
              ))}
            </nav>
          </div>

          {/* 内容区 */}
          <div className="flex-1 min-w-0 overflow-y-auto">
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
                      title="加速源健康"
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
                      title="加速源健康"
                      health={dockerMirrorHealth}
                      options={dockerMirrorOptions}
                      customConfigured={customDockerMirror !== ''}
                      labelOf={dockerMirrorLabel}
                      refreshing={dkHealthRefreshing}
                      onRefresh={refreshDkHealth}
                    />
                  </div>
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
                  <button
                    onClick={() => window.open('https://github.com/conversun/fnos-apps/issues/new?template=bug-report.yml', '_blank')}
                    className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
                  >
                    <MessageCircle className="h-3.5 w-3.5" />
                    问题反馈
                  </button>
                </div>

                {/* 保存 */}
                <div className="pt-1">
                  <Button
                    className="w-full sm:w-64 rounded-full h-10"
                    onClick={handleSave}
                    disabled={saving}
                  >
                    {saving && <Loader2 className="-ml-1 mr-2 h-4 w-4 animate-spin" />}
                    保存
                  </Button>
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
