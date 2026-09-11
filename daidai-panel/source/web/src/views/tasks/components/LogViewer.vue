<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Check,
  Close,
  DocumentCopy,
  Download,
  InfoFilled,
  Loading,
  Operation,
  Switch,
  Warning,
} from '@element-plus/icons-vue'
import { taskApi } from '@/api/task'
import { openAuthorizedEventStream, type EventStreamConnection } from '@/utils/sse'
import { useResponsive } from '@/composables/useResponsive'
import { createTerminalLineBuffer, TERMINAL_RENDER_CHUNK_SIZE } from '@/utils/ansi'

const props = defineProps<{
  visible: boolean
  taskId: number | null
  taskName: string
  mode?: 'live' | 'latest'
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

// 「自动跟随」偏好：以前每次打开弹窗都被强制重置成暂停，用户每看一次实时日志就要重新点一下开关。
// 默认值仍是 '0'（暂停），保证没设置过的老用户升级后第一眼观感不变。
const LOG_FOLLOW_STORAGE_KEY = 'dd:tasks:log_follow'

function readStoredAutoScroll(): boolean {
  if (typeof window === 'undefined') {
    return false
  }
  try {
    // 只认字面量 '1'，其余（null / 手改坏的值）一律回落默认的暂停态
    return window.localStorage.getItem(LOG_FOLLOW_STORAGE_KEY) === '1'
  } catch {
    // 隐私模式下访问 localStorage 会直接抛错，这里必须吞掉：
    // 这个函数是在 setup 顶层同步调用的，漏出去会让整个弹窗组件初始化失败。
    return false
  }
}

function persistAutoScroll(enabled: boolean) {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(LOG_FOLLOW_STORAGE_KEY, enabled ? '1' : '0')
  } catch {
    // 隐私模式 / 存储配额满：记不住偏好不影响当前这次查看，静默忽略
  }
}

// 日志正文改成「按行 + 按块」增量渲染。
// 历史行一旦落定就不会再变，块 HTML 只解析一次并缓存；
// 每次 SSE flush 只需重算「最后一个未写满的块」和「正在刷新的当前行」，
// 不再像以前那样每帧对整份日志重跑 ansiToHtml + 整块 v-html 替换。
const logBuffer = createTerminalLineBuffer()
// 行缓冲本身是普通对象，用这个计数器把它的变更接进 Vue 的响应式
const logRevision = ref(0)
const done = ref(false)
const error = ref<string | null>(null)
const emptyMessage = ref<string | null>(null)
// 「日志还没出现，但任务确实在排队/运行中」这一态。issue #115：这段时间 task_logs 里一行都没有，
// 直接报「该任务还没有日志记录」是谎报，要用等待文案 + 继续轮询，见 keepWaitingForPendingTask。
const waitingForLog = ref(false)
// 空态下 emptyMessage 底下那行小字。以前它是写死的「任务执行一次后就会出现日志」，
// 于是「日志已过期，文件已被清理」「日志已按保留策略清理」这些明明跑过的情况，
// 底下也跟着说「执行一次后就会出现」——同一块区域里两句话自相矛盾。
// 现在跟着 emptyMessage 一起设，语义对不上就留空。
const emptyHint = ref('')
// 「从来没跑过」时的那句引导，也是后端 latest-log 在这种情况下回的原话（server/handler/task_logs.go）。
const NEVER_RAN_MESSAGE = '该任务还没有日志记录'
const NEVER_RAN_HINT = '任务执行一次后就会出现日志'
const loading = ref(false)
const logContainerRef = ref<HTMLElement>()
const autoScroll = ref(readStoredAutoScroll())
const fontSize = ref<'sm' | 'md' | 'lg'>('md')
const wrap = ref(true)
// 渲染窗口封顶：默认只渲染最后 5000 行，避免超长日志把 DOM 撑到几十万节点，
// 让每次滚动/布局都变慢。用户可以点顶部提示展开完整日志。
const RENDER_WINDOW_CHUNKS = 50
const MAX_RENDERED_LINES = RENDER_WINDOW_CHUNKS * TERMINAL_RENDER_CHUNK_SIZE
const renderWindowExpanded = ref(false)
const { dialogFullscreen } = useResponsive()
let eventSource: EventStreamConnection | null = null
let pendingSseChunks: string[] = []
let logFlushTimer: ReturnType<typeof setTimeout> | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let hiddenAt: number | null = null
let pendingScrollRestore: number | null = null
// latest-log 预取：startStream() 里与 SSE 同时发起，done 分支直接消费这个已经在飞的 promise。
// 以前是等 done 事件到了才开始第二个往返（JWT + 查库 + 解压 + 全量 ANSI 解析），
// 在弱机上这段串行开销肉眼可见（issue #109-1 的后半段）。这里只把请求提前，语义完全不变。
let prefetchedLatestLog: Promise<any> | null = null
let prefetchedLatestLogTaskId: number | null = null
let prefetchedLatestLogAt = 0
// 作废预取时用它掐断在途响应体，见 dropPrefetchedLatestLog
let prefetchedLatestLogAbort: AbortController | null = null
// 预取的有效期。超过这个窗口就当它过期、老老实实重发一次请求。
//
// 【预取只在一条路径上可以被复用】
// 预取拿到的是「打开弹窗那一刻」的最近一次日志。只有「任务早就跑完了、服务端立刻回 done、
// 流里一个字节都没有」这条路径上，done 与预取几乎同时发生、两者内容必然一致——
// 这正是本次要优化的场景（issue #109-1 的后半段）。
// 其余任何情况下那份快照都比屏幕上的内容旧，复用它会把刚跑完的日志覆盖成旧的甚至空的。
//
// 🔴 挡住误用的**主**手段是 dropPrefetchedLatestLog()：流里一收到真实数据、或收到
// done:reconnect，立刻作废预取。**不能只靠下面这条 TTL** —— 任务在 1.2s 内跑完时
// done 到达时预取仍在有效期内；done:reconnect 更可能 150ms 就回来（服务端一发现
// TinyLog 就 break，并不会等满轮询窗口）。TTL 只是「谁都没作废它、它却放了很久」的兜底。
const LATEST_LOG_PREFETCH_TTL = 1200
// reconnect 风暴熔断：连续重连且无新数据时累加，超过上限即按完成处理，避免无限重连+全量重渲染卡顿。
let reconnectAttempts = 0
const MAX_RECONNECT_ATTEMPTS = 5

// 任务状态取值，与后端 model.TaskStatus* 对齐：0=已禁用 / 0.5=排队中 / 1=已启用 / 2=运行中
const TASK_STATUS_QUEUED = 0.5
const TASK_STATUS_RUNNING = 2
// 「等日志出现」的轮询上限：1s 一次、最多 60 次（约 1 分钟）。
// 随机延迟会走延迟入队、并发数被占满时任务也会一直停在「排队中」，这段窗口必须留够；
// 但任务可能长期卡在排队中，所以要有这个兜底，不能无限轮询。
const WAITING_POLL_LIMIT = 60
let waitingPollCount = 0
// 组件卸载标记：等待轮询中间夹着一次 await（查任务状态），
// 卸载/关闭正好落在这一拍时，后面绝不能再把新的定时器挂上去。
let disposed = false

// 下面这批 computed 都要跟着行缓冲走，读一下 logRevision 建立依赖即可
const activeRenderWindow = computed(() => renderWindowExpanded.value ? 0 : RENDER_WINDOW_CHUNKS)

const hasLogs = computed(() => {
  void logRevision.value
  return !logBuffer.isEmpty
})

const renderChunks = computed(() => {
  void logRevision.value
  return logBuffer.visibleChunks(activeRenderWindow.value)
})

const omittedLineCount = computed(() => {
  void logRevision.value
  return logBuffer.omittedLineCount(activeRenderWindow.value)
})

const pendingLineHtml = computed(() => {
  void logRevision.value
  return logBuffer.pendingLineHtml()
})

const lineCount = computed(() => {
  void logRevision.value
  return logBuffer.displayLineCount
})

const byteLabel = computed(() => {
  void logRevision.value
  const bytes = logBuffer.byteLength
  if (bytes === 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
})

const fontSizeClass = computed(() => `log-font-${fontSize.value}`)

// 头部状态灯、头部徽标、底部状态栏共用这一个状态口径，避免三处各写一串条件后彼此打架。
// waiting 是本轮新增的一态（issue #115）：日志还没落库，但任务确实在排队/运行中，不能显示成「无日志」。
// error 只会在 done 之后被写入（见 fetchLatestLog），所以把它排在 running 前面不改变原有观感。
const headerState = computed<'error' | 'waiting' | 'empty' | 'done' | 'running'>(() => {
  if (error.value) return 'error'
  if (waitingForLog.value) return 'waiting'
  if (emptyMessage.value) return 'empty'
  return done.value ? 'done' : 'running'
})

function expandRenderWindow() {
  const container = logContainerRef.value
  const previousHeight = container?.scrollHeight ?? 0
  const previousTop = container?.scrollTop ?? 0
  renderWindowExpanded.value = true
  void nextTick(() => {
    const el = logContainerRef.value
    if (!el) return
    // 补齐的内容是往上长的，按高度差补偿滚动位置，避免视口整个跳走
    el.scrollTop = previousTop + (el.scrollHeight - previousHeight)
  })
}

watch(() => props.visible, (visible) => {
  if (visible && props.taskId) {
    if (props.mode === 'latest') {
      void loadLatestOnly()
    } else {
      void startStream()
    }
  } else {
    cleanup()
  }
})

watch(() => props.taskId, (taskId, previousTaskId) => {
  if (props.visible && taskId && taskId !== previousTaskId) {
    if (props.mode === 'latest') {
      void loadLatestOnly()
    } else {
      void startStream()
    }
  }
})

watch(autoScroll, (enabled) => {
  // 写回必须放在 if (enabled) 外面，否则只存得住「开」、存不住「关」。
  // 只有 live 模式的切换才算用户偏好：latest 分支会程序性地强制关掉跟随（见 loadLatestOnly），
  // 那不是用户的选择，写回去会把已经存好的「跟随」抹成「暂停」。
  if (props.mode !== 'latest') {
    persistAutoScroll(enabled)
  }
  if (enabled) {
    scheduleScrollToBottom()
  }
})

async function startStream(isReconnect = false) {
  const savedScrollTop = isReconnect ? logContainerRef.value?.scrollTop ?? null : null
  cleanup()
  resetLogOutput()
  done.value = false
  error.value = null
  emptyMessage.value = null
  emptyHint.value = ''
  waitingForLog.value = false
  loading.value = !isReconnect
  pendingScrollRestore = isReconnect && savedScrollTop !== null ? savedScrollTop : null
  if (!isReconnect) {
    // 用户主动打开/切换任务重新起流：清零重连计数，确保熔断只针对一次会话内的连续空重连。
    reconnectAttempts = 0
    // 等日志的轮询次数同理，只在一次会话内累计
    waitingPollCount = 0
    // 这里刻意不再重置 autoScroll —— 跟随开关已经是持久化偏好，重开弹窗/刷新页面都要沿用上次的选择。
    // 初始视口方向随之二选一：以前无条件滚顶的前提是「打开一定是暂停态、从头看」，
    // 恢复成跟随后仍滚顶会出现「开关显示跟随、内容却停在顶部」的割裂。
    if (autoScroll.value) {
      scheduleScrollToBottom()
    } else {
      scheduleScrollToTop()
    }
  }

  if (!props.taskId) {
    loading.value = false
    return
  }

  const url = `/api/v1/logs/${props.taskId}/stream`
  // 与下面的 SSE 并行发起 latest-log 预取，不 await：
  // 实时数据仍然只走 SSE，这份预取只在「流里没有实时内容、走到 done」那条既有路径上被消费
  // （见 fetchLatestLog → takeLatestLog），所以不会和流式内容重复渲染。
  // 重连（isReconnect）时不预取：那条路径最多会重试 5 次，每次都白发一个可能要全量解压的请求，
  // 反而加重了本来想优化的开销。
  if (!isReconnect) {
    prefetchLatestLog(props.taskId!)
  }
  eventSource = openAuthorizedEventStream(url, {
    onOpen() {
      loading.value = false
    },
    onMessage(data) {
      loading.value = false
      if (!data) {
        return
      }
      // 收到真实日志数据 = 有实质进展，不算空重连风暴，重置熔断计数。
      reconnectAttempts = 0
      // 🔴 同时作废预取：这一份快照是「打开弹窗那一刻」拍的，比流里正在到达的内容更旧。
      // 只靠 TTL 挡不住这条路径 —— 任务在 1.2s 内跑完时，done 到达时预取仍在有效期内，
      // 复用它就会用旧快照（甚至是上一次运行的日志、或一条 content 为空的新记录）
      // 覆盖掉刚流完的正确输出。预取只服务「服务端立刻回 done、流里一个字节都没有」这一条路径。
      dropPrefetchedLatestLog()
      pendingSseChunks.push(data)
      scheduleBufferFlush()
    },
    onEvent(event) {
      if (event.event !== 'done') {
        return
      }
      flushBufferedLogs()
      done.value = true
      cleanup()
      if (event.data === 'reconnect') {
        // reconnect 意味着服务端已经找到 TinyLog、任务确实在跑，接下来会重新推一遍历史。
        // 预取那份快照到这里已经没有意义（而且重连很可能 150ms 就回来，TTL 根本挡不住），直接作废。
        dropPrefetchedLatestLog()
        reconnectAttempts++
        if (reconnectAttempts > MAX_RECONNECT_ATTEMPTS) {
          // 连续多次重连都没有新数据：判定为无可续流的实时日志，按完成处理，停止重连风暴。
          void fetchLatestLog(0, autoScroll.value ? 'bottom' : 'preserve')
          return
        }
        // 退避重连：第 1 次 500ms，逐次翻倍，封顶 5s，降低无效重连对前端的冲击。
        const delay = Math.min(500 * 2 ** (reconnectAttempts - 1), 5000)
        reconnectTimer = setTimeout(() => {
          reconnectTimer = null
          void startStream(true)
        }, delay)
        return
      }
      // 🔴 done 的三种载荷里，只有 'finished'（服务端 0 等待、任务早就结束、日志早已落库）
      // 才允许复用并行预取。'finished-late' 说明服务端在短轮询里真的等过 ——
      // 也就是打开弹窗那一刻任务还在排队/运行，那时按 started_at DESC 取到的很可能是
      // **上一次运行**的记录、或一条 content 还是空的新记录。复用它会把上次的输出
      // 渲染成本次结果，或者错误地显示「日志已过期」。
      // 靠 TTL 挡不住：任务一两百毫秒就跑完时，done 到达时预取仍在有效期内。
      // 未知载荷按 finished 处理（向后兼容旧服务端与演示站的假流）。
      if (event.data === 'finished-late') {
        dropPrefetchedLatestLog()
      }
      void fetchLatestLog(0, autoScroll.value ? 'bottom' : 'preserve')
    },
    onError() {
      flushBufferedLogs()
      loading.value = false
      done.value = true
      cleanup()
      // 流本身出错时无从判断服务端处于哪条路径，预取一律作废、老老实实重新请求。
      dropPrefetchedLatestLog()
      void fetchLatestLog(0, hasLogs.value ? 'preserve' : 'top')
    }
  })
}

// 作废预取。四处在用：流里收到第一段真实数据、收到 done:reconnect、
// 收到 done:finished-late（服务端等过 = 任务当时正在启动/运行）、以及 latest 模式开场。
// 判据统一成一句话：只要能证明「服务端这条流不是『任务早就结束、直接收口』」，
// 那份打开弹窗时拍的旧快照就不能再被消费。
//
// 除了置空引用，还要 abort 在途请求：运行中的大日志任务那一份可能是几十 MB，
// 不 abort 的话 axios 仍会把它下完并在主线程 JSON.parse，白白压在弱机上。
function dropPrefetchedLatestLog() {
  prefetchedLatestLogAbort?.abort()
  prefetchedLatestLogAbort = null
  prefetchedLatestLog = null
  prefetchedLatestLogTaskId = null
  prefetchedLatestLogAt = 0
}

async function loadLatestOnly() {
  cleanup()
  // latest 模式自己就是一次性拉取，不做预取；同时丢掉上一次 live 会话可能残留的、没人消费的预取，
  // 避免这里读到一份旧快照（TTL 之外还多一道保险）。
  dropPrefetchedLatestLog()
  resetLogOutput()
  done.value = true
  error.value = null
  emptyMessage.value = null
  emptyHint.value = ''
  waitingForLog.value = false
  waitingPollCount = 0
  loading.value = true
  // latest 是一次性拉取的「最近结果」，后面没有 SSE 续流，跟随开关点了也没有实质作用，
  // 所以这一支仍然强制暂停 + 从头看；持久化的跟随偏好只在 live 模式恢复（见 startStream）。
  autoScroll.value = false
  scheduleScrollToTop()

  if (!props.taskId) {
    loading.value = false
    return
  }

  await fetchLatestLog(0, 'top')
  loading.value = false
}

function prefetchLatestLog(taskId: number) {
  prefetchedLatestLogTaskId = taskId
  prefetchedLatestLogAt = Date.now()
  prefetchedLatestLogAbort = typeof AbortController === 'undefined' ? null : new AbortController()
  prefetchedLatestLog = taskApi.latestLog(taskId, prefetchedLatestLogAbort?.signal)
  // 必须就地挂一个空 catch：这是一个「可能压根没人消费」的在途请求，
  // 没有 rejection handler 的话浏览器会报 unhandled promise rejection。
  // 真正的错误处理仍在 takeLatestLog / fetchLatestLog 里（退回原来的重发 + 404 重试分支）。
  // 注意 .catch() 返回的是新 promise，prefetchedLatestLog 仍然指向原始那个。
  prefetchedLatestLog.catch(() => {})
}

// 取 latest-log：优先复用 startStream() 里已经在飞的那份预取，拿不到就照原样新发一个请求。
async function takeLatestLog(): Promise<any> {
  const pending = prefetchedLatestLog
  const pendingTaskId = prefetchedLatestLogTaskId
  const pendingAt = prefetchedLatestLogAt
  // 一次性消费：404 重试、切回前台补拉这些后续调用都必须拿最新数据，不能反复复用同一份旧快照。
  prefetchedLatestLog = null
  prefetchedLatestLogTaskId = null
  prefetchedLatestLogAt = 0

  if (pending && pendingTaskId === props.taskId && Date.now() - pendingAt <= LATEST_LOG_PREFETCH_TTL) {
    try {
      return await pending
    } catch {
      // 预取失败（网络抖动、日志还没落库的 404 等）一律静默吞掉，退回原来的行为重新发一次，
      // 让后面的 catch 按既有分支处理。绝不能让预取本身把弹窗打挂。
    }
  }
  return taskApi.latestLog(props.taskId!)
}

// 「日志还没出现」时的统一判断：任务如果还在排队中/运行中，就继续等，别急着收口成静态文案。
// issue #115 的那一屏就出在这里 —— 随机延迟走的是延迟重新入队、并发数被别的任务占满时也一样，
// 任务会长时间停在「排队中」，这段时间 task_logs 里一行都没有，
// 老逻辑重试 5 次（2.5s）就把「该任务还没有日志记录」定死，而且之后再也不会自己纠正。
//
// 返回 true = 已经接管（安排了下一次轮询，或者写好了「等到上限」的说明），调用方直接 return；
// 返回 false = 可以按调用方原本的文案收口。
async function keepWaitingForPendingTask(retryCount: number, scrollMode: 'top' | 'bottom' | 'preserve') {
  const taskId = props.taskId
  if (!disposed && props.visible && taskId) {
    // 顺带查一次任务状态：LiveLogs 的返回体里带着 status（0=已禁用 / 0.5=排队中 / 1=已启用 / 2=运行中）
    let status: number | null = null
    try {
      const live = await taskApi.liveLogs(taskId)
      status = typeof live?.status === 'number' ? live.status : null
    } catch {
      // 状态查不到就不猜，直接按调用方原来的文案收口
    }
    const pending = status === TASK_STATUS_QUEUED || status === TASK_STATUS_RUNNING
    // await 这一拍里用户可能已经关掉弹窗、切了任务、或者组件被卸载，这些情况下不能再挂新的定时器
    if (pending && !disposed && props.visible && props.taskId === taskId) {
      if (waitingPollCount < WAITING_POLL_LIMIT) {
        waitingPollCount++
        waitingForLog.value = true
        emptyMessage.value = status === TASK_STATUS_QUEUED
          ? '任务排队中，开始执行后会出现日志…'
          : '任务已开始，正在等待第一条日志…'
        emptyHint.value = '正在等待日志写入，出现后会自动显示，不用手动重开'
        // 同一时刻只留一个等待定时器：切后台再回来时 handleVisibilityChange 会另起一条链，
        // 不掐掉旧的就会变成两条链同时轮询。
        if (reconnectTimer !== null) {
          clearTimeout(reconnectTimer)
        }
        reconnectTimer = setTimeout(() => {
          reconnectTimer = null
          void fetchLatestLog(retryCount, scrollMode)
        }, 1000)
        return true
      }
      // 到达兜底上限：停止轮询。这里退出等待态，头部徽标会显示「无日志」——
      // 这句话是对着日志说的（确实一条都没有），所以正文也要写成「还没有产生日志」的口吻，
      // 任务卡在哪一步放进括号里补充，避免出现「正文说还在跑、徽标说无日志」那种看着矛盾的组合。
      waitingForLog.value = false
      emptyMessage.value = status === TASK_STATUS_QUEUED
        ? '还没有产生日志（任务一直停在排队中）'
        : '还没有产生日志（任务仍在运行，但还没输出第一行）'
      emptyHint.value = '已停止自动刷新，重新打开这个窗口可以继续等'
      return true
    }
  }
  waitingForLog.value = false
  return false
}

async function fetchLatestLog(retryCount = 0, scrollMode: 'top' | 'bottom' | 'preserve' = 'top') {
  const previousScrollTop = logContainerRef.value?.scrollTop ?? 0
  try {
    const res = await takeLatestLog() as any
    if (!res) {
      // 连记录都没返回：也可能只是任务还在排队/运行中，先按状态决定要不要继续等
      if (await keepWaitingForPendingTask(retryCount, scrollMode)) {
        return
      }
      emptyMessage.value = NEVER_RAN_MESSAGE
      emptyHint.value = NEVER_RAN_HINT
      return
    }
    if (res.content) {
      // 拿到正文：等待态要一并收掉，否则头部徽标会一直停在「等待中」
      emptyMessage.value = null
      emptyHint.value = ''
      waitingForLog.value = false
      resetLogOutput()
      appendLogChunk(String(res.content))
      if (scrollMode === 'bottom') {
        scheduleScrollToBottom()
      } else if (scrollMode === 'preserve') {
        void nextTick(() => {
          if (logContainerRef.value) {
            logContainerRef.value.scrollTop = previousScrollTop
          }
        })
      } else {
        scheduleScrollToTop()
      }
    } else {
      // 有记录却没有内容有两种可能：日志文件被保留策略清掉了，
      // 或者这次运行刚建好记录、还没写出第一行（记录是在执行开始时就落库的）。
      // 后者不能报「已过期」，同样交给状态判断，两条文案的语义由此保持互斥。
      if (await keepWaitingForPendingTask(retryCount, scrollMode)) {
        return
      }
      emptyMessage.value = '日志已过期，文件已被清理'
      // 这条记录确实跑过，只是正文没了，不能再配「执行一次后就会出现日志」那句引导
      emptyHint.value = ''
    }
  } catch (err: any) {
    if (err?.response?.status === 404) {
      if (retryCount < 5 && props.visible) {
        // 同一时刻只留一个定时器：快速重试和下面的等待轮询共用 reconnectTimer，
        // 切后台再回来时 handleVisibilityChange 会另起一条链，不掐掉旧的就会叠出多条并行轮询。
        if (reconnectTimer !== null) {
          clearTimeout(reconnectTimer)
        }
        reconnectTimer = setTimeout(() => {
          reconnectTimer = null
          void fetchLatestLog(retryCount + 1, scrollMode)
        }, 500)
        return
      }
      // 快速重试用完仍是 404，不等于「从来没跑过」，先看任务是不是还在排队/运行中
      if (await keepWaitingForPendingTask(retryCount, scrollMode)) {
        return
      }
      // 后端 404 的说明比前端这句通用文案更准（例如区分「从来没跑过」和「日志已按保留策略清理」），
      // 统一的错误结构是 { error: string }（server/pkg/response），取不到再退回原文案
      emptyMessage.value = err?.response?.data?.error || NEVER_RAN_MESSAGE
      // 只有「从来没跑过」才配得上那句引导：后端换成「日志已按保留策略清理」时再说
      // 「执行一次后就会出现日志」，就和它自己上一句自相矛盾了
      emptyHint.value = emptyMessage.value === NEVER_RAN_MESSAGE ? NEVER_RAN_HINT : ''
    } else {
      waitingForLog.value = false
      error.value = '获取日志失败'
    }
  }
}

function resetLogOutput() {
  logBuffer.reset()
  renderWindowExpanded.value = false
  logRevision.value++
}

function appendLogChunk(chunk: string) {
  logBuffer.append(chunk)
  logRevision.value++
}

// 合帧间隔按日志体量放大：增量渲染后每次 flush 的渲染开销只和新增行有关，
// 但开启自动跟随时仍要对整篇文档强制一次布局，日志越长这一步越贵。
// 放大到 48 / 120ms 肉眼仍然无感，却能显著减少长日志下的主线程占用。
function flushDelayMs(): number {
  const lines = logBuffer.lineCount
  if (lines < 2000) return 16
  if (lines < 10000) return 48
  return 120
}

function scheduleBufferFlush() {
  if (logFlushTimer !== null) {
    return
  }
  logFlushTimer = setTimeout(() => {
    logFlushTimer = null
    flushBufferedLogs()
  }, flushDelayMs())
}

function flushBufferedLogs() {
  if (logFlushTimer !== null) {
    clearTimeout(logFlushTimer)
    logFlushTimer = null
  }
  if (pendingSseChunks.length === 0) {
    return
  }

  for (const chunk of pendingSseChunks) {
    // 不把“每个 SSE 消息结束”当成真实换行。
    // 真实终端只有遇到 \n / \r\n 才落新行，裸 \r 则继续覆盖当前行。
    logBuffer.append(chunk)
  }
  pendingSseChunks = []
  // 整批只触发一次重渲染
  logRevision.value++

  if (pendingScrollRestore !== null) {
    const target = pendingScrollRestore
    pendingScrollRestore = null
    void nextTick(() => {
      if (logContainerRef.value) {
        logContainerRef.value.scrollTop = target
      }
    })
  } else if (autoScroll.value) {
    scheduleScrollToBottom()
  }
}

function scheduleScrollToBottom() {
  void nextTick(() => {
    // el-dialog 带 destroy-on-close，关闭后 logContainerRef 会被置空；
    // 重新打开时 startStream 是在 watch(props.visible) 里同步调用的，
    // 这一拍弹窗内容不一定已经挂上，只靠 scrollToBottom 内部的空值守卫会静默失效（表现为停在顶部）。
    // 补一次 rAF 重试兜底：下一帧 DOM 必然已经渲染。
    if (!logContainerRef.value) {
      requestAnimationFrame(() => {
        scrollToBottom()
      })
      return
    }
    scrollToBottom()
  })
}

function scheduleScrollToTop() {
  void nextTick(() => {
    scrollToTop()
  })
}

function scrollToBottom() {
  if (logContainerRef.value) {
    logContainerRef.value.scrollTop = logContainerRef.value.scrollHeight
  }
}

function scrollToTop() {
  if (logContainerRef.value) {
    logContainerRef.value.scrollTop = 0
  }
}

function cleanup() {
  if (logFlushTimer !== null) {
    clearTimeout(logFlushTimer)
    logFlushTimer = null
  }
  pendingSseChunks = []
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
}

function handleVisibilityChange() {
  flushBufferedLogs()
  if (document.hidden) {
    hiddenAt = Date.now()
    return
  }

  if (!props.visible || !props.taskId) {
    hiddenAt = null
    return
  }

  const wasBackgrounded = hiddenAt !== null && Date.now() - hiddenAt > 1500
  hiddenAt = null

  if (done.value) {
    if (!hasLogs.value) {
      void fetchLatestLog()
    }
    return
  }

  if (wasBackgrounded) {
    void startStream(true)
  }
}

function cycleFontSize() {
  if (fontSize.value === 'sm') fontSize.value = 'md'
  else if (fontSize.value === 'md') fontSize.value = 'lg'
  else fontSize.value = 'sm'
}

async function handleCopy() {
  if (!hasLogs.value) {
    ElMessage.warning('暂无内容可复制')
    return
  }
  // 纯文本按需还原，不进渲染路径
  const text = logBuffer.toText()
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    try {
      document.execCommand('copy')
      ElMessage.success('已复制')
    } catch {
      ElMessage.error('复制失败')
    }
    document.body.removeChild(ta)
  }
}

function handleDownload() {
  if (!hasLogs.value) {
    ElMessage.warning('暂无内容可下载')
    return
  }
  const safeName = (props.taskName || 'task').replace(/[\\/:*?"<>|]/g, '_')
  const filename = `${safeName}-${props.taskId ?? 'log'}.log`
  const blob = new Blob([logBuffer.toText()], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  ElMessage.success('已下载')
}

onMounted(() => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  // 先置标记再 cleanup：等待轮询里那次 await 如果正好在飞，回来后要靠它拦住「再挂一个定时器」
  disposed = true
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  cleanup()
})

function handleClose() {
  emit('update:visible', false)
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    width="88%"
    :fullscreen="dialogFullscreen"
    top="5vh"
    align-center
    :show-close="false"
    :lock-scroll="false"
    class="log-viewer-dialog"
    destroy-on-close
    @close="handleClose"
  >
    <template #header>
      <div class="viewer-hero">
        <div class="viewer-hero-main">
          <div class="viewer-hero-title-row">
            <transition name="status-switch" mode="out-in">
              <!-- 等待日志（任务排队中/运行中但还没日志）复用「运行中」这颗呼吸灯，不另造一套 UI -->
              <span
                v-if="headerState === 'running' || headerState === 'waiting'"
                key="running"
                class="status-orb status-orb--running"
                :aria-label="headerState === 'waiting' ? '等待日志' : '运行中'"
              >
                <span class="status-orb-core"></span>
                <span class="status-orb-ripple"></span>
              </span>
              <span v-else-if="headerState === 'error'" key="error" class="status-orb status-orb--error" aria-label="错误">
                <el-icon :size="12"><Warning /></el-icon>
              </span>
              <span v-else-if="headerState === 'empty'" key="empty" class="status-orb status-orb--empty" aria-label="无日志">
                <el-icon :size="12"><InfoFilled /></el-icon>
              </span>
              <span v-else key="done" class="status-orb status-orb--done" aria-label="已完成">
                <el-icon :size="12"><Check /></el-icon>
              </span>
            </transition>
            <h2 class="viewer-hero-title" :title="taskName">{{ taskName || '任务日志' }}</h2>
            <span v-if="taskId" class="viewer-hero-id">#{{ taskId }}</span>
            <span
              class="viewer-hero-status"
              :class="{
                'viewer-hero-status--running': headerState === 'running' || headerState === 'waiting',
                'viewer-hero-status--done': headerState === 'done',
                'viewer-hero-status--empty': headerState === 'empty',
                'viewer-hero-status--error': headerState === 'error'
              }"
            >
              {{ headerState === 'error' ? '异常' : headerState === 'waiting' ? '等待中' : headerState === 'empty' ? '无日志' : headerState === 'done' ? '已完成' : '运行中' }}
            </span>
          </div>
          <div class="viewer-hero-meta">
            <span class="viewer-hero-meta-item">{{ lineCount }} 行</span>
            <span v-if="byteLabel" class="viewer-hero-meta-item">{{ byteLabel }}</span>
            <span class="viewer-hero-meta-item">
              {{ autoScroll ? '自动跟随底部' : '已暂停自动滚动' }}
            </span>
          </div>
        </div>

        <div class="viewer-hero-actions">
          <el-tooltip content="切换字号" placement="bottom">
            <button class="tool-btn" @click="cycleFontSize" aria-label="切换字号">
              <el-icon :size="15"><Operation /></el-icon>
              <span class="tool-btn-label">{{ fontSize.toUpperCase() }}</span>
            </button>
          </el-tooltip>
          <el-tooltip :content="wrap ? '关闭自动换行' : '开启自动换行'" placement="bottom">
            <button
              class="tool-btn"
              :class="{ 'tool-btn--active': wrap }"
              @click="wrap = !wrap"
              aria-label="切换换行"
            >
              <el-icon :size="15"><Switch /></el-icon>
              <span class="tool-btn-label">Wrap</span>
            </button>
          </el-tooltip>
          <el-tooltip content="复制全部" placement="bottom">
            <button class="tool-btn" :disabled="!hasLogs" @click="handleCopy" aria-label="复制">
              <el-icon :size="15"><DocumentCopy /></el-icon>
            </button>
          </el-tooltip>
          <el-tooltip content="下载日志" placement="bottom">
            <button class="tool-btn" :disabled="!hasLogs" @click="handleDownload" aria-label="下载">
              <el-icon :size="15"><Download /></el-icon>
            </button>
          </el-tooltip>
          <div class="auto-scroll-toggle">
            <el-switch v-model="autoScroll" size="small" inline-prompt active-text="跟随" inactive-text="暂停" />
          </div>
          <button class="tool-btn tool-btn--close" @click="handleClose" aria-label="关闭">
            <el-icon :size="16"><Close /></el-icon>
          </button>
        </div>
      </div>
    </template>

    <div class="viewer-body" :class="fontSizeClass">
      <div ref="logContainerRef" class="viewer-log dd-log-surface" v-loading="loading">
        <div v-if="error" class="viewer-message viewer-message--error">
          <el-icon :size="22"><Warning /></el-icon>
          <span>{{ error }}</span>
        </div>
        <div v-else-if="emptyMessage && !hasLogs" class="viewer-message viewer-message--empty">
          <el-icon :size="22">
            <Loading v-if="headerState === 'waiting'" />
            <InfoFilled v-else />
          </el-icon>
          <span>{{ emptyMessage }}</span>
          <span v-if="emptyHint" class="viewer-message-hint">{{ emptyHint }}</span>
        </div>
        <div v-else-if="!hasLogs && !loading" class="viewer-message">
          <el-icon :size="22"><Loading /></el-icon>
          <span>等待日志输出...</span>
        </div>
        <div v-else class="viewer-log-content" :class="{ 'viewer-log-content--nowrap': !wrap }">
          <button
            v-if="omittedLineCount > 0"
            type="button"
            class="log-omitted-notice"
            title="超长日志默认只渲染末尾部分，避免页面卡顿。展开完整日志在内容极多时可能需要等待片刻，也可以直接用上方「下载」拿到全部内容。"
            @click="expandRenderWindow"
          >已省略前 {{ omittedLineCount }} 行（默认只渲染最后 {{ MAX_RENDERED_LINES }} 行）· 点击展开完整日志</button>
          <span v-for="chunk in renderChunks" :key="chunk.key" v-html="chunk.html"></span>
          <span v-html="pendingLineHtml"></span>
        </div>
      </div>

      <div class="viewer-statusbar">
        <div class="viewer-statusbar-group">
          <span class="viewer-statusbar-item">{{ lineCount }} 行</span>
          <span v-if="byteLabel" class="viewer-statusbar-item">{{ byteLabel }}</span>
          <span class="viewer-statusbar-item">Wrap {{ wrap ? 'ON' : 'OFF' }}</span>
        </div>
        <div class="viewer-statusbar-group">
          <!-- 等日志期间沿用「实时采集中」那颗跳动的点，表示这里还在自动刷新，不是死在「暂无日志」上 -->
          <span v-if="headerState === 'waiting'" class="viewer-statusbar-item viewer-statusbar-item--live">等待日志中</span>
          <span v-else-if="props.mode === 'latest' && !error && !emptyMessage" class="viewer-statusbar-item">最近结果</span>
          <span v-else-if="!done && !error && !emptyMessage" class="viewer-statusbar-item viewer-statusbar-item--live">实时采集中</span>
          <span v-else-if="error" class="viewer-statusbar-item viewer-statusbar-item--error">{{ error }}</span>
          <span v-else-if="emptyMessage" class="viewer-statusbar-item viewer-statusbar-item--empty">暂无日志</span>
          <span v-else class="viewer-statusbar-item">UTF-8</span>
        </div>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped lang="scss">

/* =============== Hero =============== */
.viewer-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 18px;
  background: var(--el-bg-color);
  flex-wrap: wrap;
}

.viewer-hero-main {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  flex: 1;
}

.viewer-hero-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

.viewer-hero-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  margin: 0;
  letter-spacing: 0.2px;
  font-family: var(--dd-font-ui);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 360px;
}

.viewer-hero-id {
  font-family: var(--dd-font-mono);
  font-size: 11.5px;
  color: var(--el-text-color-placeholder);
  letter-spacing: 0.3px;
}

.viewer-hero-status {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.5px;
  font-family: var(--dd-font-mono);
  // 运行/成功/失败的状态 chip，属于天然胶囊 → pill 档
  border-radius: var(--dd-radius-pill);

  &--running {
    background: color-mix(in srgb, var(--el-color-warning) 14%, transparent);
    color: var(--el-color-warning);
  }

  &--done {
    background: color-mix(in srgb, #22c55e 14%, transparent);
    color: color-mix(in srgb, #22c55e 80%, var(--el-text-color-primary));
  }

  &--error {
    background: color-mix(in srgb, var(--el-color-danger) 14%, transparent);
    color: var(--el-color-danger);
  }

  &--empty {
    background: color-mix(in srgb, var(--el-color-info) 18%, transparent);
    color: var(--el-color-info);
  }
}

.viewer-hero-meta {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.viewer-hero-meta-item {
  font-family: var(--dd-font-ui);
}

/* Status orb：状态底块，靠底色区分运行/成功/失败 */
.status-orb {
  position: relative;
  width: 22px;
  height: 22px;
  // 22×22 的图标底属于小型交互块 → control 档
  border-radius: var(--dd-radius-control);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.status-orb--running {
  background: color-mix(in srgb, var(--el-color-warning) 14%, transparent);
}

.status-orb--done {
  background: color-mix(in srgb, #22c55e 14%, transparent);
  color: color-mix(in srgb, #22c55e 80%, var(--el-text-color-primary));
}

.status-orb--error {
  background: color-mix(in srgb, var(--el-color-danger) 14%, transparent);
  color: var(--el-color-danger);
}

.status-orb--empty {
  background: color-mix(in srgb, var(--el-color-info) 18%, transparent);
  color: var(--el-color-info);
}

.status-orb-core {
  width: 8px;
  height: 8px;
  // 白名单：形状承载语义 —— 8×8 的呼吸点是「运行中」的状态灯，与 global.scss 的 .pulse-dot 同类，
  // 方化后就是一小块色斑、看不出是状态灯。两种 shape 模式下都固定圆形，不吃 --dd-radius-* 刻度。
  border-radius: 50%;
  background: var(--el-color-warning);
  animation: orb-core 1.4s ease-in-out infinite;
}

.status-orb-ripple {
  position: absolute;
  inset: 0;
  // 涟漪是从 .status-orb 里放大出来的一层，形状必须跟底块同档，否则放大过程中会露出错位的角
  border-radius: var(--dd-radius-control);
  background: color-mix(in srgb, var(--el-color-warning) 40%, transparent);
  animation: orb-ripple 1.8s ease-out infinite;
}

.status-switch-enter-active,
.status-switch-leave-active {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.status-switch-enter-from,
.status-switch-leave-to {
  opacity: 0;
  transform: scale(0.85);
}

/* Hero actions */
.viewer-hero-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.tool-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--viewer-border-soft, var(--el-border-color-light));
  background: var(--el-bg-color);
  color: var(--el-text-color-regular);
  // 工具栏按钮 → control 档
  border-radius: var(--dd-radius-control);
  font-size: 12px;
  font-family: var(--dd-font-mono);
  cursor: pointer;
  transition: color 0.15s, background 0.15s, border-color 0.15s;

  &:hover:not(:disabled):not(.tool-btn--close) {
    color: var(--el-color-primary);
    border-color: color-mix(in srgb, var(--el-color-primary) 40%, var(--el-border-color-light));
    background: color-mix(in srgb, var(--el-color-primary) 6%, transparent);
  }

  &:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  &--active {
    background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
    color: var(--el-color-primary);
    border-color: color-mix(in srgb, var(--el-color-primary) 40%, transparent);
  }

  &--close {
    width: 34px;
    height: 34px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--el-text-color-secondary);
    border-color: transparent;
    // 34×34 的图标按钮，与 .tool-btn 同档
    border-radius: var(--dd-radius-control);
    margin-left: 6px;
    position: relative;
    overflow: hidden;
    transition: color 0.25s;

    .el-icon {
      position: relative;
      z-index: 1;
    }

    // hover 时纯色底铺满，不再用缩放的渐变块浮起
    &::before {
      content: '';
      position: absolute;
      inset: 0;
      // hover 铺满的红底盖在按钮上，圆角必须跟按钮同档，否则圆角模式下四角会溢出按钮轮廓
      border-radius: var(--dd-radius-control);
      background: var(--el-color-danger);
      opacity: 0;
      transition: opacity 0.2s ease;
    }

    &:hover:not(:disabled) {
      color: #fff;
      border-color: transparent;
      background: transparent;

      &::before {
        opacity: 1;
      }
    }

    &:focus-visible {
      outline: 2px solid color-mix(in srgb, #ef4444 60%, transparent);
      outline-offset: 2px;
    }
  }
}

@media (prefers-reduced-motion: reduce) {
  .tool-btn--close {
    transition: none;

    &::before {
      transition: none;
    }
  }
}

.tool-btn-label {
  font-weight: 600;
  letter-spacing: 0.4px;
}

.auto-scroll-toggle {
  margin-left: 4px;
}

/* =============== Body =============== */
.viewer-body {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;

  &.log-font-sm {
    --viewer-log-font-size: 12px;
  }
  &.log-font-md {
    --viewer-log-font-size: 13.5px;
  }
  &.log-font-lg {
    --viewer-log-font-size: 15px;
  }
}

.viewer-log {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 18px 22px;
  font-family: var(--dd-font-mono);
  font-size: var(--viewer-log-font-size, 13.5px);
  line-height: 1.65;
  color: var(--dd-log-text-color, #e2e8f0);
  // 🔴 保持 0，不吃令牌 —— 必须写回来压掉 global.scss 里 .dd-log-surface 的 surface 档。
  // 这块深色日志区是【贴边内嵌】的：本文件下方把 .log-viewer-dialog .el-dialog__body 的
  // padding 设成了 0，.viewer-body 也没有内边距，所以它从 header 分隔线正下方一路铺满到
  // 弹窗底边、左右也顶到弹窗边框。带着自己的 10px 圆角的话，圆角模式下左上/右上会被切出
  // 两个缺口露出弹窗白底（缺口正好卡在 header 分隔线两端，很显眼）。
  // 外轮廓的圆角由 .log-viewer-dialog 的 surface 档 + overflow:hidden 统一去裁，这里必须是 0。
  // 同形态的另外三处（logs/index.vue、ScriptExecutionDialogs.vue、LogFileBrowser.vue）写法一致。
  border-radius: 0;
}

// 正文容器用 div + white-space:pre-wrap 代替 <pre>：
// Vue 模板编译器会原样保留 <pre> 内部的缩进空白，而这里正文要拆成多个子节点分块渲染，
// 缩进会被当成正文渲染出来。等宽字体来自父级 .viewer-log，换行语义由这里的 white-space 承担，
// 视觉与原来的 <pre> 完全一致。
.viewer-log-content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;

  &--nowrap {
    white-space: pre;
    word-break: normal;
  }
}

// 渲染窗口封顶提示：扁平虚线块，颜色全部从日志前景色派生，明暗两态自动适配
.log-omitted-notice {
  display: block;
  width: 100%;
  margin: 0 0 10px;
  padding: 6px 10px;
  border: 1px dashed color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 32%, transparent);
  // 可点击的提示小块（点一下展开更多日志）→ control 档
  border-radius: var(--dd-radius-control);
  background: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 8%, transparent);
  color: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 70%, transparent);
  font-family: var(--dd-font-mono);
  font-size: 11.5px;
  letter-spacing: 0.3px;
  text-align: left;
  white-space: normal;
  word-break: normal;
  cursor: pointer;
  transition: color 0.15s, background 0.15s, border-color 0.15s;

  &:hover {
    color: var(--dd-log-text-color, #e2e8f0);
    border-color: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 52%, transparent);
    background: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 14%, transparent);
  }
}

.viewer-message {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 10px;
  color: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 55%, transparent);
  font-size: 13px;

  &--error {
    color: color-mix(in srgb, var(--el-color-warning) 90%, transparent);
  }

  &--empty {
    color: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 70%, transparent);
  }
}

.viewer-message-hint {
  font-size: 11.5px;
  color: color-mix(in srgb, var(--dd-log-text-color, #e2e8f0) 40%, transparent);
  letter-spacing: 0.2px;
}

/* =============== Status bar =============== */
.viewer-statusbar {
  display: flex;
  justify-content: space-between;
  padding: 6px 22px;
  font-family: var(--dd-font-mono);
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  border-top: 1px solid var(--viewer-border-soft, var(--el-border-color-light));
  background: color-mix(in srgb, var(--el-fill-color-lighter) 60%, transparent);
  flex-shrink: 0;
}

.viewer-statusbar-group {
  display: inline-flex;
  gap: 14px;
}

.viewer-statusbar-item {
  letter-spacing: 0.4px;

  &--live {
    color: var(--el-color-warning);
    font-weight: 600;

    &::before {
      content: '● ';
      animation: pulse 1.6s ease-in-out infinite;
    }
  }

  &--error {
    color: var(--el-color-danger);
  }

  &--empty {
    color: var(--el-color-info);
  }
}

/* =============== Animations =============== */
@keyframes orb-core {
  0%, 100% { transform: scale(0.9); }
  50% { transform: scale(1.1); }
}

@keyframes orb-ripple {
  0% { transform: scale(0.7); opacity: 0.7; }
  100% { transform: scale(1.45); opacity: 0; }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

@media (prefers-reduced-motion: reduce) {
  .status-orb-core,
  .status-orb-ripple,
  .viewer-statusbar-item--live::before { animation: none; }
}

/* =============== Mobile =============== */
@media (max-width: 768px) {
  .viewer-hero {
    padding: 10px 12px;
    gap: 10px;
  }

  .viewer-hero-title {
    font-size: 14.5px;
    max-width: 60vw;
  }

  .viewer-hero-actions {
    width: 100%;
    justify-content: flex-end;
    gap: 4px;
  }

  .tool-btn-label {
    display: none;
  }

  .viewer-body {
    &.log-font-md {
      --viewer-log-font-size: 12.5px;
    }
  }

  .viewer-log {
    padding: 14px;
  }

  .viewer-statusbar {
    padding: 5px 14px;
    font-size: 10px;
  }
}
</style>

<!--
  独立的非 scoped style：专门处理 teleport 到 body 的 el-dialog 根元素。
  原因：LogViewer.vue 的 template root 就是 el-dialog 自身，而 el-dialog 会把 DOM teleport 到 body，
  在这种组合下 Vue scoped 的 data-v 属性往 teleport 元素传递不可靠，
  :deep(.log-viewer-dialog) 编译出的 [data-v-xxx] .log-viewer-dialog 选择器在实际 DOM 里没办法命中。
  改用非 scoped 块，编译后是纯 .log-viewer-dialog 选择器，不依赖 scope，一定生效。
  类名唯一 log-viewer-dialog不会污染其他组件。
-->
<style lang="scss">
.log-viewer-dialog {
  --viewer-border-soft: color-mix(in srgb, var(--el-border-color-light) 85%, transparent);

  width: min(1400px, 92vw);
  // 弹窗外壳 → surface 档
  border-radius: var(--dd-radius-surface);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  // 桌面端按内容观感收紧：
  // 保持足够阅读空间，但避免空日志时出现大面积“黑幕感”。
  height: clamp(680px, 85dvh, 920px);
  max-height: calc(100dvh - 56px);
  // align-center 模式下 el-overlay-dialog 是 flex 容器，用 margin: auto 让 dialog 垂直+水平居中；
  // 如果写成 margin: 0 auto 上下 margin 会变成 0，dialog 会贴到容器底部。
  margin: auto;

  // fullscreen 模式（由 :fullscreen="dialogFullscreen" prop 激活，对应 mobile），
  // 由 Element Plus 默认样式 .el-dialog.is-fullscreen { height:100%; ... } 接管，
  // 但我们的 height:90vh 优先级一样，需要显式让 fullscreen 时恢复全屏。
  &.is-fullscreen {
    width: 100%;
    height: 100%;
    max-height: 100%;
    // 保持 0，不吃令牌：全屏态下弹窗铺满整个视口，四角带圆角会露出遮罩层的黑边
    border-radius: 0;
    margin: 0;
  }

  .el-dialog__header {
    padding: 0;
    margin: 0;
    border-bottom: 1px solid var(--viewer-border-soft);
    flex-shrink: 0;
  }

  .el-dialog__body {
    padding: 0;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
}
</style>
