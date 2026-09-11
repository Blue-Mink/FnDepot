import { computed, ref } from 'vue'
import { configApi, type SystemConfigMap } from '@/api/system'
import { ElMessage } from 'element-plus'
import { applyPanelAppearance, applyPanelShapeStyle } from '@/utils/panelAppearance'
import type { SettingsConfigForm } from './types'
import {
  buildConfigDraftWrites,
  CONFIG_KEYS_RENDERED_ELSEWHERE,
  groupSystemConfigItems,
  parseConfigBool,
  parseSystemConfigItems,
  validateConfigDraft
} from './systemConfigSchema'

const logBackgroundImageMaxBytes = 10 * 1024 * 1024
const logBackgroundImageTargetDataUrlBytes = 1600 * 1024
const logBackgroundImageMaxDimensions = [1920, 1600, 1280, 1024, 900, 768, 640]
const logBackgroundImageQualities = [0.82, 0.72, 0.62, 0.52, 0.42, 0.34, 0.28]

function getTextBytes(value: string) {
  return new Blob([value]).size
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error || new Error('read failed'))
    reader.readAsDataURL(file)
  })
}

function loadImage(dataUrl: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('image load failed'))
    image.src = dataUrl
  })
}

function canvasToDataURL(canvas: HTMLCanvasElement, mimeType: string, quality: number) {
  const dataUrl = canvas.toDataURL(mimeType, quality)
  if (mimeType === 'image/webp' && !dataUrl.startsWith('data:image/webp')) {
    return canvas.toDataURL('image/jpeg', quality)
  }
  return dataUrl
}

async function compressLogBackgroundImage(file: File) {
  const originalDataUrl = await readFileAsDataURL(file)
  if (getTextBytes(originalDataUrl) <= logBackgroundImageTargetDataUrlBytes) {
    return { dataUrl: originalDataUrl, compressed: false }
  }

  const image = await loadImage(originalDataUrl)
  const sourceWidth = image.naturalWidth || image.width
  const sourceHeight = image.naturalHeight || image.height
  if (!sourceWidth || !sourceHeight) {
    throw new Error('invalid image')
  }

  for (const maxDimension of logBackgroundImageMaxDimensions) {
    const scale = Math.min(1, maxDimension / Math.max(sourceWidth, sourceHeight))
    const width = Math.max(1, Math.round(sourceWidth * scale))
    const height = Math.max(1, Math.round(sourceHeight * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height

    const context = canvas.getContext('2d', { alpha: true })
    if (!context) {
      throw new Error('canvas unsupported')
    }
    context.clearRect(0, 0, width, height)
    context.drawImage(image, 0, 0, width, height)

    for (const quality of logBackgroundImageQualities) {
      const dataUrl = canvasToDataURL(canvas, 'image/webp', quality)
      const size = getTextBytes(dataUrl)
      if (size <= logBackgroundImageTargetDataUrlBytes) {
        return { dataUrl, compressed: true }
      }
    }
  }

  throw new Error('compressed image too large')
}

export function useSettingsConfig() {
  // 此开关原为功能上线门控，当前已全量启用（保留常量以兼容消费方）
  const captchaFeatureImplemented = true
  const configsLoading = ref(false)
  const configsSaving = ref(false)

  const configForm = ref<SettingsConfigForm>({
    max_concurrent_tasks: 5,
    log_retention_days: 7,
    max_log_content_size: 102400000,
    random_delay: '',
    random_delay_extensions: '',
    auto_install_deps: true,
    console_timeout_minutes: 30,
    auto_add_cron: true,
    auto_del_cron: true,
    default_cron_rule: '',
    repo_file_extensions: '',
    cpu_warn: 80,
    memory_warn: 80,
    disk_warn: 90,
    notify_on_resource_warn: false,
    notify_panel_label: '',
    notify_on_login: false,
    proxy_url: '',
    update_image_mirror: '',
    binary_update_proxy: '',
    auto_update_enabled: false,
    trusted_proxy_cidrs: '',
    captcha_enabled: false,
    captcha_id: '',
    captcha_key: '',
    captcha_fail_mode: 'open',
    panel_title: '',
    timezone: 'Asia/Shanghai',
    panel_icon: '',
    editor_background_color: '',
    log_background_color: '',
    log_background_image: '',
    backup_schedule_enabled: false,
    backup_schedule_frequency: 'daily',
    backup_schedule_time: '03:00',
    backup_schedule_weekday: '1',
    backup_schedule_monthday: 1,
    backup_schedule_name: '',
    backup_schedule_password: '',
    backup_schedule_selection: 'configs,tasks,subscriptions,env_vars,logs,scripts,dependencies,task_views',
    max_web_sessions: 1,
    max_app_sessions: 1
  })

  // 服务端下发的原始 schema + 当前值。两个用途：
  // 1) 兜底区（ExtraConfigCard）按 schema 渲染本页没有专属表单的配置项；
  // 2) 所有保存按钮拿它当基准，只回写真正改动过的键。
  const rawConfigs = ref<SystemConfigMap>({})

  // 兜底区的表单草稿：key -> 原始字符串。
  // 控件全部按字符串读写，因为服务端 PUT /configs/batch 绑定的是 map[string]string，
  // 连 bool 的值也是字符串 "true"/"false" 而不是 JSON 布尔。
  const extraConfigDraft = ref<Record<string, string>>({})

  // 兜底区要渲染的项 = 服务端注册项 − 本页硬编码表单已覆盖的键 − 已在别的页面渲染的键。
  //
  // 「已覆盖」直接取 configForm 的键而不是另写一张名单：
  // 往 configForm 里加键就自动从兜底区消失，往服务端注册表里加项而 Web 忘了加表单，
  // 就会自动出现在兜底区。这样两边都不需要人手同步。
  const extraConfigItems = computed(() => {
    const covered = new Set<string>([
      ...Object.keys(configForm.value),
      ...Object.keys(CONFIG_KEYS_RENDERED_ELSEWHERE)
    ])
    return parseSystemConfigItems(rawConfigs.value, covered)
  })

  const extraConfigGroups = computed(() => groupSystemConfigItems(extraConfigItems.value))

  function readConfigString(cfgs: Record<string, any>, key: string, fallback = ''): string {
    const entry = cfgs[key]
    const raw = entry?.value ?? entry?.default_value ?? fallback
    if (raw === null || raw === undefined) return fallback
    return String(raw)
  }

  function readConfigNumber(cfgs: Record<string, any>, key: string, fallback: number): number {
    const raw = readConfigString(cfgs, key, String(fallback))
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : fallback
  }

  function readConfigBool(cfgs: Record<string, any>, key: string, fallback: boolean): boolean {
    const raw = readConfigString(cfgs, key, fallback ? 'true' : 'false').trim().toLowerCase()
    if (['true', '1', 'yes', 'on'].includes(raw)) return true
    if (['false', '0', 'no', 'off'].includes(raw)) return false
    return fallback
  }

  async function loadSystemConfigs() {
    configsLoading.value = true
    try {
      const res = await configApi.list()
      const cfgs: SystemConfigMap = res.data || {}
      rawConfigs.value = cfgs

      configForm.value = {
        max_concurrent_tasks: readConfigNumber(cfgs, 'max_concurrent_tasks', 5),
        log_retention_days: readConfigNumber(cfgs, 'log_retention_days', 7),
        max_log_content_size: readConfigNumber(cfgs, 'max_log_content_size', 102400000),
        random_delay: readConfigString(cfgs, 'random_delay', ''),
        random_delay_extensions: readConfigString(cfgs, 'random_delay_extensions', ''),
        auto_install_deps: readConfigBool(cfgs, 'auto_install_deps', true),
        console_timeout_minutes: readConfigNumber(cfgs, 'console_timeout_minutes', 30),
        auto_add_cron: readConfigBool(cfgs, 'auto_add_cron', true),
        auto_del_cron: readConfigBool(cfgs, 'auto_del_cron', true),
        default_cron_rule: readConfigString(cfgs, 'default_cron_rule', ''),
        repo_file_extensions: readConfigString(cfgs, 'repo_file_extensions', ''),
        cpu_warn: readConfigNumber(cfgs, 'cpu_warn', 80),
        memory_warn: readConfigNumber(cfgs, 'memory_warn', 80),
        disk_warn: readConfigNumber(cfgs, 'disk_warn', 90),
        notify_on_resource_warn: readConfigBool(cfgs, 'notify_on_resource_warn', false),
        notify_panel_label: readConfigString(cfgs, 'notify_panel_label', ''),
        notify_on_login: readConfigBool(cfgs, 'notify_on_login', false),
        proxy_url: readConfigString(cfgs, 'proxy_url', ''),
        update_image_mirror: readConfigString(cfgs, 'update_image_mirror', ''),
        binary_update_proxy: readConfigString(cfgs, 'binary_update_proxy', ''),
        auto_update_enabled: readConfigBool(cfgs, 'auto_update_enabled', false),
        trusted_proxy_cidrs: readConfigString(cfgs, 'trusted_proxy_cidrs', ''),
        captcha_enabled: readConfigBool(cfgs, 'captcha_enabled', false),
        captcha_id: readConfigString(cfgs, 'captcha_id', ''),
        captcha_key: readConfigString(cfgs, 'captcha_key', ''),
        captcha_fail_mode: readConfigString(cfgs, 'captcha_fail_mode', 'open'),
        panel_title: readConfigString(cfgs, 'panel_title', ''),
        timezone: readConfigString(cfgs, 'timezone', 'Asia/Shanghai'),
        panel_icon: readConfigString(cfgs, 'panel_icon', ''),
        editor_background_color: readConfigString(cfgs, 'editor_background_color', ''),
        log_background_color: readConfigString(cfgs, 'log_background_color', ''),
        log_background_image: readConfigString(cfgs, 'log_background_image', ''),
        backup_schedule_enabled: readConfigBool(cfgs, 'backup_schedule_enabled', false),
        backup_schedule_frequency: readConfigString(cfgs, 'backup_schedule_frequency', 'daily'),
        backup_schedule_time: readConfigString(cfgs, 'backup_schedule_time', '03:00'),
        backup_schedule_weekday: readConfigString(cfgs, 'backup_schedule_weekday', '1'),
        backup_schedule_monthday: readConfigNumber(cfgs, 'backup_schedule_monthday', 1),
        backup_schedule_name: readConfigString(cfgs, 'backup_schedule_name', ''),
        backup_schedule_password: readConfigString(cfgs, 'backup_schedule_password', ''),
        backup_schedule_selection: readConfigString(cfgs, 'backup_schedule_selection', 'configs,tasks,subscriptions,env_vars,logs,scripts,dependencies,task_views'),
        max_web_sessions: readConfigNumber(cfgs, 'max_web_sessions', 1),
        max_app_sessions: readConfigNumber(cfgs, 'max_app_sessions', 1)
      }

      // 兜底区草稿整体重建，以服务端当前值为准（丢弃上一次未保存的编辑）。
      // 与本页其它表单「重新加载即回到服务端状态」的行为一致。
      //
      // bool 收敛成 "true"/"false" 两个字面量：开关控件按这两个字符串绑值，
      // 服务端 strconv.FormatBool 出来的也正是这两个，历史脏值（"1"/"on"）在这里被拉平。
      const draft: Record<string, string> = {}
      for (const item of extraConfigItems.value) {
        draft[item.key] = item.valueType === 'bool'
          ? (parseConfigBool(item.value) ? 'true' : 'false')
          : item.value
      }
      extraConfigDraft.value = draft

      applyPanelAppearance(configForm.value)
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || '加载配置失败')
    } finally {
      configsLoading.value = false
    }
  }

  // 本页所有保存按钮的共同出口：只回写「与服务端当前值不同」的键。
  //
  // 服务端 BatchSet 是逐键 SetConfig、中途失败直接 400 返回而前面的键已经落库，
  // 全量回写会把「这一组里有一项填错」放大成「一半保存了一半没保存」。
  // 只发改动项能把出错范围缩到用户真正动过的那几个键上。
  async function submitConfigs(next: Record<string, string>): Promise<boolean> {
    const changed: Record<string, string> = {}
    for (const [key, value] of Object.entries(next)) {
      if (rawConfigs.value[key]?.value === value) continue
      changed[key] = value
    }

    if (Object.keys(changed).length === 0) {
      // 一个改动都没有时不发请求：服务端的 configs 是 binding:"required"，空对象会直接 400
      ElMessage.info('配置没有变化')
      return false
    }

    configsSaving.value = true
    try {
      await configApi.batchSet(changed)
      // 更新比较基准，避免同一份改动下次保存时被重复写一遍。
      // 服务端 normalize 可能把值改写（例如二进制加速源会补上尾部斜杠），这里只记我们发出去的值：
      // 真有差异时下次保存会再写一次，写入是幂等的，没有副作用。
      for (const [key, value] of Object.entries(changed)) {
        const entry = rawConfigs.value[key]
        if (entry) entry.value = value
        else rawConfigs.value[key] = { value }
      }
      ElMessage.success('配置已保存')
      return true
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || '保存失败')
      // 保存失败后从后端重新拉取该组的真实值，避免陈旧本地状态被下一次保存再次带出
      void loadSystemConfigs()
      return false
    } finally {
      configsSaving.value = false
    }
  }

  async function saveConfigKeys(keys: string[]) {
    const next: Record<string, string> = {}
    for (const key of keys) {
      const val = (configForm.value as any)[key]
      next[key] = typeof val === 'boolean' ? (val ? 'true' : 'false') : String(val ?? '')
    }
    if (await submitConfigs(next)) {
      applyPanelAppearance(configForm.value)
    }
  }

  function handleSaveSystemConfig() {
    void saveConfigKeys([
      'panel_title', 'timezone', 'panel_icon', 'editor_background_color', 'log_background_color', 'log_background_image'
    ])
  }

  function handleSaveAlertConfig() {
    void saveConfigKeys([
      'cpu_warn', 'memory_warn', 'disk_warn', 'notify_on_resource_warn', 'notify_panel_label', 'notify_on_login'
    ])
  }

  function handleIconUpload(file: File) {
    if (!file.name.endsWith('.svg')) {
      ElMessage.warning('仅支持 SVG 格式图标')
      return false
    }
    if (file.size > 100 * 1024) {
      ElMessage.warning('图标文件不能超过 100KB')
      return false
    }
    const reader = new FileReader()
    reader.onload = (e) => {
      configForm.value.panel_icon = e.target?.result as string
    }
    reader.readAsDataURL(file)
    return false
  }

  function handleLogBackgroundUpload(file: File) {
    if (!file.type.startsWith('image/')) {
      ElMessage.warning('仅支持图片格式背景')
      return false
    }
    if (file.size > logBackgroundImageMaxBytes) {
      ElMessage.warning('背景图片不能超过 10MB')
      return false
    }

    void (async () => {
      try {
        const result = await compressLogBackgroundImage(file)
        configForm.value.log_background_image = result.dataUrl
        applyPanelAppearance(configForm.value)
        if (result.compressed) {
          ElMessage.success('背景图片已自动压缩，请保存配置后生效')
        }
      } catch {
        ElMessage.error('背景图片压缩失败，请换一张尺寸更小的图片')
      }
    })()
    return false
  }

  function previewPanelAppearance() {
    applyPanelAppearance(configForm.value)
  }

  function handleSaveTaskConfig() {
    void saveConfigKeys([
      'max_concurrent_tasks', 'log_retention_days',
      'max_log_content_size', 'random_delay', 'random_delay_extensions', 'auto_install_deps',
      'console_timeout_minutes'
    ])
  }

  function handleSaveProxy() {
    void saveConfigKeys(['proxy_url', 'update_image_mirror', 'binary_update_proxy', 'auto_update_enabled', 'trusted_proxy_cidrs'])
  }

  function handleSaveCaptcha() {
    void saveConfigKeys(['captcha_enabled', 'captcha_id', 'captcha_key', 'captcha_fail_mode'])
  }

  function handleSaveSessionConfig() {
    void saveConfigKeys(['max_web_sessions', 'max_app_sessions'])
  }

  function handleSaveBackupSchedule(selectionCSV?: string) {
    const normalizedSelection = selectionCSV?.trim() || configForm.value.backup_schedule_selection
    configForm.value.backup_schedule_selection = normalizedSelection
    void saveConfigKeys([
      'backup_schedule_enabled',
      'backup_schedule_frequency',
      'backup_schedule_time',
      'backup_schedule_weekday',
      'backup_schedule_monthday',
      'backup_schedule_name',
      'backup_schedule_password',
      'backup_schedule_selection'
    ])
  }

  // 兜底区保存：先做前端能确定的整数校验，再把草稿交给统一出口做差集回写
  async function handleSaveExtraConfigs() {
    for (const item of extraConfigItems.value) {
      const message = validateConfigDraft(item, extraConfigDraft.value[item.key] ?? '')
      if (message) {
        ElMessage.warning(message)
        return
      }
    }
    if (await submitConfigs(buildConfigDraftWrites(extraConfigItems.value, extraConfigDraft.value))) {
      // 单独补一次圆角刻度：panel_shape_style 走的是兜底区、不在 configForm 里，
      // 本文件另外四个 applyPanelAppearance(configForm.value) 的调用点
      //（loadSystemConfigs / saveConfigKeys / handleLogBackgroundUpload / previewPanelAppearance）
      // 都覆盖不到这条保存路径，不补的话用户在这里切了圆角要刷新页面才看得到效果。
      // 认不出的值会被 applyPanelShapeStyle 忽略，草稿里没有这一项时也能安全传 undefined。
      applyPanelShapeStyle(extraConfigDraft.value['panel_shape_style'])
    }
  }

  return {
    captchaFeatureImplemented,
    configsLoading,
    configsSaving,
    configForm,
    extraConfigGroups,
    extraConfigDraft,
    handleSaveExtraConfigs,
    loadSystemConfigs,
    handleSaveSystemConfig,
    handleSaveAlertConfig,
    handleIconUpload,
    handleLogBackgroundUpload,
    previewPanelAppearance,
    handleSaveTaskConfig,
    handleSaveProxy,
    handleSaveCaptcha,
    handleSaveSessionConfig,
    handleSaveBackupSchedule
  }
}
