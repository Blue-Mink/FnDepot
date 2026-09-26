import React, { useState, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import type { AppInfo, AppOperation, PanelDetailResponse } from '../api/client';
import { availableVersionLabel, installedVersionLabel, assetUrl, appWebUrl, fetchPanelDetail, downloadFpk, sourceLabel, effectiveMaintainer, descriptionPlainText } from '../api/client';
import { toast } from 'sonner';
import { cn } from '@/lib/utils';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import AppIcon from "./AppIcon";
import {
  Package,
  Globe,
  Clock,
  Tag,
  Cpu,
  Network,
  ExternalLink,
  Circle,
  Download,
  RefreshCw,
  BellOff,
  Bell,
  Loader2,
  Play,
  Square,
  Trash2,
  User,
  Images,
  Hash,
  HardDrive,
  FileText,
  X,
  ChevronLeft,
  ChevronRight,
  Check,
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import rehypeSanitize from 'rehype-sanitize';
import DOMPurify from 'dompurify';

/**
 * 判断 README 内容是否为 HTML 文档/片段（而非 Markdown）。
 * 第三方 FnDepot 源里相当一部分 README 直接给 HTML（<p>/<div>…），
 * 走 ReactMarkdown 会把原始标签当纯文本显示出来——那种必须走
 * innerHTML（先经 DOMPurify 消毒）渲染。
 */
// 详情页与应用列表共用的"源/开发者/发布者"蓝框徽章样式（字号两端统一）
const META_PILL = "inline-flex items-start gap-1 rounded-full bg-primary/10 px-2 py-[3px] max-w-full text-[11px] leading-[15px] font-medium text-primary hover:bg-primary/20 transition-colors focus:outline-none focus-visible:outline-none";

/** 描述富文本渲染样式（官方 desc / HTML 第三方 desc 共用；链接=主色+下划线） */
const DESC_RICH_CLS = "text-sm leading-relaxed [&_h1]:text-base [&_h1]:font-semibold [&_h2]:text-sm [&_h2]:font-semibold [&_h3]:text-sm [&_h3]:font-semibold [&_h4]:text-[13px] font-medium [&_p]:my-1.5 [&_b]:font-semibold [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5 [&_li]:my-0.5 [&_img]:max-w-full [&_img]:rounded-lg [&_a]:text-primary [&_a]:underline";

const readmeLooksLikeHtml = (t: string): boolean => {
  const s = (t || '').trimStart();
  if (!s.startsWith('<')) return false;
  const head = s.slice(0, 500);
  const hasBlockTag = /<\/?(?:p|div|br|hr|span|ul|ol|li|h[1-6]|table|thead|tbody|tr|td|th|pre|code|section|article|blockquote|img|a)\b/i.test(head);
  if (!hasBlockTag) return false;
  // 同时存在明显的 Markdown 结构（# 标题 / 加粗 / 列表 / 表格）时按 Markdown 处理
  const hasMarkdown = /^#{1,6}\s+\S|\*\*[^*\n]+\*\*|^[-*]\s+\S|^\d+\.\s+\S|^\|.+\|/m.test(s);
  return !hasMarkdown;
};

// 移动端悬浮返回钮：磨玻璃圆钮贴左缘半露出（磁吸），细线 ‹ 箭头右移完全可见。

interface AppDetailDialogProps {
  app: AppInfo | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onInstall: (app: AppInfo) => void;
  onUpdate: (app: AppInfo) => void;
  onIgnoreUpdate?: (app: AppInfo) => void;
  onUnignoreUpdate?: (app: AppInfo) => void;
  onUninstall?: (app: AppInfo) => void;
  operation?: AppOperation;
  /** 点击源名 → 只看该源的应用 */
  onSourceFilter?: (source: string) => void;
  /** 点击作者 → 只看该作者的应用 */
  onAuthorFilter?: (author: string) => void;
  /** 点击发布者 → 只看该发布者发布的应用 */
  onDistributorFilter?: (distributor: string) => void;
  /** 搜索框内当前词条（徽章词条叠加多选），命中者渲染选中态。 */
  activeTerms?: string[];
  /** 打开应用 Web UI（与 fnOS 应用中心"打开"按钮同机制） */
  onOpenApp?: (app: AppInfo) => void;
  /** 已安装应用启动/停用（与 fnOS 应用中心同步） */
  onControl?: (app: AppInfo, action: 'start' | 'stop') => void;
  /** 正在执行启停操作的应用名（显示转圈） */
  controlling?: string | null;
}

const DetailRow: React.FC<{ icon: React.ElementType; label: string; children: React.ReactNode }> = ({ icon: Icon, label, children }) => (
  <div className="flex items-start gap-3 py-2">
    <Icon className="h-4 w-4 mt-0.5 text-muted-foreground shrink-0" />
    <div className="flex-1 min-w-0">
      <p className="text-xs text-muted-foreground mb-0.5">{label}</p>
      <div className="text-sm text-foreground break-words">{children}</div>
    </div>
  </div>
);

const formatSize = (bytes?: number): string => {
  if (!bytes || bytes <= 0) return '-';
  if (bytes >= 1024 ** 3) return (bytes / 1024 ** 3).toFixed(2) + ' GB';
  if (bytes >= 1024 ** 2) return (bytes / 1024 ** 2).toFixed(1) + ' MB';
  return (bytes / 1024).toFixed(0) + ' KB';
};

const formatDownloads = (n?: number): string => {
  if (!n || n <= 0) return '-';
  if (n >= 10000) return (n / 10000).toFixed(1) + ' 万';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k';
  return String(n);
};

/** 官方应用描述是面板发布者写的 HTML 片段（h3/p/b/br…）。
 *  来源是官方 CDN（可信），但仍做白名单清洗：去 script/style/iframe 等
 *  可执行节点与 on* 事件属性，只保留展示型标签。 */
const sanitizeDescHtml = (html: string): string => {
  try {
    const doc = new DOMParser().parseFromString(html, 'text/html');
    const BAD = new Set(['SCRIPT', 'STYLE', 'IFRAME', 'OBJECT', 'EMBED', 'LINK', 'META', 'FORM', 'INPUT', 'BUTTON']);
    doc.body.querySelectorAll('*').forEach((el) => {
      if (BAD.has(el.tagName)) {
        el.remove();
        return;
      }
      Array.from(el.attributes).forEach((attr) => {
        const name = attr.name.toLowerCase();
        if (name.startsWith('on')) el.removeAttribute(attr.name);
        if ((name === 'href' || name === 'src') && /^\s*javascript:/i.test(attr.value)) {
          el.removeAttribute(attr.name);
        }
      });
    });
    return doc.body.innerHTML;
  } catch {
    return html.replace(/<[^>]*>/g, '');
  }
};

const AppDetailDialog: React.FC<AppDetailDialogProps> = ({ app, open, onOpenChange, onInstall, onUpdate, onIgnoreUpdate, onUnignoreUpdate, onUninstall, operation, onSourceFilter, onAuthorFilter, onDistributorFilter, activeTerms, onOpenApp, onControl, controlling }) => {
  const [readme, setReadme] = useState<string | null>(null);
  const [readmeError, setReadmeError] = useState(false);
  // 官方应用（fnos-official）：列表条目不带描述/截图/发布者，打开详情时
  // 惰性拉一次面板详情补全（描述 HTML、预览截图、发布者、安装体积等）。
  const [panelInfo, setPanelInfo] = useState<PanelDetailResponse | null>(null);
  const isOfficial = app?.source === 'fnos-official';
  const [lightbox, setLightbox] = useState<number | null>(null);
  const [lightboxLoading, setLightboxLoading] = useState(false);
  const [lightboxError, setLightboxError] = useState(false);
  const [lightboxRetry, setLightboxRetry] = useState(0);
  // 「下载 fpk」：SSE 进度（后端下载 FPK 到本地缓存并登记面板官方下载通道）。
  // 进度显示在按钮内（此版 sonner 无 toast.update）。
  const [dlBusy, setDlBusy] = useState(false);
  const [dlPct, setDlPct] = useState<number | null>(null);
  const handleDownloadFpk = () => {
    if (dlBusy || !app) return;
    setDlBusy(true);
    setDlPct(null);
    let doneMsg = '';
    downloadFpk(app.appname, (ev) => {
      if (ev.step === 'done' && ev.message) doneMsg = ev.message;
      if (ev.step === 'downloading' && ev.total && ev.total > 0 && typeof ev.downloaded === 'number') {
        setDlPct(Math.min(99, Math.round((ev.downloaded / ev.total) * 100)));
      }
    }).promise
      .then(() => toast.success(doneMsg || 'FPK 下载完成'))
      .catch((e: unknown) => toast.error(e instanceof Error ? e.message : 'FPK 下载失败'))
      .finally(() => { setDlBusy(false); setDlPct(null); });
  };
  // 灯箱换图动画方向：open=首次打开(缩放进入) / next / prev(左右滑入，消除生硬跳切)
  const [lightboxAnim, setLightboxAnim] = useState<'open' | 'next' | 'prev'>('open');
  // 预览轮播：当前可见图索引（按滚动位置更新，驱动圆点/计数/箭头）
  const [previewIndex, setPreviewIndex] = useState(0);
  const carouselRef = useRef<HTMLDivElement>(null);
  // 灯箱滑动切换：pointer 拖拽（触屏/鼠标通用），水平位移足够才翻页
  const lightboxDrag = useRef<{ x: number; y: number; swiping: boolean } | null>(null);
  // 悬浮返回钮（移动端）：磨玻璃圆钮磁吸贴左缘（半露出）—— x 恒锁定左边缘，
  // 只能沿左缘纵向拖动（y 持久化）；静置 = 比背景浅一档的半透白磨玻璃（不影响阅读），
  // 拖动 = 加深为页面背景色 + 微放大；细线 ‹ 箭头右移、露出区内完全可见；轻点 = 返回。
  const BACK_X = -32; // 56px 圆钮露出 24px，紧贴左边缘（加大点击区）
  const [backY, setBackY] = useState<number>(() => {
    try {
      const n = parseInt(localStorage.getItem('detail-back-y') || '', 10);
      if (Number.isFinite(n)) return Math.max(8, Math.min(window.innerHeight - 64, n));
    } catch { /* ignore */ }
    return Math.max(8, Math.round((window.innerHeight - 56) / 2)); // 默认垂直居中
  });
  const [backDragging, setBackDragging] = useState(false);
  const backDragRef = useRef<{ sy: number; oy: number; moved: boolean } | null>(null);
  const moveBackY = (ny: number) => {
    setBackY(ny);
    try { localStorage.setItem('detail-back-y', String(ny)); } catch { /* ignore */ }
  };

  // 灯箱切换图片：重置加载/错误态 + 预加载下一张（弱网下少一次白等）
  // 注意：previewCount/previewSrc 在下方 early-return 之后才声明，这里自包含计算。
  useEffect(() => {
    if (lightbox == null || !app) return;
    setLightboxLoading(true);
    setLightboxError(false);
    const official = app.source === 'fnos-official';
    const poster = official ? (panelInfo?.app.appDetail?.poster ?? []) : [];
    const pc = official ? poster.length : (app.preview_count || 0);
    if (pc > 1) {
      const next = new Image();
      next.src = official
        ? (poster[(lightbox + 1) % pc] ?? '')
        : assetUrl(app.key || app.appname, 'preview', (lightbox + 1) % pc);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lightbox, app?.key, open, panelInfo]);

  // 切换应用时预览轮播回第一张
  useEffect(() => {
    setPreviewIndex(0);
    carouselRef.current?.scrollTo({ left: 0 });
  }, [app?.key]);

  // 轮播滚动 → 以容器中线最近的一张为当前页
  const onCarouselScroll = () => {
    const el = carouselRef.current;
    if (!el) return;
    const mid = el.scrollLeft + el.clientWidth / 2;
    let best = 0;
    let bestDist = Infinity;
    Array.from(el.children).forEach((child, i) => {
      const c = child as HTMLElement;
      const d = Math.abs(c.offsetLeft + c.offsetWidth / 2 - mid);
      if (d < bestDist) { bestDist = d; best = i; }
    });
    setPreviewIndex(best);
  };
  const scrollToPreview = (i: number) => {
    const el = carouselRef.current;
    if (!el) return;
    const target = el.children[Math.max(0, Math.min((el.children.length || 1) - 1, i))] as HTMLElement | undefined;
    target?.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' });
  };

  // 切换应用时重新拉官方详情（描述/截图/发布者）
  useEffect(() => {
    if (!app || !open || app.source !== 'fnos-official') {
      setPanelInfo(null);
      return;
    }
    let cancelled = false;
    setPanelInfo(null);
    fetchPanelDetail(app.appname)
      .then((d) => { if (!cancelled) setPanelInfo(d); })
      .catch(() => { if (!cancelled) setPanelInfo(null); }); // 失败不影响基础信息展示
    return () => { cancelled = true; };
  }, [app?.key, open, app?.source]);

  // 切换应用时重新拉 README
  useEffect(() => {
    if (!app || !open || !app.has_readme) {
      setReadme(null);
      setReadmeError(false);
      return;
    }
    let cancelled = false;
    setReadme(null);
    setReadmeError(false);
    fetch(assetUrl(app.key || app.appname, 'readme'))
      .then((r) => {
        if (!r.ok) throw new Error(r.statusText);
        return r.text();
      })
      .then((text) => { if (!cancelled) setReadme(text); })
      .catch(() => { if (!cancelled) setReadmeError(true); });
    return () => { cancelled = true; };
  }, [app?.key, open, app?.has_readme]);

  if (!app) return null;

  const isInstalled = app.installed;
  const canUpdate = isInstalled && app.has_update;
  // 预览图源：官方应用用面板详情的 poster（CDN 直链），其余用本地 asset 通道
  const posterUrls: string[] = isOfficial ? (panelInfo?.app.appDetail?.poster ?? []) : [];
  const previewCount = isOfficial ? posterUrls.length : (app.preview_count || 0);
  const previewSrc = (i: number): string =>
    isOfficial ? posterUrls[i] : assetUrl(app.key || app.appname, 'preview', i);
  // 打开目标（daemon appServiceInfo）；仅运行中的应用提供"打开"
  const openUrl = isInstalled ? appWebUrl(app) : null;
  const canControl = isInstalled && !!onControl && (app.start_stop ?? true) && app.status !== 'nostart';
  const controlBusy = app.status === 'starting' || app.status === 'stopping';

  // 主操作：App Store「GET」位 —— 与应用图标同一排（不再放页面最底部）
  const primaryPill = operation ? (
    <button disabled className="h-9 min-w-[84px] px-4 rounded-full bg-primary text-primary-foreground text-[13px] font-semibold flex items-center justify-center gap-1.5 opacity-80">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
      处理中
    </button>
  ) : !isInstalled ? (
    <Button onClick={() => { onOpenChange(false); onInstall(app); }} className="h-9 px-5 rounded-full text-[13px] font-semibold shadow-sm">
      <Download className="mr-1.5 h-3.5 w-3.5" />
      安装
    </Button>
  ) : canUpdate ? (
    <Button onClick={() => { onOpenChange(false); onUpdate(app); }} className="h-9 px-5 rounded-full text-[13px] font-semibold shadow-sm">
      <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
      更新
    </Button>
  ) : isInstalled && onOpenApp && openUrl && app.status === 'running' ? (
    <Button onClick={() => onOpenApp(app)} className="h-9 px-5 rounded-full text-[13px] font-semibold shadow-sm">
      <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
      打开
    </Button>
  ) : isInstalled && canControl && !controlBusy ? (
    <Button variant="outline" onClick={() => onControl?.(app, app.status === 'running' ? 'stop' : 'start')} className="h-9 px-5 rounded-full text-[13px] font-semibold">
      {app.status === 'running' ? (
        <><Square className="mr-1.5 h-3 w-3 fill-current" />停用</>
      ) : (
        <><Play className="mr-1.5 h-3 w-3 fill-current" />启动</>
      )}
    </Button>
  ) : isInstalled && controlBusy ? (
    <button disabled className="h-9 min-w-[84px] px-4 rounded-full border border-border/60 text-[13px] font-semibold text-muted-foreground flex items-center justify-center gap-1.5">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
      {app.status === 'starting' ? '启动中' : '停用中'}
    </button>
  ) : (
    <button disabled className="h-9 px-5 rounded-full border border-border/60 text-[13px] font-semibold text-muted-foreground">
      已安装
    </button>
  );

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running': return 'text-emerald-500 fill-emerald-500';
      case 'stopped': return 'text-amber-500 fill-amber-500';
      default: return 'text-muted-foreground/40';
    }
  };

  const getStatusText = (status: string) => {
    switch (status) {
      case 'running': return '运行中';
      case 'stopped': return '已停止';
      default: return status || '未安装';
    }
  };

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return '-';
    try {
      return new Date(dateStr).toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return dateStr;
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* 移动端 = 整页展示（左上角返回按钮退回应用列表，App Store 同构）；
          桌面端保持居中对话框 */}
      <DialogContent className="inset-0 w-full h-full max-w-none rounded-none translate-x-0 translate-y-0 flex flex-col !p-0 gap-0 overflow-hidden bg-background sm:inset-auto sm:left-[50%] sm:top-[50%] sm:h-auto sm:max-h-[88vh] sm:max-w-lg sm:translate-x-[-50%] sm:translate-y-[-50%] [&>button.absolute]:top-3 [&>button.absolute]:right-3 [&>button.absolute]:hidden sm:[&>button.absolute]:inline-flex">
        {/* 整页滚动区：移动端把应用的**全部**内容（图标排 + 描述 + 预览 + 信息 +
            说明/README + 操作）包进一张圆角内边框卡（与列表同款）；桌面端卡片样式
            透明化（对话框本身就是容器）。返回走悬浮可拖钮。 */}
        <div className="flex-1 min-h-0 overflow-y-auto px-3 pt-3 sm:px-0 sm:pt-0">
        <div className="bg-card rounded-[18px] border border-border/20 shadow-appstore overflow-hidden sm:bg-transparent sm:rounded-none sm:border-0 sm:shadow-none">
        {/* 头部行：应用信息 + 主操作 GET 位（下载 / 下载fpk 左右排列） */}
        <div className="border-b border-border/60 bg-background px-3 py-3 sm:bg-transparent">
          <DialogHeader className="space-y-0">
          <div className="flex items-center gap-3 sm:pr-7">
            <AppIcon app={app} className="w-12 h-12 rounded-[12px] shrink-0" />
            <div className="flex-1 min-w-0">
              <DialogTitle className="text-base truncate">{app.display_name}</DialogTitle>
              {/* appname 统一显示在应用名下面（与列表同款） */}
              {app.appname && (
                <div className="text-[13px] text-muted-foreground/80 truncate" title={app.appname}>
                  {app.appname}
                </div>
              )}
              <div className="flex items-center gap-2 mt-1 flex-wrap">
                {isInstalled ? (
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Circle className={`h-2 w-2 ${getStatusColor(app.status)}`} />
                    <span>{getStatusText(app.status)}</span>
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground/50">未安装</span>
                )}
                {canUpdate && (
                  <Badge variant="secondary" className="bg-primary/10 text-primary border-0 font-medium px-1.5 h-5 text-[11px] rounded-full">
                    有更新
                  </Badge>
                )}
                {app.update_ignored && (
                  <Badge variant="secondary" className="bg-muted text-muted-foreground border-0 font-medium px-1.5 h-5 text-[11px] rounded-full gap-0.5">
                    <BellOff className="h-2.5 w-2.5" />
                    已忽略更新
                  </Badge>
                )}
              </div>
            </div>
            {/* 主操作 GET 位 + 下载 fpk：左右排列（靠近图标的是主按钮，后跟下载 fpk）
                官方应用没有可直链的 FPK（走面板 cloud 通道），不显示下载 fpk */}
            <div className="flex items-center gap-2 shrink-0">
              {primaryPill}
              {!isOfficial && (
                <Button
                  onClick={handleDownloadFpk}
                  disabled={dlBusy}
                  size="sm"
                  variant="ghost"
                  className="relative h-9 min-w-[96px] overflow-hidden px-3 text-[13px] font-medium text-muted-foreground hover:text-foreground rounded-full"
                >
                  {dlBusy && dlPct != null && (
                    <span
                      className="absolute inset-y-0 left-0 bg-primary/15 transition-[width] duration-300"
                      style={{ width: `${dlPct}%` }}
                    />
                  )}
                  <span className="relative inline-flex items-center">
                    {dlBusy
                      ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
                      : <Download className="mr-1 h-3.5 w-3.5" />}
                    {dlBusy ? (dlPct != null ? `下载中 ${dlPct}%` : '下载中…') : '下载 fpk'}
                  </span>
                </Button>
              )}
            </div>
          </div>
          {/* 来源 + 开发者/发布者：图标行下方横排，左缘对齐标题/"未安装"列
              （pl = 图标 48px + gap 12px）；三项与列表同一款蓝框徽章（字号统一）、
              小地球源图标、点击过滤；官方→飞牛应用中心源、内置→fnos-store/conversun */}
          <div className="flex items-start gap-2 mt-2 flex-wrap pl-[60px]">
            {(() => {
              const src = sourceLabel(app);
              const author = effectiveMaintainer(app);
              // 徽章词条已在搜索框（多选叠加）→ 命中徽章渲染选中态（实心 + ✓）
              const aSrc = !!activeTerms && !!src && activeTerms.includes(src);
              const aAuth = !!activeTerms && !!author && activeTerms.includes(author);
              const pillCls = (active: boolean) => cn(META_PILL, active && "bg-primary text-primary-foreground");
              return (<>
                {src && onSourceFilter && (
                  <button
                    onClick={() => { onOpenChange(false); onSourceFilter(src); }}
                    className={pillCls(aSrc)}
                    title={aSrc ? `正在筛选「${src}」源 · 点击清除` : `只看「${src}」源的应用`}
                  >
                    <Globe className="h-3 w-3 mt-px shrink-0" />
                    <span className="min-w-0 break-words">{src}</span>
                    {aSrc && <Check className="h-2.5 w-2.5 mt-px shrink-0" />}
                  </button>
                )}
                {author && onAuthorFilter && (
                  <button
                    onClick={() => { onOpenChange(false); onAuthorFilter(author); }}
                    className={pillCls(aAuth)}
                    title={aAuth ? `正在筛选「${author}」· 点击清除` : `只看「${author}」开发的应用`}
                  >
                    <User className="h-3 w-3 mt-px shrink-0" />
                    <span className="min-w-0 break-words">{author}</span>
                    {aAuth && <Check className="h-2.5 w-2.5 mt-px shrink-0" />}
                  </button>
                )}
                {/* 官方应用的开发者同步自面板详情（后台批量回填前，惰性详情兜底） */}
                {!author && isOfficial && panelInfo?.app.appDetail?.maintainer && (
                  <span className="inline-flex items-start gap-1 rounded-full bg-primary/10 px-2 py-[3px] max-w-full text-[11px] leading-[15px] font-medium text-primary">
                    <User className="h-3 w-3 mt-px shrink-0" />
                    <span className="min-w-0 break-words">{panelInfo.app.appDetail.maintainer}</span>
                  </span>
                )}
              </>);
            })()}
            {app.distributor && app.distributor !== effectiveMaintainer(app) && (() => {
              const aDist = !!activeTerms && activeTerms.includes(app.distributor);
              return (
              <button
                onClick={() => { if (onDistributorFilter) { onOpenChange(false); onDistributorFilter(app.distributor!); } }}
                className={cn(META_PILL, aDist && "bg-primary text-primary-foreground")}
                title={onDistributorFilter ? (aDist ? `正在筛选「${app.distributor}」· 点击清除` : `只看「${app.distributor}」发布的应用`) : `发布：${app.distributor}`}
              >
                <Package className="h-3 w-3 mt-px shrink-0" />
                <span className="min-w-0 break-words">发布：{app.distributor}</span>
                {aDist && <Check className="h-2.5 w-2.5 mt-px shrink-0" />}
                {app.distributor_url && (
                  <a href={app.distributor_url} target="_blank" rel="noreferrer" className="inline-flex mt-px hover:text-primary" onClick={(e) => e.stopPropagation()}>
                    <ExternalLink className="h-2.5 w-2.5" />
                  </a>
                )}
              </button>
              );
            })()}
          </div>
          </DialogHeader>
        </div>
        {/* 内容区：描述 / 预览 / 信息 / 更新说明 / README / 次要操作（移动端在卡片内） */}
        <div className="px-4 py-3 sm:px-5">
        {(() => {
          const officialDesc = isOfficial ? panelInfo?.app.appDetail?.desc : undefined;
          if (officialDesc) {
            // 官方描述是 HTML 片段：白名单清洗后按富文本渲染
            return (
              <>
                <DialogDescription
                  className={DESC_RICH_CLS}
                  dangerouslySetInnerHTML={{ __html: sanitizeDescHtml(officialDesc) }}
                />
                <Separator />
              </>
            );
          }
          if (app.description) {
            const d = app.description;
            // 第三方 desc 同样允许 HTML（与官方 desc 同源写法）：像 HTML 则清洗后按富文本
            // 渲染（<a> 超链接可点，如 QQ 群链接），否则纯文本
            if (readmeLooksLikeHtml(d)) {
              return (
                <>
                  <DialogDescription
                    className={DESC_RICH_CLS}
                    dangerouslySetInnerHTML={{ __html: sanitizeDescHtml(d) }}
                  />
                  <Separator />
                </>
              );
            }
            return (
              <>
                <DialogDescription className="text-sm leading-relaxed">
                  {descriptionPlainText(d)}
                </DialogDescription>
                <Separator />
              </>
            );
          }
          return null;
        })()}

        {/* 预览图画廊：App Store 风格大图轮播 —— 手指横滑（touch snap 滚动）/
            桌面端圆点+箭头，点图放大进灯箱（灯箱同样支持左右滑动翻页） */}
        {previewCount > 0 && (
          <>
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <Images className="h-3.5 w-3.5" />
                预览
              </span>
              <span className="tabular-nums">{previewIndex + 1}/{previewCount}</span>
            </div>
            <div className="relative">
              <div
                ref={carouselRef}
                onScroll={onCarouselScroll}
                className="flex gap-3 overflow-x-auto snap-x snap-mandatory scroll-px-4 px-4 -mx-4 sm:px-0 sm:mx-0 py-1.5 no-scrollbar"
              >
                {Array.from({ length: previewCount }, (_, i) => (
                  <button
                    key={i}
                    onClick={() => { setLightboxAnim('open'); setLightbox(i); }}
                    className="snap-center shrink-0 w-[86%] max-w-[340px] sm:w-[76%] sm:max-w-none rounded-2xl overflow-hidden border border-border/40 hover:opacity-90 active:opacity-90 transition-opacity"
                    title="点击放大"
                  >
                    <img
                      src={previewSrc(i)}
                      alt={`${app.display_name} 预览 ${i + 1}`}
                      loading="lazy"
                      draggable={false}
                      className="w-full aspect-[16/10] object-cover bg-muted/40"
                    />
                  </button>
                ))}
              </div>
              {previewCount > 1 && (
                <>
                  <button
                    onClick={() => scrollToPreview(previewIndex - 1)}
                    disabled={previewIndex === 0}
                    className="hidden sm:flex absolute left-0 top-1/2 -translate-y-1/2 h-8 w-8 items-center justify-center rounded-full bg-background/90 border border-border/50 shadow-sm text-foreground hover:bg-background disabled:opacity-0"
                    aria-label="上一张预览"
                  >
                    <ChevronLeft className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => scrollToPreview(previewIndex + 1)}
                    disabled={previewIndex === previewCount - 1}
                    className="hidden sm:flex absolute right-0 top-1/2 -translate-y-1/2 h-8 w-8 items-center justify-center rounded-full bg-background/90 border border-border/50 shadow-sm text-foreground hover:bg-background disabled:opacity-0"
                    aria-label="下一张预览"
                  >
                    <ChevronRight className="h-4 w-4" />
                  </button>
                </>
              )}
            </div>
            {previewCount > 1 && (
              <div className="flex justify-center gap-1 mt-1" aria-hidden>
                {Array.from({ length: previewCount }, (_, i) => (
                  <span key={i} className={cn("h-1.5 rounded-full transition-all", i === previewIndex ? "w-4 bg-foreground/60" : "w-1.5 bg-foreground/20")} />
                ))}
              </div>
            )}
            <Separator />
          </>
        )}

        <div className="space-y-0">
          <DetailRow icon={Tag} label="版本">
            <div className="flex items-center gap-2 flex-wrap">
              <span>{isInstalled ? `v${installedVersionLabel(app)}` : `v${app.latest_version}`}</span>
              {canUpdate && (
                <>
                  <span className="text-muted-foreground">→</span>
                  <span className="text-primary font-medium">v{availableVersionLabel(app)}</span>
                </>
              )}
              {!isInstalled && (
                <span className="text-muted-foreground text-xs">(最新)</span>
              )}
            </div>
          </DetailRow>

          {app.service_port ? (
            <DetailRow icon={Network} label="服务端口">
              {app.service_port}
            </DetailRow>
          ) : null}

          <DetailRow icon={Cpu} label="支持平台">
            {app.platform || '-'}
          </DetailRow>

          {app.download_count ? (
            <DetailRow icon={Download} label="下载次数">
              {formatDownloads(app.download_count)}
            </DetailRow>
          ) : null}

          {(app.size_bytes || (isOfficial && panelInfo?.app.appDetail?.installSize)) ? (
            <DetailRow icon={HardDrive} label="安装包大小">
              {formatSize(app.size_bytes || panelInfo?.app.appDetail?.installSize)}
            </DetailRow>
          ) : null}

          {isOfficial && panelInfo?.app.appDetail?.osMinVersion && (
            <DetailRow icon={Network} label="系统最低版本">
              fnOS {panelInfo.app.appDetail.osMinVersion}
            </DetailRow>
          )}

          {app.sha256 && (
            <DetailRow icon={Hash} label="SHA256">
              <code className="text-xs font-mono break-all text-muted-foreground">{app.sha256}</code>
            </DetailRow>
          )}

          {/* 官方目录不提供更新时间（面板契约无此字段），空值行不显示 */}
          {!isOfficial && app.updated_at && (
            <DetailRow icon={Clock} label="最近更新">
              {formatDate(app.updated_at)}
            </DetailRow>
          )}

          {app.homepage && (
            <DetailRow icon={Globe} label="官网">
              <a
                href={app.homepage}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline inline-flex items-center gap-1 break-all"
              >
                {app.homepage.replace(/^https?:\/\//, '').replace(/\/$/, '')}
                <ExternalLink className="h-3 w-3 shrink-0" />
              </a>
            </DetailRow>
          )}

          {app.release_url && (
            <DetailRow icon={Tag} label="发布页">
              <a
                href={app.release_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline inline-flex items-center gap-1"
              >
                GitHub Release
                <ExternalLink className="h-3 w-3 shrink-0" />
              </a>
            </DetailRow>
          )}
        </div>

        {/* Changelog */}
        {app.changelog && (
          <>
            <Separator />
            <div className="text-xs text-muted-foreground flex items-center gap-1.5">
              <FileText className="h-3.5 w-3.5" />
              更新说明
            </div>
            <p className="text-sm text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">{app.changelog}</p>
          </>
        )}

        {/* README */}
        {app.has_readme && (
          <>
            <Separator />
            <div className="text-xs text-muted-foreground flex items-center gap-1.5">
              <FileText className="h-3.5 w-3.5" />
              README
            </div>
            {readme === null && !readmeError ? (
              <div className="flex items-center gap-2 py-4 text-xs text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                正在加载 README…
              </div>
            ) : readmeError ? (
              <p className="py-2 text-xs text-muted-foreground">README 加载失败（源服务器不可达）</p>
            ) : (
              <div className="markdown-body text-sm leading-relaxed text-foreground/90 prose prose-sm dark:prose-invert max-w-none
                [&_img]:max-w-full [&_img]:rounded-lg [&_h1]:text-lg [&_h2]:text-base [&_h3]:text-sm [&_h1]:mt-4 [&_h1]:mb-2 [&_h2]:mt-3 [&_h2]:mb-1.5 [&_h3]:mt-2 [&_h3]:mb-1
                [&_pre]:bg-muted [&_pre]:rounded-lg [&_pre]:p-3 [&_pre]:overflow-x-auto [&_code]:text-xs
                [&_table]:w-full [&_table]:text-xs [&_th]:border [&_th]:border-border [&_th]:p-1.5 [&_td]:border [&_td]:border-border [&_td]:p-1.5
                [&_a]:text-primary [&_a]:underline [&_a]:break-all
                [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5 [&_li]:my-0.5
                [&_p]:my-2 [&_blockquote]:border-l-4 [&_blockquote]:border-border [&_blockquote]:pl-3 [&_blockquote]:text-muted-foreground [&_hr]:my-4 [&_video]:max-w-full [&_video]:rounded-lg">
                {readme && readmeLooksLikeHtml(readme) ? (
                  /* HTML README：第三方源直接给 HTML，Markdown 渲染会裸露标签，
                     改走 DOMPurify 消毒后的 innerHTML */
                  <div dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(readme, { ADD_ATTR: ['target'] }) }} />
                ) : (
                  /* Markdown README；rehypeRaw+sanitize 让内嵌 HTML 片段也安全渲染 */
                  <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw, rehypeSanitize]}>
                    {readme || ''}
                  </ReactMarkdown>
                )}
              </div>
            )}
          </>
        )}

        <Separator />

        <div className="space-y-3">
          {/* 次要操作：居中一行小药丸（主操作与下载 fpk 已移到图标排，App Store GET 位） */}
          <div className="flex flex-wrap justify-center gap-2">
            {isInstalled && canControl && app.status === 'running' && !controlBusy && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => onControl?.(app, 'stop')}
                disabled={!!operation || controlling !== null}
                aria-label={`停用 ${app.display_name}`}
                className="rounded-full h-8 px-4 text-[13px] font-medium border-border/60 text-foreground hover:text-amber-600 hover:border-amber-500/40 hover:bg-amber-500/5"
              >
                <Square className="mr-1.5 h-3 w-3 fill-current" />
                停用
              </Button>
            )}
            {isInstalled && onUninstall && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => { onOpenChange(false); onUninstall(app); }}
                disabled={!!operation || controlling !== null}
                aria-label={`卸载 ${app.display_name}`}
                className="rounded-full h-8 px-4 text-[13px] font-medium border-border/60 text-foreground hover:text-destructive hover:border-destructive/40 hover:bg-destructive/5"
              >
                <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                卸载
              </Button>
            )}
            {app.update_ignored && onUnignoreUpdate && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => onUnignoreUpdate(app)}
                className="rounded-full px-4 text-muted-foreground"
              >
                <Bell className="mr-1.5 h-3.5 w-3.5" />
                取消忽略
              </Button>
            )}
            {canUpdate && !app.update_ignored && onIgnoreUpdate && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => onIgnoreUpdate(app)}
                className="rounded-full px-4 text-muted-foreground"
              >
                <BellOff className="mr-1.5 h-3.5 w-3.5" />
                忽略更新
              </Button>
            )}
          </div>

          {/* 主操作已移到头部图标排（GET 位）；此处仅保留次要操作 */}
        </div>
        </div>
        </div>
        </div>
        {/* 悬浮返回钮（移动端）：磨玻璃圆钮磁吸贴左缘半露出。
            静置 = 毛玻璃 + 比背景浅一档的半透白（不影响阅读）；拖动 = 加深为背景色 + 微放大。
            x 磁吸贴左缘，只能沿左缘纵向拖动（y 持久化）；轻点 = 返回列表。
            细线 ‹ 箭头右移，在露出区内完全可见。
            必须放在 DialogContent 内部：Radix Dialog 会把对话框外的 DOM 置为
            inert（无法交互）；在内容同层堆叠上下文内 z-40 浮于内容之上。 */}
        <button
          className={`back-wing sm:hidden fixed z-40 h-14 w-14 rounded-full backdrop-blur-xl border shadow-md flex items-center justify-center select-none touch-none transition-[background-color,border-color,box-shadow,transform] duration-200 ease-out ${
            backDragging
              ? 'bg-background border-black/10 dark:border-white/20 shadow-lg text-foreground scale-105'
              : 'bg-white/75 dark:bg-white/10 border-black/5 dark:border-white/10 text-muted-foreground dark:text-white/70'
          }`}
          style={{ left: BACK_X, top: backY }}
          onPointerDown={(e) => {
            (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
            backDragRef.current = { sy: e.clientY, oy: backY, moved: false };
            setBackDragging(true);
          }}
          onPointerMove={(e) => {
            const d = backDragRef.current;
            if (!d) return;
            const dy = e.clientY - d.sy;
            if (!d.moved && Math.abs(dy) <= 6) return;
            d.moved = true;
            // x 磁吸锁定在左缘，只跟随纵向位移
            moveBackY(Math.max(8, Math.min(window.innerHeight - 64, d.oy + dy)));
          }}
          onPointerUp={() => {
            const d = backDragRef.current;
            backDragRef.current = null;
            setBackDragging(false);
            if (!d || d.moved) return;
            // 轻点 = 返回。关掉弹窗后按钮随之卸载，pointerup 之后的 click 事件
            // 可能穿透落到下方的列表行上把详情重新打开 —— 捕获阶段一次性吞掉它。
            // 兜底：若该 click 因目标节点已卸载而未派发，300ms 后移除监听，避免
            // 残留监听误吞用户的下一次点击。注意移除必须带 { capture: true } ——
            // 两参 removeEventListener 的 capture 默认 false，移除不掉 capture
            // 注册的监听（曾导致吞掉用户下一次点击）。
            const suppressClick = (e: Event) => { e.stopPropagation(); e.preventDefault(); };
            document.addEventListener('click', suppressClick, { capture: true, once: true });
            setTimeout(() => document.removeEventListener('click', suppressClick, { capture: true }), 300);
            onOpenChange(false);
          }}
          onPointerCancel={() => { backDragRef.current = null; setBackDragging(false); }}
          aria-label="返回应用列表（可沿左缘上下拖动）"
        >
          {/* 细线 ‹ 箭头（用户指定样式）：右移到露出区(0~24px)偏右、完全可见
              （56px 钮 + ml-9 → 笔画中心落在屏幕 x≈14，露出区中点 12 的右侧） */}
          <ChevronLeft className="ml-9 h-5 w-5" strokeWidth={2.5} />
        </button>
      </DialogContent>

      {/* 预览图灯箱：createPortal 挂到 document.body + z-[100]，
          确保压在 Radix DialogOverlay(z-50) 之上（修复点击被 overlay 拦截的问题） */}
      {lightbox != null && createPortal(
        <div
          className="fixed inset-0 z-[100] bg-black/90 flex items-center justify-center p-4 select-none"
          style={{ zIndex: 100, pointerEvents: 'auto' }}
          // 灯箱 portal 在 Radix Dialog 的 DOM 之外：必须在这里拦掉 pointerdown，
          // 否则灯箱内任何点击都会触发 Radix 的「外部点击关闭对话框」。
          onPointerDown={(e) => e.stopPropagation()}
          onClick={() => setLightbox(null)}
        >
          {/* X 仅桌面端保留（鼠标够得到右上角）；移动端单手用 点按/上下滑 关闭 */}
          <button className="absolute top-4 right-4 z-10 hidden sm:inline-flex p-2 text-white/80 hover:text-white pointer-events-auto" onClick={() => setLightbox(null)} aria-label="关闭">
            <X className="h-6 w-6" />
          </button>
          <button
            className="absolute left-2 sm:left-4 top-1/2 -translate-y-1/2 z-10 hidden sm:inline-flex p-2 text-white/60 hover:text-white disabled:opacity-0 pointer-events-auto"
            disabled={lightbox === 0}
            onClick={(e) => { e.stopPropagation(); setLightboxAnim('prev'); setLightbox((lightbox - 1 + previewCount) % previewCount); }}
            aria-label="上一张"
          >
            <ChevronLeft className="h-8 w-8" />
          </button>
          {/* 灯箱内容区：横向滑动翻页；点按（无位移）= 关闭；纵向滑动
              （|dy|>60 且为主方向）= 关闭 —— 单手无需够右上角 X。
              顶部浅色提示行说明手势，不干扰看图 */}
          <div
            className="relative flex items-center justify-center w-full h-full"
            style={{ pointerEvents: 'auto' }}
            onClick={(e) => e.stopPropagation()}
            onPointerDown={(e) => { lightboxDrag.current = { x: e.clientX, y: e.clientY, swiping: false }; }}
            onPointerMove={(e) => {
              const d = lightboxDrag.current;
              if (!d) return;
              const dx = e.clientX - d.x;
              const dy = e.clientY - d.y;
              if (Math.abs(dx) > 10 && Math.abs(dx) > Math.abs(dy)) d.swiping = true;
            }}
            onPointerUp={(e) => {
              const d = lightboxDrag.current;
              lightboxDrag.current = null;
              if (!d || lightbox == null) return;
              // 点按在按钮上（箭头/X/重试）不触发"点按关闭"
              if (e.target instanceof Element && e.target.closest('button')) return;
              const dx = e.clientX - d.x;
              const dy = e.clientY - d.y;
              // 点按（无位移）= 关闭
              if (Math.abs(dx) < 10 && Math.abs(dy) < 10) { setLightbox(null); return; }
              // 纵向滑动 = 关闭（手指上滑/下滑）
              if (Math.abs(dy) > 60 && Math.abs(dy) > Math.abs(dx)) { setLightbox(null); return; }
              // 横向滑动 = 翻页（带方向滑入动画）
              if (previewCount > 1 && d.swiping && Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.2) {
                setLightboxAnim(dx < 0 ? 'next' : 'prev');
                setLightbox((dx < 0 ? lightbox + 1 : lightbox - 1 + previewCount) % previewCount);
              }
            }}
            onPointerLeave={() => { lightboxDrag.current = null; }}
          >
            <div className="absolute top-3 left-1/2 -translate-x-1/2 text-[13px] tracking-wide text-white/45 pointer-events-none select-none whitespace-nowrap">
              上下滑动可退出 · 左右滑动切换
            </div>
            {lightboxLoading && !lightboxError && (
              <div className="absolute flex flex-col items-center gap-2 text-white/70">
                <Loader2 className="h-8 w-8 animate-spin" />
                <span className="text-xs">预览图加载中…</span>
              </div>
            )}
            {lightboxError ? (
              <div className="flex flex-col items-center gap-3 text-white/80">
                <p className="text-sm">预览图加载失败</p>
                <Button variant="outline" size="sm" className="border-white/40 text-white hover:bg-white/10 hover:text-white"
                  onClick={() => { setLightboxAnim('open'); setLightboxError(false); setLightboxLoading(true); setLightboxRetry((n) => n + 1); }}>
                  重试
                </Button>
              </div>
            ) : (
              <img
                key={`${lightbox}-${lightboxRetry}`}
                src={previewSrc(lightbox) + (lightboxRetry ? `&r=${lightboxRetry}` : '')}
                alt={`${app.display_name} 预览 ${lightbox + 1}`}
                className={`max-w-full max-h-full object-contain rounded-lg transition-opacity duration-150 pointer-events-auto touch-none select-none ${lightboxLoading ? 'opacity-0' : 'opacity-100'} ${
                  lightboxAnim === 'next' ? 'lightbox-anim-next' : lightboxAnim === 'prev' ? 'lightbox-anim-prev' : 'lightbox-anim-open'
                }`}
                onLoad={() => setLightboxLoading(false)}
                onError={() => { setLightboxLoading(false); setLightboxError(true); }}
                draggable={false}
              />
            )}
          </div>
          <button
            className="absolute right-2 sm:right-4 top-1/2 -translate-y-1/2 z-10 hidden sm:inline-flex p-2 text-white/60 hover:text-white disabled:opacity-0 pointer-events-auto"
            disabled={lightbox === previewCount - 1}
            onClick={(e) => { e.stopPropagation(); setLightboxAnim('next'); setLightbox((lightbox + 1) % previewCount); }}
            aria-label="下一张"
          >
            <ChevronRight className="h-8 w-8" />
          </button>
          {previewCount > 1 && (
            <div className="absolute bottom-5 left-1/2 -translate-x-1/2 text-white/70 text-xs tabular-nums pointer-events-none">
              {lightbox + 1} / {previewCount}
            </div>
          )}
        </div>,
        document.body
      )}
    </Dialog>
  );
};

export default AppDetailDialog;
