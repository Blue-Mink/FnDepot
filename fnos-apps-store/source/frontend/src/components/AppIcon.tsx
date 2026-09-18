import React, { useState } from 'react';
import { Package } from 'lucide-react';
import { cn } from "@/lib/utils";
import { apiUrl } from '../api/base';
import type { AppInfo } from '../api/client';

/**
 * 应用图标统一渲染。
 *
 * - 外部源应用（source 非 fnos-apps）走后端图标代理
 *   `/api/apps/{appname@source}/asset?type=icon`：后端先取应用声明的
 *   icon_url，为空/失效时自动按源仓库布局探测 `<appname>/ICON.PNG`
 *   等候选路径，并经 GitHub 镜像链抓取 —— 解决社区源普遍不写
 *   icon_url 或直连 raw 失败导致图标缺失的问题。
 * - 内置目录（fnos-apps）图标本身已是加速后的直链，直接加载。
 * - 加载失败回退占位图标，不出现破图。
 */
const AppIcon: React.FC<{
  app: AppInfo;
  /** 容器尺寸/类（图标与占位框共用） */
  className?: string;
  /** 占位图标类（默认 h-6 w-6） */
  iconClassName?: string;
}> = ({ app, className, iconClassName }) => {
  const [failed, setFailed] = useState(false);
  const isExternal = !!app.source && app.source !== 'fnos-apps';
  const src = isExternal
    ? apiUrl(`/api/apps/${encodeURIComponent(app.key)}/asset?type=icon`)
    : app.icon_url || '';

  if (!src || failed) {
    return (
      <div className={cn("bg-muted/60 squircle flex items-center justify-center text-muted-foreground", className)}>
        <Package className={cn("opacity-40", iconClassName || "h-6 w-6")} />
      </div>
    );
  }
  return (
    <img
      src={src}
      alt={app.display_name}
      loading="lazy"
      onError={() => setFailed(true)}
      className={cn("squircle object-cover bg-muted/40", className)}
    />
  );
};

export default AppIcon;
