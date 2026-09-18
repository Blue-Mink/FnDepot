import React, { useState, useEffect, useCallback } from 'react';
import { fetchSources, addSourcesBatch, removeSource, syncSource, syncSourceList, fetchSettings, updateSettings, type SourceEntry } from '../api/client';
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Loader2, Plus, Trash2, ExternalLink, Link2, RefreshCw, ListTree } from 'lucide-react'
import { toast } from 'sonner'

interface SourceManagerProps {
  /** 源列表变化后通知父组件刷新应用目录 */
  onCatalogChanged?: () => void;
}

const SourceManager: React.FC<SourceManagerProps> = ({ onCatalogChanged }) => {
  const [sources, setSources] = useState<SourceEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [input, setInput] = useState('');
  const [adding, setAdding] = useState(false);
  const [removingId, setRemovingId] = useState<string | null>(null);
  const [syncingId, setSyncingId] = useState<string | null>(null);
  // 内置源列表自动同步（列表地址固定用内置/配置值，界面不再暴露输入框）
  const [listAuto, setListAuto] = useState(true);
  const [syncingList, setSyncingList] = useState(false);
  const [savingList, setSavingList] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetchSources();
      setSources(res.sources || []);
    } catch (e) {
      console.error('Failed to load sources:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    fetchSettings()
      .then((s) => {
        setListAuto(!(s.source_list_disabled ?? false));
      })
      .catch(() => {});
  }, [load]);

  // 保存源列表设置（带上现有设置全量回传，避免覆盖其它配置；
  // 列表地址沿用当前值，界面已不提供修改入口）
  const persistListSettings = useCallback(async (auto?: boolean) => {
    const cur = await fetchSettings();
    await updateSettings({
      check_interval_hours: cur.check_interval_hours,
      mirror: cur.mirror,
      docker_mirror: cur.docker_mirror,
      custom_github_mirror: cur.custom_github_mirror,
      custom_docker_mirror: cur.custom_docker_mirror,
      install_volume: cur.install_volume,
      source_list_url: cur.source_list_url,
      source_list_disabled: !(auto ?? listAuto),
    });
  }, [listAuto]);

  const handleListAutoChange = async (v: boolean) => {
    setListAuto(v);
    setSavingList(true);
    try {
      await persistListSettings(v);
      toast.success(v ? '已开启源列表自动同步' : '已关闭源列表自动同步');
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '保存设置失败');
    } finally {
      setSavingList(false);
    }
  };

  const handleSyncList = async () => {
    setSyncingList(true);
    try {
      // 先落盘当前地址/开关，再触发同步
      try {
        await persistListSettings();
      } catch {
        /* 保存失败不阻断同步 */
      }
      const res = await syncSourceList();
      if (res.added > 0) {
        toast.success(
          `源列表同步完成：${res.fetched} 个条目，新增 ${res.added} 个源` +
            (res.added_names?.length ? `（${res.added_names.join('、')}）` : '')
        );
      } else {
        toast.success(`源列表同步完成：${res.fetched} 个条目，没有新源`);
      }
      if (res.failed > 0) {
        const errs = (res.errors || []).slice(0, 3).join('；');
        toast.warning(`源列表同步：${res.failed} 个地址无效（${errs}${(res.errors || []).length > 3 ? '…' : ''}）`);
      }
      await load();
      onCatalogChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '同步源列表失败');
    } finally {
      setSyncingList(false);
    }
  };

  const lines = input.split('\n').map((l) => l.trim()).filter(Boolean);

  const handleAdd = async () => {
    if (lines.length === 0) {
      toast.error('请输入应用源地址（每行一个）');
      return;
    }
    setAdding(true);
    try {
      const res = await addSourcesBatch(lines.map((url) => ({ url })));
      const ok = res.results.filter((r) => r.ok);
      const fail = res.results.filter((r) => !r.ok);
      if (ok.length > 0) {
        const names = ok.map((r) => `「${r.name}」`).join('、');
        toast.success(`已添加 ${ok.length} 个应用源：${names}`);
      }
      for (const r of fail) {
        toast.error(`${r.url}：${r.error || '添加失败'}`);
      }
      if (ok.length > 0) setInput('');
      await load();
      onCatalogChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '批量添加应用源失败');
    } finally {
      setAdding(false);
    }
  };

  const handleRemove = async (src: SourceEntry) => {
    setRemovingId(src.id);
    try {
      await removeSource(src.id);
      toast.success(`已移除应用源「${src.name}」`);
      await load();
      onCatalogChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '删除应用源失败');
    } finally {
      setRemovingId(null);
    }
  };

  const handleSync = async (src: SourceEntry) => {
    setSyncingId(src.id);
    try {
      const updated = await syncSource(src.id);
      if (updated.error) {
        toast.error(`源「${src.name}」同步失败：${updated.error}`);
      } else {
        toast.success(`源「${src.name}」同步完成，${updated.app_count} 个应用`);
      }
      await load();
      onCatalogChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '同步应用源失败');
    } finally {
      setSyncingId(null);
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">应用源</h3>
        <span className="text-[11px] text-muted-foreground">FnDepot V1/V2</span>
      </div>

      {/* 源列表自动同步 */}
      <div className="space-y-2 rounded-lg border bg-muted/30 p-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-1.5 text-xs font-medium text-foreground">
            <ListTree className="h-3.5 w-3.5 text-muted-foreground" />
            源列表自动同步
          </div>
          <Switch checked={listAuto} onCheckedChange={handleListAutoChange} disabled={savingList || syncingList} title="开启后每次目录检查自动添加列表中的新源" />
        </div>
        <Button
          variant="outline"
          className="h-9 w-full gap-1.5"
          onClick={handleSyncList}
          disabled={syncingList || savingList}
          title="立即抓取内置社区源列表并自动添加新源（单次最多 150 个）"
        >
          <RefreshCw className={`h-4 w-4 ${syncingList ? 'animate-spin text-primary' : ''}`} />
          {syncingList ? '同步中…' : '立即同步源列表'}
        </Button>
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          从内置社区源列表自动发现并添加新应用源；抓取走设置的 GitHub 加速镜像链。
        </p>
      </div>

      {loading ? (
        <div className="flex justify-center py-3">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      ) : sources.length === 0 ? (
        <p className="text-xs leading-relaxed text-muted-foreground">
          暂无外部应用源。添加后，源中的应用会并入商店目录，来源与作者会在应用上标注。
        </p>
      ) : (
        <div className="space-y-2">
          {sources.map((src) => (
            <div key={src.id} className="flex items-center gap-2 rounded-lg border px-3 py-2">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{src.name}</span>
                  {src.app_count > 0 && (
                    <Badge variant="secondary" className="h-5 px-1.5 text-[10px] font-normal">
                      {src.app_count} 个应用
                    </Badge>
                  )}
                  {src.error && (
                    <Badge variant="destructive" className="h-5 px-1.5 text-[10px] font-normal">
                      不可用
                    </Badge>
                  )}
                </div>
                <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="truncate">{src.url}</span>
                  {src.homepage && (
                    <a
                      href={src.homepage}
                      target="_blank"
                      rel="noreferrer"
                      className="shrink-0 hover:text-foreground"
                      title={src.homepage}
                    >
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  )}
                </div>
                {src.error && (
                  <div className="mt-0.5 truncate text-[11px] text-red-500">{src.error}</div>
                )}
              </div>
              {/* 手动同步（圆形箭头，同步中转圈） */}
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 shrink-0 rounded-full p-0 text-muted-foreground hover:text-primary hover:bg-primary/10"
                onClick={() => handleSync(src)}
                disabled={syncingId === src.id || removingId === src.id}
                title="立即同步该应用源"
              >
                <RefreshCw className={`h-3.5 w-3.5 ${syncingId === src.id ? 'animate-spin text-primary' : ''}`} />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 shrink-0 p-0 text-muted-foreground hover:text-red-500"
                onClick={() => handleRemove(src)}
                disabled={removingId === src.id || syncingId === src.id}
                title="移除应用源"
              >
                {removingId === src.id ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Trash2 className="h-3.5 w-3.5" />
                )}
              </Button>
            </div>
          ))}
        </div>
      )}

      <div className="space-y-2 rounded-lg border bg-muted/30 p-3">
        <div className="flex items-center gap-1.5 text-xs font-medium text-foreground">
          <Link2 className="h-3.5 w-3.5 text-muted-foreground" />
          添加应用源
          {lines.length > 1 && (
            <Badge variant="secondary" className="h-4 px-1 text-[10px]">
              {lines.length} 行
            </Badge>
          )}
        </div>
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !adding) {
              e.preventDefault();
              handleAdd();
            }
          }}
          placeholder={'每行一个源地址，回车换行继续输入，例如：\nhttps://github.com/Blue-Mink/FnDepot\nhttps://github.com/SomeAuthor/Apps'}
          rows={3}
          className="w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-xs leading-relaxed placeholder:text-muted-foreground/60 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        />
        <div className="flex items-center justify-between gap-2">
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            FnDepot V1/V2：JSON 直链或 GitHub 仓库。源名自动取仓库作者名。单次最多添加 150 个。
          </p>
          <Button size="sm" onClick={handleAdd} disabled={adding || lines.length === 0} className="h-8 shrink-0">
            {adding ? (
              <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Plus className="mr-1 h-3.5 w-3.5" />
            )}
            {adding ? '验证中…' : `添加${lines.length > 1 ? ` ${lines.length} 个` : ''}`}
          </Button>
        </div>
      </div>
    </div>
  );
};

export default SourceManager;
