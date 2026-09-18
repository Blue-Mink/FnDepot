import React from 'react';
import type { AppInfo, AppOperation } from '../api/client';
import { availableVersionLabel, installedVersionLabel, appWebUrl, appDownloadLabel } from '../api/client';
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import AppIcon from "./AppIcon";
import { cn, formatSpeed, formatProgress } from "@/lib/utils";
import { 
  Download, 
  RefreshCw, 
  Package,
  Circle,
  ArrowRight,
  Container,
  X,
  BellOff,
  Tag,
  ExternalLink,
} from 'lucide-react';

interface AppCardProps {
  app: AppInfo;
  operation?: AppOperation;
  onInstall: (app: AppInfo) => void;
  onUpdate: (app: AppInfo) => void;
  /** false when this fnOS build cannot update apps without destroying them. */
  upgradeAllowed?: boolean;
  onUninstall?: (app: AppInfo) => void;
  onDetail?: (app: AppInfo) => void;
  onCancelOp?: (app: AppInfo) => void;
  /** 点击源名徽章 → 只看该源的应用 */
  onSourceFilter?: (source: string) => void;
  /** 点击开发者 → 只看该作者的应用 */
  onAuthorFilter?: (author: string) => void;
  /** 点击发布者 → 只看该发布者发布的应用 */
  onDistributorFilter?: (distributor: string) => void;
  /** 已安装应用启动/停用（与 fnOS 应用中心同步） */
  onControl?: (app: AppInfo, action: 'start' | 'stop') => void;
  /** 正在执行启停操作的应用名（显示转圈） */
  controlling?: string | null;
  /** 打开应用 Web UI（与 fnOS 应用中心"打开"按钮同目标） */
  onOpenApp?: (app: AppInfo) => void;
}

const AppCard: React.FC<AppCardProps> = ({ app, operation, onInstall, onUpdate, onDetail, onCancelOp, upgradeAllowed = true, onSourceFilter, onAuthorFilter, onDistributorFilter, onOpenApp }) => {
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

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running': return 'text-emerald-500 fill-emerald-500';
      case 'stopped': return 'text-amber-500 fill-amber-500';
      case 'starting': return 'text-primary animate-pulse';
      case 'stopping': return 'text-amber-500 animate-pulse';
      case 'installing': return 'text-primary animate-pulse';
      case 'uninstalling': return 'text-destructive animate-pulse';
      case 'updating': return 'text-primary animate-pulse';
      default: return 'text-muted-foreground/40';
    }
  };

  const getStatusText = (status: string) => {
    switch (status) {
      case 'running': return '运行中';
      case 'stopped': return '已停止';
      case 'starting': return '启动中';
      case 'stopping': return '停用中';
      case 'installing': return '安装中';
      case 'uninstalling': return '卸载中';
      case 'updating': return '更新中';
      case 'nostart': return '系统组件';
      default: return status || '未知';
    }
  };

  const getStepText = (step: string): string => {
    switch (step) {
      case 'downloading': return '正在下载...';
      case 'pulling': return '正在拉取镜像...';
      case 'installing': return '正在安装...';
      case 'verifying': return '正在验证...';
      case 'starting': return '正在启动...';
      case 'stopping': return '正在停止...';
      case 'uninstalling': return '正在卸载...';
      default: return '处理中...';
    }
  };

  return (
    <Card className={cn(
      "relative overflow-hidden border border-border/20 bg-card shadow-appstore rounded-[18px] transition-all duration-200 hover:shadow-appstore-hover hover:-translate-y-0.5",
      operation && "border-primary/50"
    )}>
      <div className="p-4 flex flex-col h-full gap-3">

        <div className="flex items-start gap-3 cursor-pointer" onClick={() => onDetail?.(app)}>
          <div className="shrink-0">
            <AppIcon app={app} className="w-14 h-14" />
          </div>

          <div className="flex-1 min-w-0 flex flex-col gap-0.5">
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-1.5 min-w-0">
                <h3 className="font-semibold text-[15px] leading-tight text-foreground truncate" title={app.display_name}>
                  {app.display_name}
                </h3>
                {app.app_type === 'docker' && (
                  <Container className="h-3.5 w-3.5 text-primary shrink-0" />
                )}
              </div>
              {canUpdate && (
                <Badge variant="secondary" className="bg-primary/10 text-primary border-0 font-medium px-1.5 h-5 text-[11px] shrink-0 rounded-full">
                  有更新
                </Badge>
              )}
              {app.update_ignored && (
                <Badge variant="secondary" className="bg-muted text-muted-foreground border-0 font-medium px-1.5 h-5 text-[11px] shrink-0 rounded-full gap-0.5">
                  <BellOff className="h-2.5 w-2.5" />
                  已忽略
                </Badge>
              )}
            </div>

            {/* 来源徽章（外部源）+ 开发者行（App Store 风格，可点击过滤） */}
            <div className="flex items-center gap-1.5 min-w-0">
              {/* 仅外部 FnDepot 源应用标注来源徽章；内置目录（fnos-apps）不显示 */}
              {app.source && app.source !== 'fnos-apps' && onSourceFilter && (
                <button
                  onClick={(e) => { e.stopPropagation(); onSourceFilter(app.source!); }}
                  className="shrink-0 inline-flex items-center gap-0.5 rounded-full bg-primary/10 px-1.5 h-[18px] text-[10px] font-medium text-primary hover:bg-primary/20 transition-colors"
                  title={`只看「${app.source}」源的应用`}
                >
                  <Tag className="h-2.5 w-2.5" />
                  <span className="max-w-[90px] truncate">{app.source}</span>
                </button>
              )}
              {app.maintainer && onAuthorFilter ? (
                <button
                  onClick={(e) => { e.stopPropagation(); onAuthorFilter(app.maintainer!); }}
                  className="text-xs text-muted-foreground/80 truncate hover:text-primary transition-colors"
                  title={`只看「${app.maintainer}」开发的应用`}
                >
                  {app.maintainer}
                </button>
              ) : (
                <span className="text-xs text-muted-foreground/80 truncate" title={app.appname}>
                  {app.appname}
                </span>
              )}
              {app.distributor && app.distributor !== app.maintainer && onDistributorFilter && (
                <>
                  <span className="text-muted-foreground/30 shrink-0">·</span>
                  <button
                    onClick={(e) => { e.stopPropagation(); onDistributorFilter(app.distributor!); }}
                    className="inline-flex items-center gap-0.5 text-xs text-muted-foreground/80 truncate hover:text-primary transition-colors shrink-0"
                    title={`只看「${app.distributor}」发布的应用`}
                  >
                    <Package className="h-3 w-3 shrink-0" />
                    <span className="max-w-[90px] truncate">{app.distributor}</span>
                  </button>
                </>
              )}
            </div>

            <div className="flex items-center flex-wrap gap-x-1.5 text-xs text-muted-foreground">
              <span>v{isInstalled ? installedVersionLabel(app) : app.latest_version}</span>
              {canUpdate && (
                <>
                  <ArrowRight className="h-3 w-3 text-muted-foreground/50" />
                  <span className="text-primary">
                    v{availableVersionLabel(app)}
                  </span>
                </>
              )}
              {downloadLabel && (
                <>
                  <span className="text-muted-foreground/30">·</span>
                  <span className="inline-flex items-center gap-0.5">
                    <Download className="h-3 w-3" />
                    {downloadLabel}
                  </span>
                </>
              )}
            </div>
          </div>
        </div>

        {app.description && (
          <p
            className="text-xs text-muted-foreground line-clamp-2 leading-relaxed cursor-pointer hover:text-foreground transition-colors"
            onClick={() => onDetail?.(app)}
            title="点击查看详情"
          >
            {app.description}
          </p>
        )}

        <div className="flex-1" />

        {operation ? (
          <div className="pt-2 border-t border-border/20 space-y-2">
            <Progress value={operation.progress} className="w-full h-1" />
            
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-xs text-muted-foreground min-w-0 tabular-nums">
                <span className="shrink-0">{getStepText(operation.step)}</span>
                {operation.step === 'downloading' && operation.speed != null && operation.speed > 0 ? (
                  <>
                    <span className="shrink-0">{formatSpeed(operation.speed)}</span>
                    {operation.downloaded != null && operation.total != null && operation.total > 0 && (
                      <span className="truncate">{formatProgress(operation.downloaded, operation.total)}</span>
                    )}
                  </>
                ) : (
                  <span>{Math.round(operation.progress)}%</span>
                )}
              </div>
              
              {(operation.step === 'downloading' || operation.step === 'pulling') && operation.cancel && (
                <button
                  onClick={() => onCancelOp?.(app)}
                  className="shrink-0 p-0.5 rounded-full text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                  title="取消"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          </div>
        ) : (
          <div className="flex items-center justify-between gap-2 pt-2 border-t border-border/20">

            <div className="flex items-center min-h-[1.25rem]">
               {isInstalled ? (
                 <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                   <Circle className={`h-2 w-2 ${getStatusColor(app.status)}`} />
                   <span>{getStatusText(app.status)}</span>
                 </div>
               ) : (
                 <span className="text-xs text-muted-foreground/50">
                   未安装
                 </span>
               )}
            </div>

            <div className="flex items-center gap-1.5">
              {/* App Store 风格单一药丸：未安装=安装 / 有更新=更新 / 已安装=打开 */}
              {!isInstalled ? (
                <Button
                  onClick={() => onInstall(app)}
                  className="pill bg-primary text-primary-foreground px-4 h-8 text-[13px] font-semibold shadow-sm hover:opacity-90"
                >
                  <Download className="mr-1 h-3.5 w-3.5" />
                  安装
                </Button>
              ) : canUpdate ? (
                <Button
                  onClick={() => onUpdate(app)}
                  variant="outline"
                  disabled={!upgradeAllowed}
                  title={upgradeAllowed ? undefined : '当前 fnOS 版本的更新通道会删除应用数据，请在系统应用中心手动安装 fpk'}
                  className="pill h-8 px-4 text-[13px] font-semibold border-primary/50 text-primary hover:bg-primary/10 hover:text-primary disabled:border-muted disabled:text-muted-foreground"
                >
                  <RefreshCw className="mr-1 h-3.5 w-3.5" />
                  {upgradeAllowed ? '更新' : '需手动更新'}
                </Button>
              ) : canOpen ? (
                <Button
                  onClick={() => onOpenApp(app)}
                  aria-label={`打开 ${app.display_name}`}
                  className="pill bg-primary text-primary-foreground px-4 h-8 text-[13px] font-semibold shadow-sm hover:opacity-90"
                >
                  <ExternalLink className="mr-1 h-3.5 w-3.5" />
                  打开
                </Button>
              ) : null}
            </div>
          </div>
        )}
      </div>
    </Card>
  );
};

export default AppCard;
