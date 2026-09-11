import request from './request'
import type { RawLogDownloadTicket } from '@/utils/rawLogDownload'

export const taskApi = {
  list(params?: { keyword?: string; status?: number | string; label?: string; page?: number; page_size?: number; filters?: string; sort_rules?: string; all?: 0 | 1 }) {
    return request.get('/tasks', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  // push_scope 见 @/api/notification 的 NotifyPushScope：
  // 'bound' 的渠道不参与广播，任务不显式选中它就一条通知都收不到，任务表单需要据此打标。
  notificationChannels() {
    return request.get('/tasks/notification-channels') as Promise<{ data: { id: number; name: string; type: string; enabled: boolean; push_scope?: string }[] }>
  },

  create(data: any) {
    return request.post('/tasks', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: any) {
    return request.put(`/tasks/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/tasks/${id}`) as Promise<{ message: string }>
  },

  run(id: number) {
    return request.put(`/tasks/${id}/run`) as Promise<{ message: string }>
  },

  stop(id: number) {
    return request.put(`/tasks/${id}/stop`) as Promise<{ message: string }>
  },

  enable(id: number) {
    return request.put(`/tasks/${id}/enable`) as Promise<{ message: string; data: any }>
  },

  disable(id: number) {
    return request.put(`/tasks/${id}/disable`) as Promise<{ message: string; data: any }>
  },

  pin(id: number) {
    return request.put(`/tasks/${id}/pin`) as Promise<{ message: string }>
  },

  unpin(id: number) {
    return request.put(`/tasks/${id}/unpin`) as Promise<{ message: string }>
  },

  // 列表拖拽排序：把 sourceId 挪到 targetId 前面（position='after' 时挪到后面）；
  // targetId 留空 = 移到本区末尾。这里的「区」= 置顶状态相同且状态分组（启用/禁用）相同的一批任务，
  // 跨区拖后端直接回 400，前端在 onEnd 里已经先拦过一道。
  // ⚠️ 它改的是 list_order（列表展示顺序），不是 sort_order（开机任务的执行顺序），两者互不影响。
  sort(sourceId: number, targetId?: number, position?: 'before' | 'after') {
    return request.put('/tasks/sort', { source_id: sourceId, target_id: targetId, position }) as Promise<{ message: string }>
  },

  copy(id: number) {
    return request.post(`/tasks/${id}/copy`) as Promise<{ message: string; data: any }>
  },

  // 清除订阅锁：任务重新跟随订阅源的名称与定时，下次拉取会被订阅源的值覆盖回来
  restoreSubscriptionDefault(id: number) {
    return request.put(`/tasks/${id}/restore-subscription-default`) as Promise<{ message: string; data: any }>
  },

  // signal 是给「并行预取」用的（LogViewer 打开弹窗时会先发一份，见 issue #109-1）：
  // 一旦流里来了实时数据，那份预取就作废了 —— 但如果不 abort，axios 仍会把响应体下完
  // 并在主线程 JSON.parse。运行中的大日志任务这一份可能是几十 MB，正好压在
  // 「弱机 + 大日志」这个本来要优化的场景上。不传 signal 时行为与以前完全一致。
  latestLog(id: number, signal?: AbortSignal) {
    return request.get(`/tasks/${id}/latest-log`, { signal }) as Promise<any>
  },

  liveLogs(id: number) {
    return request.get(`/tasks/${id}/live-logs`) as Promise<{ logs: string[]; done: boolean; status: number }>
  },

  logFiles(id: number) {
    return request.get(`/tasks/${id}/log-files`) as Promise<any[]>
  },

  logFileContent(id: number, filename: string, path?: string) {
    return request.get(`/tasks/${id}/log-files/${encodeURIComponent(filename)}`, { params: path ? { path } : undefined }) as Promise<{ filename: string; content: string }>
  },

  deleteLogFile(id: number, filename: string, path?: string) {
    return request.delete(`/tasks/${id}/log-files/${encodeURIComponent(filename)}`, { params: path ? { path } : undefined }) as Promise<{ message: string }>
  },

  // 换取「下载原始日志文件」的短期票据。真正的文件由浏览器原生下载去拉，不走 axios。
  logFileRawDownloadTicket(id: number, filename: string, path?: string) {
    return request.get(`/tasks/${id}/log-files/${encodeURIComponent(filename)}/raw-ticket`, { params: path ? { path } : undefined }) as Promise<RawLogDownloadTicket>
  },

  // 换取「日志文件夹打包下载」的短期票据，zip 同样由浏览器原生下载去拉。
  // 不传 start/end = 打包该任务的全部日志文件；两端都是 RFC3339，
  // 用 utils/datetime.ts 的 toDateRangeParams 生成，按磁盘 ModTime（列表里的「时间」列）筛。
  // 返回体里的 size 是【未压缩总字节】——流式 zip 事先算不出压缩后大小。
  logArchiveDownloadTicket(id: number, params?: { start?: string; end?: string }) {
    return request.get(`/tasks/${id}/log-files/archive-ticket`, { params }) as Promise<RawLogDownloadTicket & { file_count: number }>
  },

  stats(id: number, days?: number) {
    return request.get(`/tasks/${id}/stats`, { params: { days } }) as Promise<any>
  },

  batch(ids: number[], action: string) {
    return request.put('/tasks/batch', { ids, action }) as Promise<{ message: string; count: number }>
  },

  batchEnable(taskIds: number[]) {
    return request.put('/tasks/batch/enable', { task_ids: taskIds }) as Promise<{ message: string; success_count: number }>
  },

  batchDisable(taskIds: number[]) {
    return request.put('/tasks/batch/disable', { task_ids: taskIds }) as Promise<{ message: string; success_count: number }>
  },

  batchDelete(taskIds: number[]) {
    return request.delete('/tasks/batch/delete', { data: { task_ids: taskIds } }) as Promise<{ message: string; count: number }>
  },

  batchRun(taskIds: number[]) {
    return request.post('/tasks/batch/run', { task_ids: taskIds }) as Promise<{ message: string; count: number }>
  },

  batchAddLabels(taskIds: number[], labels: string[]) {
    return request.put('/tasks/batch/add-labels', { task_ids: taskIds, labels }) as Promise<{ message: string; success_count: number }>
  },

  cleanLogs(days?: number) {
    return request.delete('/tasks/clean-logs', { params: { days } }) as Promise<{ message: string }>
  },

  export() {
    return request.get('/tasks/export') as Promise<{ data: any[] }>
  },

  import(tasks: any[]) {
    return request.post('/tasks/import', { tasks }) as Promise<{ message: string; errors: string[] }>
  },

  cronParse(expression: string) {
    return request.post('/tasks/cron/parse', { expression }) as Promise<any>
  },

  cronTemplates() {
    return request.get('/tasks/cron/templates') as Promise<any[]>
  }
}
