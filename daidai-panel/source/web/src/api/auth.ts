import request from './request'
import axios from 'axios'
import type { GeeTestValidateResult } from '@/utils/geetest'

export const authApi = {
  checkInit() {
    return request.get('/auth/check-init') as Promise<{ need_init: boolean }>
  },

  init(username: string, password: string) {
    return request.post('/auth/init', { username, password }) as Promise<{ message: string; user: any }>
  },

  login(username: string, password: string, totpCode?: string, captcha?: GeeTestValidateResult | null) {
    return request.post('/auth/login', {
      username,
      password,
      totp_code: totpCode || '',
      captcha: captcha || undefined
    }) as Promise<{
      message: string
      access_token: string
      refresh_token: string
      user: any
      two_factor_required?: boolean
      captcha_required?: boolean
    }>
  },

  logout() {
    return request.post('/auth/logout') as Promise<{ message: string }>
  },

  refresh() {
    const refreshToken = localStorage.getItem('refresh_token')
    return axios.post('/api/auth/refresh', null, {
      headers: {
        Authorization: `Bearer ${refreshToken}`,
        'X-Client-Type': 'web',
        'X-Client-App': 'daidai-panel-web'
      }
    }).then(res => res.data) as Promise<{ access_token: string }>
  },

  getUser() {
    return request.get('/auth/user') as Promise<{ user: any }>
  },

  changePassword(oldPassword: string, newPassword: string) {
    return request.put('/auth/password', {
      old_password: oldPassword,
      new_password: newPassword
    }) as Promise<{ message: string }>
  },

  captchaConfig(username?: string) {
    return request.get('/auth/captcha-config', {
      params: username ? { username } : undefined
    }) as Promise<{
      enabled: boolean
      captcha_id: string
      configured: boolean
      implemented: boolean
      required: boolean
      require_after_failures: number
      message: string
    }>
  },

  uploadAvatar(file: File) {
    const formData = new FormData()
    formData.append('avatar', file)
    return request.post('/auth/avatar', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    }) as Promise<{ message: string; avatar_url: string }>
  },

  deleteAvatar() {
    return request.delete('/auth/avatar') as Promise<{ message: string }>
  },

  changeUsername(username: string) {
    return request.put('/auth/username', { username }) as Promise<{ message: string; user: any }>
  },

  // 当前登录用户的界面偏好（目前只有编辑器那一组）。
  // 只要登录就能读写自己的那份，不限角色 —— 脚本页 operator 就能进，
  // 不能要求管理员权限才允许改自己的编辑器开关。
  // stored 表示「服务端是否真的存过这个用户的偏好」。它不是冗余字段：
  // 服务端对「没存过」的用户下发的是一整套默认值，与「用户主动选了默认值」逐字相同，
  // 客户端只有靠这个标记才能区分两者 —— 没存过时要把本机偏好上行迁移，
  // 而不是拿默认值把本机（可能是 v3.2.2 存下来的）选择覆盖掉。
  getPreferences() {
    return request.get('/auth/preferences') as Promise<{
      editor: Record<string, any>
      stored?: boolean
    }>
  },

  // 只提交改动过的键，服务端做字段级合并，两个标签页各改各的开关不会互相覆盖。
  updatePreferences(editor: Record<string, string | boolean>) {
    return request.put('/auth/preferences', { editor }) as Promise<{
      editor: Record<string, any>
      stored?: boolean
    }>
  }
}
