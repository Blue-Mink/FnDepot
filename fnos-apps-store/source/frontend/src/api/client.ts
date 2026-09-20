import { apiUrl } from './base';
import { formatCount } from '../lib/utils';

export interface AppInfo {
  /** 注册表内部键（外部源应用为 appname@源名）；同名应用共存时用它做唯一标识。 */
  key: string;
  appname: string;
  display_name: string;
  description?: string;
  installed: boolean;
  installed_version: string;
  latest_version: string;
  /**
   * Package versions. `installed_version` / `latest_version` are UPSTREAM
   * strings produced by two different sources and can disagree for the same
   * package (headscale reports installed 0.29.7 against catalog 0.29.3 while
   * both sides are 0.29.3-rN packages). The fpk pair below is what the backend
   * actually compares, so prefer it for display.
   */
  installed_fpk_version?: string;
  available_version?: string;
  has_update: boolean;
  update_ignored?: boolean;
  platform: string;
  release_url: string;
  release_notes: string;
  /**
   * 应用中心 daemon 上报的运行状态：running / stopped / starting / stopping /
   * nostart（系统组件，无独立启停）等。未安装应用为空。
   */
  status: string;
  /** daemon 能力位：是否支持启动/停用（nostart 系统组件为 false）。缺省按支持处理。 */
  start_stop?: boolean;
  /** daemon 能力位：是否可卸载。缺省按可卸载处理。 */
  uninstallable?: boolean;
  /**
   * 已安装应用的可打开 Web 入口（daemon appServiceInfo，与应用中心"打开"同源）。
   * web_url 在 daemon 提供了 host 时为完整 URL；否则用 web_protocol/web_port/
   * web_path 由前端按当前访问主机拼出（直达 :38011 或 Web UI 内嵌 iframe 均成立）。
   */
  web_protocol?: string;
  web_url?: string;
  web_port?: number;
  web_path?: string;
  /** Web 入口由 fnOS Web UI 自身服务（:5666 + web_path），无独立端口。 */
  web_on_webui?: boolean;
  /** daemon appServiceInfo.serviceName（如 "Gitea.Application"）：
   *  内嵌 fnOS Web UI 时"打开"走壳窗口 openApp(serviceName) 在壳内打开。 */
  web_service_name?: string;
  service_port?: number;
  homepage?: string;
  icon_url?: string;
  updated_at?: string;
  download_count?: number;
  /** 本机安装/更新次数（第三方源应用无全局下载量时回退展示「本机 N 次」）。 */
  local_installs?: number;
  app_type?: string;
  category?: string;
  post_install_note?: string;
  /** 应用来自哪个目录源（内置目录无此字段；外部 FnDepot 源为其显示名）。 */
  source?: string;
  // 外部源详情页扩展元数据（内置目录应用无这些字段）。
  maintainer?: string;
  maintainer_url?: string;
  distributor?: string;
  distributor_url?: string;
  changelog?: string;
  size_bytes?: number;
  sha256?: string;
  preview_count?: number;
  has_readme?: boolean;
}

/**
 * The installed version to SHOW. Prefers the package version the backend
 * compares on, so it lines up with `available_version` instead of pairing two
 * unrelated upstream strings. Falls back to the upstream version for packages
 * built before fpk_version existed.
 */
export const installedVersionLabel = (app: AppInfo): string =>
  app.installed_fpk_version || app.installed_version;

/** The version an update would move the app TO. */
/**
 * 下载量展示回退链：全局 download_count（fnos-apps 官方/源提供）→
 * 本机安装次数（第三方源应用无全局数据，官方规范不统计外部源下载量）→ 版本。
 */
export const appDownloadLabel = (app: AppInfo): string | null => {
  if (app.download_count != null && app.download_count > 0) {
    return formatCount(app.download_count) + ' 次下载';
  }
  if (app.local_installs != null && app.local_installs > 0) {
    return `本机 ${app.local_installs} 次`;
  }
  return null;
};

export const availableVersionLabel = (app: AppInfo): string =>
  app.available_version || app.latest_version;

/**
 * 已安装应用的"打开"目标 URL（等价于 fnOS 应用中心的"打开"按钮）。
 * daemon 通常不带 host（实测 host 恒为空），此时按当前访问 store 的主机拼接：
 * 用户从 http://<nas>:38011 直达，或在 fnOS Web UI 内嵌 iframe 使用，
 * 两种情况下 location.hostname 都是 NAS 主机，拼出的地址一致。
 * 无 Web 入口的应用返回 null（不渲染"打开"按钮）。
 */
export const appWebUrl = (app: AppInfo): string | null => {
  if (!app.installed) return null;
  if (app.web_url) return app.web_url;
  const protocol = app.web_protocol || (window.location.protocol === 'https:' ? 'https' : 'http');
  if (app.web_port) {
    return `${protocol}://${window.location.hostname}:${app.web_port}${app.web_path || '/'}`;
  }
  // 入口由 fnOS Web UI 自身服务（fn-knock 的 /cgi/ThirdParty/...、
  // fndepot 的 /app/fndepot 等）：按当前主机 + 5666 拼
  if (app.web_on_webui && app.web_path) {
    return `${protocol}://${window.location.hostname}:5666${app.web_path}`;
  }
  return null;
};

export interface AppsResponse {
  apps: AppInfo[];
  last_check: string;
  /** false on fnOS builds where an in-store update would destroy the app. */
  upgrade_allowed?: boolean;
  upgrade_blocked_reason?: string;
}

export interface RecommendedApp {
  name: string;
  display_name: string;
  description: string;
  source_url: string;
  github_repo?: string;
  latest_version?: string;
  updated_at?: string;
}

export interface RecommendedAppsResponse {
  apps: RecommendedApp[];
}

export interface CheckResponse {
  status: string;
  checked: number;
  updates_available: number;
}

export interface UpdateProgress {
  type?: string;
  step: string;
  progress?: number;
  message?: string;
  new_version?: string;
  app?: string;
  error?: string;
  speed?: number;
  downloaded?: number;
  total?: number;
}

export interface AppOperation {
  step: string;
  progress: number;
  message: string;
  cancel?: () => void;
  speed?: number;
  downloaded?: number;
  total?: number;
}

export const fetchApps = async (): Promise<AppsResponse> => {
  const response = await fetch(apiUrl('/api/apps'));
  if (!response.ok) {
    throw new Error(`Failed to fetch apps: ${response.statusText}`);
  }
  return response.json();
};

export const fetchRecommended = async (): Promise<RecommendedAppsResponse> => {
  const response = await fetch(apiUrl('/api/recommended'));
  if (!response.ok) {
    return { apps: [] };
  }
  return response.json();
};

export const triggerCheck = async (): Promise<CheckResponse> => {
  const response = await fetch(apiUrl('/api/check'), {
    method: 'POST',
  });
  if (!response.ok) {
    throw new Error(`Failed to trigger check: ${response.statusText}`);
  }
  return response.json();
};

export type SSECallback = (event: UpdateProgress) => void;

export interface SSEHandle {
  promise: Promise<void>;
  cancel: () => void;
}

/**
 * Stream a POST endpoint's SSE body.
 *
 * `url` must already be mount-point resolved by the caller (`apiUrl(...)`).
 * Resolving it a second time here would double-apply the prefix and produce
 * `/store/store/api/...` behind a sub-path proxy.
 */
function streamSSE(url: string, onEvent: SSECallback): SSEHandle {
  const controller = new AbortController();

  const promise = (async () => {
    const response = await fetch(url, { method: 'POST', signal: controller.signal });
    if (!response.ok) {
      // 后端错误体是 JSON {error: "..."}（如"应用已安装"），优先展示具体原因。
      let detail = `Request failed: ${response.statusText}`;
      try {
        const j = await response.json();
        if (j && (j.error || j.message)) detail = j.error || j.message;
      } catch { /* 非 JSON 错误体，沿用 statusText */ }
      throw new Error(detail);
    }

    const reader = response.body?.getReader();
    if (!reader) {
      throw new Error('No response body');
    }

    const decoder = new TextDecoder();
    let buffer = '';
    let pendingData = '';

    const dispatchPending = () => {
      if (!pendingData) return;
      try {
        onEvent(JSON.parse(pendingData));
      } catch (e) {
        // Don't silently drop terminal events ('done' / 'error') -- log so
        // we can debug a UI stuck in a spinner. Truncate the raw payload to
        // avoid leaking large/sensitive data into the browser console.
        const preview = pendingData.length > 200
          ? pendingData.slice(0, 200) + `...(+${pendingData.length - 200} chars)`
          : pendingData;
        console.warn('streamSSE: failed to parse event payload', e, 'preview:', preview);
      }
      pendingData = '';
    };

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const rawLine of lines) {
          // Trim trailing CR so CRLF-style streams (some proxies/servers) parse correctly.
          const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
          if (line.startsWith('data: ')) {
            // SSE spec: multiple consecutive data: lines are joined with newline.
            pendingData += (pendingData ? '\n' : '') + line.slice(6);
          } else if (line === '' && pendingData) {
            dispatchPending();
          }
        }
      }
      // EOF flush: if the stream ends after a 'data:' line but BEFORE the
      // blank-line terminator (e.g. server killed mid-event), dispatch what
      // we have. Without this, the final 'done' / 'error' event can be lost,
      // leaving the UI stuck on a spinner.
      buffer += decoder.decode();
      if (buffer) {
        const tail = buffer.endsWith('\r') ? buffer.slice(0, -1) : buffer;
        if (tail.startsWith('data: ')) {
          pendingData += (pendingData ? '\n' : '') + tail.slice(6);
        }
      }
      dispatchPending();
    } finally {
      reader.releaseLock();
    }
  })();

  return { promise, cancel: () => controller.abort() };
}

/** One answer to an app's install wizard. */
export interface WizardParam {
  key: string;
  value: string;
}

/** An app's install-time form, as the app itself declares it. */
export interface AppWizard {
  appname: string;
  version?: string;
  has_wizard: boolean;
  /** Raw fnOS wizard definition; rendered as-is so new field types keep working. */
  content?: WizardStep[];
  install_volume_id?: number;
  error?: string;
}

export interface WizardStep {
  stepTitle?: string;
  items?: WizardItem[];
}

export interface WizardItem {
  type: string;
  field?: string;
  label?: string;
  helpText?: string;
  initValue?: string;
  rules?: { required?: boolean; message?: string; min?: number }[];
}

export const fetchWizard = async (appname: string): Promise<AppWizard> => {
  const r = await fetch(apiUrl(`/api/apps/${appname}/wizard`));
  if (!r.ok) return { appname, has_wizard: false };
  return r.json();
};

/** 官方应用中心（fnos-official）安装参数：安装卷 + 用户对每个依赖的选择。 */
export interface PanelInstallParams {
  volumeID?: number;
  deps?: { appName: string; action: 'install' | 'skip' }[];
}

export const installApp = (appname: string, onEvent: SSECallback, wizard?: WizardParam[], panel?: PanelInstallParams): SSEHandle => {
  const params = new URLSearchParams();
  if (wizard && wizard.length) params.set('wizard', JSON.stringify(wizard));
  if (panel) params.set('panel', JSON.stringify(panel));
  const qs = params.toString() ? `?${params.toString()}` : '';
  return streamSSE(apiUrl(`/api/apps/${appname}/install${qs}`), onEvent);
};

/** 「下载 fpk」：后端按镜像链下载 FPK 到本地缓存，并登记进面板官方下载通道（SSE 进度）。 */
export const downloadFpk = (appname: string, onEvent: SSECallback): SSEHandle =>
  streamSSE(apiUrl(`/api/apps/${appname}/download-task`), onEvent);

/** 官方应用详情页 + 依赖弹窗数据（面板实时状态 + 商店目录同名条目）。 */
export interface PanelDep {
  sourceID: string;
  appName: string;
  name: string;
  icon: string;
  version: string;
  /** noinstall / nostart / running（面板实时状态）。 */
  status: string;
}

export interface PanelDetailApp {
  appName: string;
  name: string;
  version: string;
  icon: string;
  docker: boolean;
  installDepApps: PanelDep[];
  appDetail: {
    desc?: string;
    maintainer?: string;
    maintainerUrl?: string;
    distributor?: string;
    distributorUrl?: string;
    installSize?: number;
    osMinVersion?: string;
    /** 官方详情页预览截图 URL（部分应用为空）。 */
    poster?: string[];
  };
}

export interface PanelDetailResponse {
  app: PanelDetailApp;
  volume: number;
  /** depAppname -> 商店目录里同名应用展示标签（可「用已有的」）。 */
  same_name_apps?: Record<string, string[]>;
}

export const fetchPanelDetail = async (appname: string): Promise<PanelDetailResponse> => {
  const r = await fetch(apiUrl(`/api/apps/${encodeURIComponent(appname)}/panel-detail`));
  if (!r.ok) {
    const body = await r.json().catch(() => null);
    throw new Error(body?.error || `获取官方应用详情失败: ${r.statusText}`);
  }
  return r.json();
};

/** 用当前配置实测面板登录（返回官方目录应用数）。 */
/** 实测面板登录；可传未保存的表单值（覆盖服务端配置）。 */
export const testPanelLogin = async (creds?: {
  username?: string;
  password?: string;
  base_url?: string;
}): Promise<{ ok: boolean; app_count: number }> => {
  const r = await fetch(apiUrl('/api/panel/test'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: creds ? JSON.stringify(creds) : undefined,
  });
  const body = await r.json().catch(() => null);
  if (!r.ok) throw new Error(body?.error || `登录测试失败: ${r.statusText}`);
  return body;
};

export const updateApp = (appname: string, onEvent: SSECallback): SSEHandle => {
  return streamSSE(apiUrl(`/api/apps/${appname}/update`), onEvent);
};

export const uninstallApp = (appname: string, onEvent: SSECallback): SSEHandle => {
  return streamSSE(apiUrl(`/api/apps/${appname}/uninstall`), onEvent);
};

// 启动 / 停用已安装应用（与 fnOS 应用中心同步）
export const controlApp = async (appname: string, action: 'start' | 'stop'): Promise<void> => {
  const response = await fetch(apiUrl(`/api/apps/${encodeURIComponent(appname)}/${action}`), { method: 'POST' });
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.error || `${action === 'start' ? '启动' : '停用'}失败: ${response.statusText}`);
  }
};

export const reloadApps = (onEvent: SSECallback): SSEHandle => {
  return streamSSE(apiUrl('/api/apps/reload'), onEvent);
};

export interface MirrorOption {
  key: string;
  label: string;
  description: string;
}

export interface VolumeOption {
  index: number;
  path: string;
  total_bytes: number;
  free_bytes: number;
}

export interface Settings {
  check_interval_hours: number;
  mirror: string;
  mirror_options?: MirrorOption[];
  docker_mirror: string;
  docker_mirror_options?: MirrorOption[];
  custom_github_mirror?: string;
  custom_docker_mirror?: string;
  install_volume: number;
  volume_options?: VolumeOption[];
  // 内置源列表自动同步（空/缺省 = 内置默认列表地址）
  source_list_url?: string;
  source_list_disabled?: boolean;
  // FPK 下载目录 + 应用源自动监测（缺省 = 目录默认 / 监测开启）
  download_dir?: string;
  source_auto_care_disabled?: boolean;
  // 官方应用中心直连（面板账号）
  panel_enabled?: boolean;
  panel_username?: string;
  panel_base_url?: string;
  panel_has_password?: boolean;
}

export interface SourceListSyncResult {
  fetched: number;
  already: number;
  added: number;
  failed: number;
  added_names?: string[];
  errors?: string[];
}

export interface MirrorCheckResult {
  key: string;
  label: string;
  latency_ms: number;
  status: 'ok' | 'timeout' | 'error';
}

export interface MirrorCheckResponse {
  github_mirrors: MirrorCheckResult[];
  docker_mirrors: MirrorCheckResult[];
}

export const checkMirrors = async (type?: 'github' | 'docker'): Promise<MirrorCheckResponse> => {
  const params = type ? `?type=${type}` : '';
  const response = await fetch(apiUrl(`/api/mirrors/check${params}`), { method: 'POST' });
  if (!response.ok) {
    throw new Error(`Failed to check mirrors: ${response.statusText}`);
  }
  return response.json();
};

/** 单个加速源的健康状态（来自后台周期探测 + 手动测速，服务端汇总）。 */
export interface MirrorStat {
  key: string;
  label: string;
  latency_ms: number;
  status: 'ok' | 'fail' | '';
  last_check: string;
  consec_fails: number;
}

export interface MirrorSwitchInfo {
  from: string;
  to: string;
  time: string;
  reason: string;
}

/** GitHub 加速源健康监测快照（智能监测 + 自动切换）。 */
export interface MirrorHealth {
  mirrors: MirrorStat[];
  /** 用户在设置里选定的镜像 key（auto = 智能模式） */
  selected: string;
  /** 当前实际生效的镜像 key（auto 时 = 最稳定源） */
  active: string;
  last_probe: string;
  last_switch?: MirrorSwitchInfo | null;
  interval_s: number;
}

export const fetchMirrorHealth = async (refresh = false): Promise<MirrorHealth> => {
  const params = refresh ? '?refresh=1' : '';
  const response = await fetch(apiUrl(`/api/mirrors/health${params}`));
  if (!response.ok) {
    throw new Error(`Failed to fetch mirror health: ${response.statusText}`);
  }
  return response.json();
};

/** Docker 镜像加速健康监测快照（与 GitHub 版同构）。 */
export const fetchDockerMirrorHealth = async (refresh = false): Promise<MirrorHealth> => {
  const params = refresh ? '?refresh=1' : '';
  const response = await fetch(apiUrl(`/api/mirrors/docker/health${params}`));
  if (!response.ok) {
    throw new Error(`Failed to fetch docker mirror health: ${response.statusText}`);
  }
  return response.json();
};


// ── 外部应用源（FnDepot V1/V2 协议）──────────────────────────────────────────

export interface SourceEntry {
  id: string;
  name: string;
  url: string;
  author?: string;
  homepage?: string;
  app_count: number;
  error?: string;
  last_fetched?: string;
  /** 源是否启用（关闭后不再抓取）；内置官方源恒为 true */
  enabled?: boolean;
  /** 连续「抓取失败或 0 应用」次数（自动监测：连续 5 次自动关闭） */
  empty_streak?: number;
}

export interface SourcesResponse {
  sources: SourceEntry[];
}

const extractError = async (response: Response, fallback: string): Promise<string> => {
  try {
    const body = await response.json();
    if (body && body.error) return body.error;
  } catch {
    // 非 JSON 错误体，用 fallback
  }
  return fallback;
};

export const fetchSources = async (): Promise<SourcesResponse> => {
  const response = await fetch(apiUrl('/api/sources'));
  if (!response.ok) {
    throw new Error(await extractError(response, `获取应用源列表失败: ${response.statusText}`));
  }
  return response.json();
};

/** 添加外部应用源。后端会立即抓取+解析验证，可能耗时数秒。 */
export const addSource = async (url: string, name?: string): Promise<SourceEntry> => {
  const response = await fetch(apiUrl('/api/sources'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, name: name || undefined }),
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `添加应用源失败: ${response.statusText}`));
  }
  const body = await response.json();
  return body.source as SourceEntry;
};

export interface BatchSourceResult {
  url: string;
  ok: boolean;
  name?: string;
  error?: string;
}

/** 批量添加外部应用源（多行输入一次提交）。单条失败不影响其他条。 */
export const addSourcesBatch = async (
  items: { url: string; name?: string }[],
): Promise<{ added: number; results: BatchSourceResult[] }> => {
  const response = await fetch(apiUrl('/api/sources/batch'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ items }),
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `批量添加应用源失败: ${response.statusText}`));
  }
  return response.json();
};

/** 手动同步单个外部源（立即抓取，返回最新应用数）。 */
export const syncSource = async (id: string): Promise<SourceEntry> => {
  const response = await fetch(apiUrl(`/api/sources/${encodeURIComponent(id)}/sync`), {
    method: 'POST',
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `同步应用源失败: ${response.statusText}`));
  }
  const body = await response.json();
  return body.source as SourceEntry;
};

/** 同步内置源列表：自动发现并添加列表中未添加过的 FnDepot 应用源。 */
export const syncSourceList = async (): Promise<SourceListSyncResult> => {
  const response = await fetch(apiUrl('/api/sources/sync-list'), { method: 'POST' });
  if (!response.ok) {
    throw new Error(await extractError(response, `同步源列表失败: ${response.statusText}`));
  }
  return response.json();
};

export const removeSource = async (id: string): Promise<void> => {
  const response = await fetch(apiUrl(`/api/sources/${encodeURIComponent(id)}`), {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `删除应用源失败: ${response.statusText}`));
  }
};

/** 开启/关闭应用源（关闭后不再抓取，其应用从目录移除）。 */
export const toggleSource = async (id: string, enabled: boolean): Promise<void> => {
  const response = await fetch(apiUrl(`/api/sources/${encodeURIComponent(id)}/toggle`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `切换应用源状态失败: ${response.statusText}`));
  }
};

/** 已下载的 FPK 缓存条目。 */
export interface FpkDownloadFile {
  name: string;
  size: number;
  mod_at: string;
  /** FPK 清单里的 appname（解析失败为空）。 */
  appname?: string;
  /** 该应用当前是否已安装（含官方中心/其他方式安装）。 */
  installed?: boolean;
}

/**
 * 应用源显示名：官方应用中心 → 中文，内置目录（原 fnos-store 源）→ "fnos-store"，
 * 外部 FnDepot 源用其显示名。搜索/筛选/徽章统一走这里。
 */
export const sourceLabel = (app: AppInfo): string => {
  switch (app.source) {
    case 'fnos-official': return '飞牛应用中心源';
    case 'fnos-apps': return 'fnos-store';
    default: return app.source || '';
  }
};

/** 开发者显示名：内置目录（fnos-apps）的应用统一显示为 conversun。 */
export const effectiveMaintainer = (app: AppInfo): string =>
  app.maintainer || (app.source === 'fnos-apps' ? 'conversun' : '');

/**
 * 把可能混有 HTML 片段的描述文本转成纯文本（列表卡/行列表按纯文本渲染）。
 * 部分官方应用 desc 里带 <h1>/<p>/<strong> 等标签，直接插会露出标签；
 * 去标签 + 解码常见实体 + 折叠空白。
 */
export const descriptionPlainText = (t: string): string => {
  if (!t) return '';
  let s = t;
  if (/<[a-z][\s\S]*?>/i.test(s)) {
    s = s
      .replace(/<\s*br\s*\/?\s*>/gi, '\n')
      .replace(/<\s*\/\s*(p|div|li|h[1-6]|tr|blockquote)\s*>/gi, '\n')
      .replace(/<[^>]+>/g, ' ');
  }
  s = s
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n{2,}/g, '\n');
  return s.trim();
};

/** FPK 下载目录 + 已下载列表（设置页「FPK 下载目录」同步展示）。 */
export const fetchFpkDownloads = async (): Promise<{ dir: string; files: FpkDownloadFile[] }> => {
  const response = await fetch(apiUrl('/api/fpk-downloads'));
  if (!response.ok) {
    throw new Error(await extractError(response, `获取 FPK 下载列表失败: ${response.statusText}`));
  }
  return response.json();
};

/** 删除单个已下载 FPK 缓存。 */
export const deleteFpkDownload = async (name: string): Promise<void> => {
  const response = await fetch(apiUrl(`/api/fpk-downloads/${encodeURIComponent(name)}`), {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `删除 FPK 缓存失败: ${response.statusText}`));
  }
};

/** 直接安装已下载的 FPK 缓存（SSE 进度流；不重新下载，文件保留在缓存中）。 */
export const installFpkDownload = (name: string, onEvent: SSECallback): SSEHandle => {
  return streamSSE(apiUrl(`/api/fpk-downloads/${encodeURIComponent(name)}/install`), onEvent);
};

/** 手动排序应用源（传全部自定义源 ID 的新顺序；官方源固定置顶不受影响）。 */
export const reorderSources = async (order: string[]): Promise<void> => {
  const response = await fetch(apiUrl('/api/sources/reorder'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ order }),
  });
  if (!response.ok) {
    throw new Error(await extractError(response, `排序应用源失败: ${response.statusText}`));
  }
};

/** 应用详情页资源（README / 预览图）的代理地址，走后端镜像链。 */
export const assetUrl = (appname: string, type: 'readme' | 'preview', index?: number): string =>
  apiUrl(`/api/apps/${encodeURIComponent(appname)}/asset?type=${type}${index != null ? `&index=${index}` : ''}`);

export interface StatusResponse {
  version?: string;
  platform: string;
}

export interface StoreUpdateInfo {
  current_version: string;
  available_version?: string;
  has_update: boolean;
}

export const fetchSettings = async (): Promise<Settings> => {
  const response = await fetch(apiUrl('/api/settings'));
  if (!response.ok) {
    throw new Error(`Failed to fetch settings: ${response.statusText}`);
  }
  return response.json();
};

export const updateSettings = async (settings: { check_interval_hours: number; mirror: string; docker_mirror: string; custom_github_mirror?: string; custom_docker_mirror?: string; install_volume: number; source_list_url?: string; source_list_disabled?: boolean; download_dir?: string; source_auto_care_disabled?: boolean; panel_enabled?: boolean; panel_username?: string; panel_password?: string; panel_base_url?: string; panel_clear_password?: boolean }): Promise<void> => {
  const response = await fetch(apiUrl('/api/settings'), {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(settings),
  });
  if (!response.ok) {
    throw new Error(`Failed to update settings: ${response.statusText}`);
  }
};

export const fetchStatus = async (): Promise<StatusResponse> => {
  const response = await fetch(apiUrl('/api/status'));
  if (!response.ok) {
    throw new Error(`Failed to fetch status: ${response.statusText}`);
  }
  return response.json();
};

export const fetchStoreUpdate = async (): Promise<StoreUpdateInfo> => {
  const response = await fetch(apiUrl('/api/store-update'));
  if (!response.ok) {
    throw new Error(`Failed to fetch store update info: ${response.statusText}`);
  }
  return response.json();
};

export const triggerStoreUpdate = (onEvent: SSECallback): SSEHandle => {
  return streamSSE(apiUrl('/api/store-update'), onEvent);
};

export const ignoreUpdate = async (appname: string): Promise<void> => {
  const response = await fetch(apiUrl(`/api/apps/${appname}/ignore-update`), { method: 'PUT' });
  if (!response.ok) {
    throw new Error(`Failed to ignore update: ${response.statusText}`);
  }
};

export const unignoreUpdate = async (appname: string): Promise<void> => {
  const response = await fetch(apiUrl(`/api/apps/${appname}/ignore-update`), { method: 'DELETE' });
  if (!response.ok) {
    throw new Error(`Failed to unignore update: ${response.statusText}`);
  }
};

export interface DiagnosticReport {
  app: string;
  display_name: string;
  version?: string;
  arch: 'x86' | 'ARM';
  app_type?: string;
  failed_step: string;
  error_message: string;
  log_tail: string;
  log_truncated: boolean;
  store_version: string;
  platform: string;
  timestamp: string;
}

export interface DiagnosticResponse {
  report: DiagnosticReport;
  issue_url: string;
}

export async function fetchDiagnostic(app: string, step: string, errorMsg: string): Promise<DiagnosticResponse> {
  const params = new URLSearchParams({ step, error: errorMsg });
  const res = await fetch(apiUrl(`/api/apps/${encodeURIComponent(app)}/diagnostic?${params}`));
  if (!res.ok) throw new Error(`获取诊断信息失败: ${res.status}`);
  return res.json();
}
