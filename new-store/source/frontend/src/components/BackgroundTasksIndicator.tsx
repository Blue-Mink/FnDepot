import React, { useState, useEffect, useRef } from 'react';
import { fetchTasks, type BackgroundTask } from '../api/client';
import { Progress } from "@/components/ui/progress"
import { formatSpeed } from "@/lib/utils"
import { RefreshCw, CheckCircle2, XCircle, PauseCircle, Download } from 'lucide-react'

// BackgroundTasksIndicator：全局后台任务通知（顶部，3-5 秒自动收起）。
//
// 轮询 GET /api/tasks（每 3s）。任务状态变化（新任务开始 / 暂停 / 完成 / 失败）
// 时在顶部闪现一条通知，4 秒后自动收起（非常驻、不挡操作）。
// 进行中的详细进度见「设置 → FPK 下载列表」；此条只负责轻量提示。
const BackgroundTasksIndicator: React.FC = () => {
  const [tasks, setTasks] = useState<BackgroundTask[]>([]);
  const [visible, setVisible] = useState(false);
  const sigRef = useRef<string>('');
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      try {
        const list = await fetchTasks();
        if (cancelled) return;
        setTasks(list);
        // 状态签名：所有返回任务的 appname:op:status（不含进度，进度变化不重弹）
        const sig = list
          .map((t) => `${t.appname}:${t.op}:${t.status}`)
          .sort()
          .join('|');
        if (sig !== sigRef.current) {
          sigRef.current = sig;
          if (list.length > 0) {
            setVisible(true);
            // 3-5 秒自动收起（取 4s）
            if (timerRef.current) clearTimeout(timerRef.current);
            timerRef.current = setTimeout(() => setVisible(false), 4000);
          }
        }
      } catch {
        // 忽略轮询错误（后端短暂不可用等），下一轮再试
      }
    };
    poll();
    const timer = setInterval(poll, 3000);
    return () => {
      cancelled = true;
      clearInterval(timer);
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  if (!visible || tasks.length === 0) return null;

  const opLabel = (op: string) =>
    op === 'update' ? '更新' : op === 'download' ? '下载' : '安装';

  return (
    <div className="fixed top-14 left-1/2 z-50 -translate-x-1/2 w-[calc(100%-2rem)] max-w-sm space-y-2">
      {tasks.map((t) => {
        const finished = t.status === 'done' || t.status === 'failed';
        const paused = t.status === 'paused';
        const pct = t.total && t.total > 0 && t.downloaded != null
          ? Math.min(100, Math.round((t.downloaded / t.total) * 100))
          : (t.progress ?? 0);
        return (
          <div
            key={`${t.appname}:${t.op}`}
            className="rounded-2xl border border-border/20 bg-card/95 backdrop-blur px-3.5 py-2.5 shadow-appstore"
          >
            <div className="flex items-center gap-2">
              {finished ? (
                t.status === 'done' ? (
                  <CheckCircle2 className="h-4 w-4 text-green-500 shrink-0" />
                ) : (
                  <XCircle className="h-4 w-4 text-red-500 shrink-0" />
                )
              ) : paused ? (
                <PauseCircle className="h-4 w-4 text-amber-500 shrink-0" />
              ) : t.op === 'download' ? (
                <Download className="h-4 w-4 animate-pulse text-blue-500 shrink-0" />
              ) : (
                <RefreshCw className="h-4 w-4 animate-spin text-muted-foreground shrink-0" />
              )}
              <span className="text-sm font-medium truncate">
                {finished
                  ? `后台${opLabel(t.op)} ${t.appname} ${t.status === 'done' ? '完成' : '失败'}`
                  : `后台${opLabel(t.op)} ${t.appname}${paused ? '（已暂停）' : ''}`}
              </span>
              {t.new_version && !finished && (
                <span className="text-xs text-muted-foreground shrink-0">→ {t.new_version}</span>
              )}
              {!finished && !paused && (
                <span className="ml-auto text-xs text-muted-foreground shrink-0">
                  {Math.round(pct)}%
                </span>
              )}
            </div>
            {!finished && !paused && (
              <Progress value={pct} className="mt-1.5 h-1.5 w-full" />
            )}
            {!finished && !paused && t.speed && t.speed > 0 && (
              <div className="mt-1 text-xs text-muted-foreground text-right">
                {formatSpeed(t.speed)}
              </div>
            )}
            {finished && t.message && (
              <div className="mt-1 text-xs text-muted-foreground truncate">{t.message}</div>
            )}
          </div>
        );
      })}
    </div>
  );
};

export default BackgroundTasksIndicator;
