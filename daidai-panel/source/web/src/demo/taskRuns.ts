import { appendTaskRunLog, db, nowIso } from './db'
import type { DemoTask, DemoTaskLog } from './types'
import { LOG_STATUS_SUCCESS, TASK_STATUS_ENABLED, TASK_STATUS_RUNNING } from './types'

/**
 * 「手动点了运行」这件事在演示环境里的生命周期。
 *
 * 为什么需要它：`handleRun()`（views/tasks/index.vue:429-433）拿到 200 之后会立刻
 * `task.status = 2` 并打开实时日志弹窗。如果 mock 的 run 端点像之前那样直接落一条
 * 【已完成】的日志，那个弹窗就永远等不到任何数据，演示里最有观赏性的一幕直接没了。
 *
 * 所以这里把一次手动运行拆成两段：
 *   1. startDemoTaskRun()  —— 落一条运行中的日志、把任务置为运行中；
 *   2. finishDemoTaskRun() —— 收尾成一次成功执行。
 *
 * 收尾有两条触发路径，谁先到算谁（幂等）：
 *   - 假日志流（demo/sse.ts）吐完最后一行时主动收尾，这是正常路径；
 *   - 兜底定时器，防止访客压根没打开日志弹窗时留下一条永远「运行中」的记录
 *     —— 那会让仪表盘的「运行中的任务」越点越多，且再也降不回去。
 */

interface DemoTaskRun {
  log: DemoTaskLog
  /** 运行前的任务状态：手动运行一个「已禁用」的任务，跑完要回到已禁用而不是已启用 */
  previousStatus: number
  timer: ReturnType<typeof setTimeout>
}

/**
 * 兜底收尾时间。
 *
 * 必须明显长于假日志流的总时长（约 3.4 秒），否则定时器会抢在流前面把日志收掉，
 * 访客会看到「日志还在滚，任务却已经显示成功了」。
 */
const AUTO_FINISH_MS = 9000

const runs = new Map<number, DemoTaskRun>()

/** 手动运行：落一条运行中的日志，并把任务置为运行中 */
export function startDemoTaskRun(task: DemoTask): DemoTaskLog {
  // 同一个任务连点两次「运行」：先把上一次收尾掉，避免留下两条运行中的日志。
  finishDemoTaskRun(task.id)

  const previousStatus = task.status === TASK_STATUS_RUNNING ? TASK_STATUS_ENABLED : task.status
  // duration 传 0 ⇒ started_at 就是此刻，且 duration / ended_at 都是 null（运行中的语义）
  const log = appendTaskRunLog(task, 'running', 0)

  task.status = TASK_STATUS_RUNNING
  task.pid = 20000 + Math.floor(Math.random() * 20000)
  task.updated_at = nowIso()

  runs.set(task.id, {
    log,
    previousStatus,
    timer: setTimeout(() => {
      finishDemoTaskRun(task.id)
    }, AUTO_FINISH_MS),
  })

  return log
}

/**
 * 把「运行中」收尾成一次成功执行。
 *
 * 幂等：找不到运行中的日志就什么都不做，所以假日志流与兜底定时器谁先到都安全。
 *
 * @param transcript 假日志流刚刚滚过的正文。留在日志上是为了避免「画面突变」——
 *   LogViewer 收到 done 之后会立刻回查 latest-log 并整体替换渲染内容
 *   （LogViewer.vue:239-241 的 resetLogOutput + appendLogChunk），
 *   不留正文的话访客会看到刚看完的日志被换成另一套措辞的版本。
 */
export function finishDemoTaskRun(taskId: number, transcript?: string): void {
  const run = runs.get(taskId)
  if (run) {
    clearTimeout(run.timer)
    runs.delete(taskId)
  }

  const current = db()
  const task = current.tasks.find((row) => row.id === taskId)

  // 只认「当前这份内存数据里还在」的日志对象：横幅上的「重置演示数据」会整体换掉
  // state，此刻注册表里的旧引用必须原地作废，不能拿去改一条已经不存在的记录。
  // 没有注册表条目时（例如访客直接打开 fixture 里那两个运行中任务的实时日志），
  // 就按任务去找那条运行中的记录。
  const log = run && current.logs.includes(run.log)
    ? run.log
    : current.logs.find((row) => row.task_id === taskId && row.kind === 'running')

  if (!log) return

  // 真实耗时按 started_at 算。上限收到任务超时的九成：fixture 里的运行中日志是
  // 「已经跑了一会儿」的状态，访客把标签页挂在后台几十分钟再来点开的话，
  // 直接用真实差值会算出一个远超该任务超时时间的「成功」，一眼假。
  const elapsedSeconds = (Date.now() - new Date(log.started_at).getTime()) / 1000
  const cap = task && task.timeout > 0 ? task.timeout * 0.9 : Number.POSITIVE_INFINITY
  const duration = Math.round(Math.min(Math.max(elapsedSeconds, 0.1), cap) * 10) / 10

  log.kind = 'ok'
  log.status = LOG_STATUS_SUCCESS
  log.duration = duration
  log.ended_at = nowIso()
  if (transcript) log.content = transcript

  if (task) {
    if (task.status === TASK_STATUS_RUNNING) {
      task.status = run?.previousStatus ?? TASK_STATUS_ENABLED
    }
    task.pid = null
    task.last_run_status = LOG_STATUS_SUCCESS
    task.last_running_time = duration
    task.updated_at = nowIso()
  }
}

/**
 * 「停止」按钮走的路径：只撤掉兜底定时器。
 *
 * 日志与任务状态由 adapter 的 `PUT /tasks/:id/stop` 自己改成「已终止」，
 * 这里如果不撤定时器，9 秒后它会把那条已经终止的记录又翻成成功。
 */
export function cancelDemoTaskRun(taskId: number): void {
  const run = runs.get(taskId)
  if (!run) return
  clearTimeout(run.timer)
  runs.delete(taskId)
}
