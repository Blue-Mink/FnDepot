import React from 'react';
import type { AppInfo, AppOperation } from '../api/client';
import { availableVersionLabel, installedVersionLabel, appWebUrl, appDownloadLabel, sourceLabel, effectiveMaintainer, descriptionPlainText } from '../api/client';
import { cn, formatSpeed, formatProgress } from '@/lib/utils';
import { Progress } from '@/components/ui/progress';
import { Badge } from '@/components/ui/badge';
import {
  Download, Package, Circle, Container, X, BellOff, Globe, User, Check,
} from 'lucide-react';
import { CheckCircle2, RefreshCw as UpdateIcon, Search } from 'lucide-react';
import AppIcon from './AppIcon';

interface AppRowListProps {
  apps: AppInfo[];
  onInstall: (app: AppInfo) => void;
  onUpdate: (app: AppInfo) => void;
  onUninstall: (app: AppInfo) => void;
  onDetail: (app: AppInfo) => void;
  onCancelOp?: (app: AppInfo) => void;
  appOperations?: Map<string, AppOperation>;
  searchQuery?: string;
  filterType?: string;
  /** false when this fnOS build cannot update apps without destroying them. */
  upgradeAllowed?: boolean;
  onSourceFilter?: (source: string) => void;
  onAuthorFilter?: (author: string) => void;
  onDistributorFilter?: (distributor: string) => void;
  /** 搜索框内当前词条（徽章词条叠加多选），命中者渲染选中态。 */
  activeTerms?: string[];
  onControl?: (app: AppInfo, action: 'start' | 'stop') => void;
  controlling?: string | null;
  /** 打开应用 Web UI（与 fnOS 应用中心"打开"按钮同目标） */
  onOpenApp?: (app: AppInfo) => void;
}

const STATUS_TEXT: Record<string, string> = {
  running: '运行中',
  stopped: '已停止',
  starting: '启动中',
  stopping: '停用中',
  installing: '安装中',
  uninstalling: '卸载中',
  updating: '更新中',
  nostart: '系统组件',
};

const statusColor = (s: string) =>
  s === 'running' ? 'text-emerald-500'
    : s === 'stopped' || s === 'stopping' ? 'text-amber-500'
    : s === 'nostart' ? 'text-muted-foreground'
    : 'text-primary';

/**
 * 移动端 App Store 风格列表：
 * iOS grouped table（白色圆角容器 + 发丝线分隔），每行 =
 * 超椭圆图标 + 名称/开发者/描述/版本 + 右侧药丸按钮，
 * 关键信息一屏可见，无需二次点击。
 * 注意：行内应用名用 <span>（非 heading），避免与桌面卡片
 * 的 e2e heading 选择器冲突（桌面布局下本组件 display:none）。
 */
const AppRowList: React.FC<AppRowListProps> = ({
  apps, onInstall, onUpdate, onDetail, onCancelOp, appOperations, searchQuery, filterType, upgradeAllowed = true, onSourceFilter, onAuthorFilter, onDistributorFilter, onOpenApp, activeTerms,
}) => {
  if (apps.length === 0) {
    const emptyText = searchQuery?.trim()
      ? `未找到匹配「${searchQuery.trim()}」的应用`
      : filterType === 'installed' ? '暂无已安装的应用'
      : filterType === 'update_available' ? '所有应用都是最新版本'
      : '暂无可用应用';
    const Icon = searchQuery?.trim() ? Search : filterType === 'installed' ? CheckCircle2 : filterType === 'update_available' ? UpdateIcon : Search;
    return (
      <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
        <Icon className="h-12 w-12 mb-4 opacity-40" />
        <p className="text-sm">{emptyText}</p>
      </div>
    );
  }

  return (
    <div className="bg-card rounded-[18px] overflow-hidden border border-border/20 shadow-appstore">
      {apps.map((app, i) => {
        const operation = appOperations?.get(app.appname);
        const isInstalled = app.installed;
        const canUpdate = isInstalled && app.has_update;
        const downloadLabel = appDownloadLabel(app);
        // "打开"目标：daemon appServiceInfo；无 Web 入口的应用不渲染按钮
        const openUrl = isInstalled ? appWebUrl(app) : null;
        // "打开"药丸：运行中且有 Web 入口；或处于可启动状态
        //（停止时 daemon 不下发 web 字段，启动成功后由 handleOpenApp 重新解析目标）
        const canOpen = isInstalled && !!onOpenApp && (
          !!openUrl || ((app.start_stop ?? true) && app.status !== 'nostart' &&
            (app.status === 'stopped' || app.status === 'starting' || app.status === 'stopping'))
        );
        return (
          <div
            key={app.key || app.appname}
            className={cn(
              "flex items-center gap-3.5 p-4 cursor-pointer transition-colors hover:bg-muted/30 active:bg-muted/50",
              i > 0 && "border-t border-border/40"
            )}
            onClick={() => onDetail(app)}
          >
            {/* 图标 */}
            <AppIcon app={app} className="w-14 h-14 shrink-0" />

            {/* 中部信息 */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="font-semibold text-[16px] leading-tight truncate" title={app.display_name}>
                  {app.display_name}
                </span>
                {app.app_type === 'docker' && (
                  <Container className="h-3.5 w-3.5 text-primary shrink-0" />
                )}
                {canUpdate && (
                  <Badge variant="secondary" className="bg-primary/10 text-primary border-0 font-medium px-1.5 h-5 text-[11px] shrink-0 rounded-full">
                    有更新
                  </Badge>
                )}
                {app.update_ignored && (
                  <Badge variant="secondary" className="bg-muted text-muted-foreground border-0 font-medium px-1.5 h-5 text-[11px] shrink-0 rounded-full gap-0.5">
                    <BellOff className="h-2.5 w-2.5" />已忽略
                  </Badge>
                )}
              </div>

              {/* 所有应用：appname 统一显示在应用名下面 */}
              {app.appname && (
                <span className="text-[13px] text-muted-foreground/80 truncate" title={app.appname}>
                  {app.appname}
                </span>
              )}

              {app.description && (
                <p className="text-[13px] text-muted-foreground/80 leading-snug line-clamp-2 mt-0.5">
                  {descriptionPlainText(app.description)}
                </p>
              )}

              <div className="flex items-center flex-wrap gap-x-1.5 text-xs text-muted-foreground mt-1">
                <span>v{isInstalled ? installedVersionLabel(app) : app.latest_version}</span>
                {canUpdate && (
                  <span className="text-primary">→ v{availableVersionLabel(app)}</span>
                )}
                {downloadLabel && (
                  <>
                    <span className="text-muted-foreground/30">·</span>
                    <span className="inline-flex items-center gap-0.5">
                      <Download className="h-3 w-3" />{downloadLabel}
                    </span>
                  </>
                )}
                {isInstalled && (
                  <>
                    <span className="text-muted-foreground/30">·</span>
                    <span className={cn("inline-flex items-center gap-1", statusColor(app.status))}>
                      <Circle className="h-1.5 w-1.5 fill-current" />
                      {STATUS_TEXT[app.status] || app.status}
                    </span>
                  </>
                )}
              </div>

              {/* 应用源 → 开发者 → 发布者（版本号之下）：与卡片同一款蓝框徽章 + 小地球源图标，
                  可点击过滤；长名称在框内换行不截断；
                  官方→飞牛应用中心源、内置→fnos-store/conversun */}
              {(() => {
                const src = sourceLabel(app);
                const author = effectiveMaintainer(app);
                const dist = app.distributor;
                // 徽章词条已在搜索框（多选叠加）→ 命中徽章渲染选中态（实心 + ✓）
                const aSrc = !!activeTerms && !!src && activeTerms.includes(src);
                const aAuth = !!activeTerms && !!author && activeTerms.includes(author);
                const aDist = !!activeTerms && !!dist && activeTerms.includes(dist);
                const pillBase = "inline-flex items-start gap-1 rounded-full px-2 py-[3px] max-w-full text-[11px] leading-[15px] font-medium transition-colors";
                const pillCls = (active: boolean) => cn(pillBase, active ? "bg-primary text-primary-foreground" : "bg-primary/10 text-primary hover:bg-primary/20");
                return (
                  <div className="flex items-start flex-wrap gap-1 min-w-0 mt-0.5">
                    {src && onSourceFilter && (
                      <button
                        onClick={(e) => { e.stopPropagation(); onSourceFilter(src); }}
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
                        onClick={(e) => { e.stopPropagation(); onAuthorFilter(author); }}
                        className={pillCls(aAuth)}
                        title={aAuth ? `正在筛选「${author}」· 点击清除` : `只看「${author}」开发的应用`}
                      >
                        <User className="h-3 w-3 mt-px shrink-0" />
                        <span className="min-w-0 break-words">{author}</span>
                        {aAuth && <Check className="h-2.5 w-2.5 mt-px shrink-0" />}
                      </button>
                    )}
                    {dist && dist !== author && onDistributorFilter && (
                      <button
                        onClick={(e) => { e.stopPropagation(); onDistributorFilter(dist); }}
                        className={pillCls(aDist)}
                        title={aDist ? `正在筛选「${dist}」· 点击清除` : `只看「${dist}」发布的应用`}
                      >
                        <Package className="h-3 w-3 mt-px shrink-0" />
                        <span className="min-w-0 break-words">{dist}</span>
                        {aDist && <Check className="h-2.5 w-2.5 mt-px shrink-0" />}
                      </button>
                    )}
                  </div>
                );
              })()}

              {/* 进行中的操作：紧凑进度条 */}
              {operation && (
                <div className="mt-2 space-y-1.5">
                  <Progress value={operation.progress} className="h-1" />
                  <div className="flex items-center justify-between text-xs text-muted-foreground tabular-nums">
                    <span className="min-w-0 truncate">
                      {operation.message}
                      {operation.step === 'downloading' && operation.speed != null && operation.speed > 0 && ` · ${formatSpeed(operation.speed)}`}
                      {operation.downloaded != null && operation.total != null && operation.total > 0 && ` · ${formatProgress(operation.downloaded, operation.total)}`}
                    </span>
                    {(operation.step === 'downloading' || operation.step === 'pulling') && operation.cancel && (
                      <button
                        onClick={(e) => { e.stopPropagation(); onCancelOp?.(app); }}
                        className="shrink-0 p-1 -m-1 rounded-full text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        aria-label="取消"
                      >
                        <X className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>

            {/* 右侧操作（App Store 风格：未安装=GET / 有更新=UPDATE / 已安装=OPEN，单一药丸） */}
            {!operation && (
              <div className="shrink-0 flex flex-col items-end gap-1.5" onClick={e => e.stopPropagation()}>
                {!isInstalled ? (
                  <button
                    onClick={() => onInstall(app)}
                    className="pill bg-primary text-primary-foreground h-7 px-4 text-[13px] font-semibold shadow-sm active:opacity-80"
                  >
                    安装
                  </button>
                ) : canUpdate ? (
                  <button
                    onClick={() => onUpdate(app)}
                    disabled={!upgradeAllowed}
                    title={upgradeAllowed ? undefined : '当前 fnOS 版本的更新通道会删除应用数据，请在系统应用中心手动安装 fpk'}
                    className="pill h-7 px-3.5 text-[13px] font-semibold border border-primary/50 text-primary active:bg-primary/10 disabled:border-muted disabled:text-muted-foreground"
                  >
                    {upgradeAllowed ? '更新' : '需手动'}
                  </button>
                ) : canOpen ? (
                  <button
                    onClick={() => onOpenApp(app)}
                    aria-label={`打开 ${app.display_name}`}
                    className="pill bg-primary text-primary-foreground h-7 px-4 text-[13px] font-semibold shadow-sm active:opacity-80"
                  >
                    打开
                  </button>
                ) : null}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};

// memo：搜索输入（防抖前）/其他无关状态变化时，若 apps 引用与回调未变，
// 跳过整棵行列表的重新渲染 —— WebView 输入流畅度的关键。
export default React.memo(AppRowList);
