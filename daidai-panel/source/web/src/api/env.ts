import request from './request'

export type EnvPayload = {
  name: string
  value?: string
  remarks?: string
  group?: string
  groups?: string[]
}

/** /envs/names 的一项：变量名 + 全库同名条数（不随当前筛选变化） */
export type EnvNameOption = {
  name: string
  count: number
}

export const envApi = {
  // names 是【精确】匹配的变量名筛选（逗号分隔多值，之间是 OR），与 keyword / groups / enabled 之间是 AND。
  // 与 keyword 的 LIKE 模糊搜索不是一回事：names=JD_COOKIE 不会带出 JD_COOKIE_EXTRA。
  list(params?: { keyword?: string; group?: string; groups?: string; names?: string; enabled?: boolean; page?: number; page_size?: number; all?: 0 | 1 }) {
    return request.get('/envs', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  get(id: number) {
    return request.get(`/envs/${id}`) as Promise<{ data: any }>
  },

  create(data: EnvPayload | EnvPayload[]) {
    return request.post('/envs', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: any) {
    return request.put(`/envs/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/envs/${id}`) as Promise<{ message: string }>
  },

  enable(id: number) {
    return request.put(`/envs/${id}/enable`) as Promise<{ message: string; data: any }>
  },

  disable(id: number) {
    return request.put(`/envs/${id}/disable`) as Promise<{ message: string; data: any }>
  },

  batchDelete(ids: number[]) {
    return request.delete('/envs/batch', { data: { ids } }) as Promise<{ message: string }>
  },

  batchRename(ids: number[], name: string) {
    return request.put('/envs/batch/rename', { ids, name }) as Promise<{ message: string }>
  },

  batchEnable(ids: number[]) {
    return request.put('/envs/batch/enable', { ids }) as Promise<{ message: string }>
  },

  batchDisable(ids: number[]) {
    return request.put('/envs/batch/disable', { ids }) as Promise<{ message: string }>
  },

  batchSetGroup(ids: number[], groups: string[]) {
    return request.put('/envs/batch/group', { ids, groups }) as Promise<{ message: string }>
  },

  sort(sourceId: number, targetId?: number) {
    return request.put('/envs/sort', { source_id: sourceId, target_id: targetId }) as Promise<{ message: string }>
  },

  moveToTop(id: number) {
    return request.put(`/envs/${id}/move-top`) as Promise<{ message: string }>
  },

  cancelTop(id: number) {
    return request.put(`/envs/${id}/cancel-top`) as Promise<{ message: string }>
  },

  groups() {
    return request.get('/envs/groups') as Promise<{ data: string[] }>
  },

  names() {
    return request.get('/envs/names') as Promise<{ data: EnvNameOption[] }>
  },

  export(ids?: number[]) {
    return request.get('/envs/export', { params: ids?.length ? { ids: ids.join(',') } : undefined }) as Promise<{ data: Record<string, string> }>
  },

  exportAll(ids?: number[]) {
    return request.get('/envs/export-all', { params: ids?.length ? { ids: ids.join(',') } : undefined }) as Promise<{ data: any[] }>
  },

  exportFiles(format?: string, enabledOnly?: boolean, ids?: number[]) {
    return request.post('/envs/export-files', { format, enabled_only: enabledOnly, ids }) as Promise<{ data: Record<string, string> }>
  },

  import(envs: any[], mode?: string) {
    return request.post('/envs/import', { envs, mode }) as Promise<{ message: string; errors: string[] }>
  }
}
