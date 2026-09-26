import React from 'react';
import type { AppInfo, AppOperation } from '../api/client';
import AppCard from './AppCard';
import { PackageSearch, CheckCircle2, RefreshCw, Search } from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';

interface AppListProps {
  apps: AppInfo[];
  loading: boolean;
  onInstall: (app: AppInfo) => void;
  onUpdate: (app: AppInfo) => void;
  onUninstall: (app: AppInfo) => void;
  onDetail: (app: AppInfo) => void;
  onCancelOp?: (app: AppInfo) => void;
  filterType?: string;
  appOperations?: Map<string, AppOperation>;
  searchQuery?: string;
  /** false when this fnOS build cannot update apps without destroying them. */
  upgradeAllowed?: boolean;
  onSourceFilter?: (source: string) => void;
  onAuthorFilter?: (author: string) => void;
  onDistributorFilter?: (distributor: string) => void;
  onControl?: (app: AppInfo, action: 'start' | 'stop') => void;
  controlling?: string | null;
  /** 打开已安装应用的 Web UI（有 web 入口的应用才渲染按钮）。 */
  onOpenApp?: (app: AppInfo) => void;
  /** 搜索框内当前词条（徽章词条叠加多选），命中者渲染选中态。 */
  activeTerms?: string[];
}

const getEmptyMessage = (filterType?: string) => {
  switch (filterType) {
    case 'installed':
      return { icon: CheckCircle2, text: '暂无已安装的应用' };
    case 'update_available':
      return { icon: RefreshCw, text: '所有应用都是最新版本' };
    default:
      return { icon: PackageSearch, text: '暂无可用应用' };
  }
};

const AppList: React.FC<AppListProps> = ({ apps, loading, onInstall, onUpdate, onUninstall, onDetail, onCancelOp, filterType, appOperations, searchQuery, upgradeAllowed, onSourceFilter, onAuthorFilter, onDistributorFilter, onControl, controlling, onOpenApp, activeTerms }) => {
  if (loading) {
    return (
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        {[...Array(6)].map((_, i) => (
          <div key={i} className="flex flex-col space-y-3">
            <Skeleton className="h-[125px] w-full rounded-xl" />
            <div className="space-y-2">
              <Skeleton className="h-4 w-[250px]" />
              <Skeleton className="h-4 w-[200px]" />
            </div>
          </div>
        ))}
      </div>
    );
  }

  if (apps.length === 0) {
    if (searchQuery?.trim()) {
      return (
        <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
          <Search className="h-12 w-12 mb-4 opacity-40" />
          <p className="text-sm">未找到匹配「{searchQuery.trim()}」的应用</p>
        </div>
      );
    }
    const empty = getEmptyMessage(filterType);
    const Icon = empty.icon;
    return (
      <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
        <Icon className="h-12 w-12 mb-4 opacity-40" />
        <p className="text-sm">{empty.text}</p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      {apps.map((app) => (
        <AppCard
          key={app.key || app.appname}
          app={app}
          operation={appOperations?.get(app.appname)}
          onInstall={onInstall}
          onUpdate={onUpdate}
          onUninstall={onUninstall}
          onDetail={onDetail}
          onCancelOp={onCancelOp}
          upgradeAllowed={upgradeAllowed}
          onSourceFilter={onSourceFilter}
          onAuthorFilter={onAuthorFilter}
          onDistributorFilter={onDistributorFilter}
          activeTerms={activeTerms}
          onControl={onControl}
          controlling={controlling}
          onOpenApp={onOpenApp}
        />
      ))}
    </div>
  );
};

// memo：搜索输入（防抖前）/其他无关状态变化时，若 apps 引用与回调未变，
// 跳过整棵卡片树的重新渲染 —— 这是 WebView 输入流畅度的关键。
export default React.memo(AppList);
