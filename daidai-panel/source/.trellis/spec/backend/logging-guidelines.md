# 日志规范

> 当前项目既有 Gin 请求日志，也有面板运行日志，还会把部分日志写入数据目录。

---

## 当前真实做法

- 使用标准库 `log`
- Gin 使用自定义 writer 输出请求日志
- 面板启动时会根据运行模式把日志写到 `panel.log` 或 stdout
- 数据库层和部分兼容迁移也会通过 `log.Printf` 记录信息

---

## 应该记录什么

- 启动/关闭关键流程
- 数据库初始化和兼容迁移结果
- 调度器、资源监控、自动更新等关键后台流程
- 会影响用户使用的失败信息
- 安全相关事件，如登录、锁定、白名单限制等

---

## 不应该记录什么

- 明文密码
- token、refresh token、secret、私钥
- 用户敏感配置的完整原文
- 不必要的大段重复噪声日志

---

## 日志风格建议

- 文案尽量直接，便于排查。
- 如果是兼容逻辑、迁移逻辑、历史遗留兜底逻辑，建议加中文注释解释代码目的。
- 对可恢复的异常优先 `log.Printf`，对必须终止启动的问题再 `log.Fatalf`。

---

## 常见错误

- 把敏感信息直接打印到日志。
- 同一错误在循环中重复刷屏。
- 发生失败时没有打关键上下文，后面难排查。
- 只写“失败了”，不写哪个步骤失败。

## Scenario: 任务日志流中的终端覆盖刷新

### 1. Scope / Trigger
- Trigger: 修改 `server/service/script_runner.go`、`server/service/task_executor.go`、`server/service/scheduler.go`、`server/handler/log.go` 这条任务日志实时输出链时必须看本节。
- 原因: 进度条、下载器、CLI 状态条常用裸 `\r` 回到当前行开头覆盖内容；如果后端在读取脚本输出或写 SSE 时把 `\r` 直接洗成 `\n`，前端就只能把每次刷新渲染成新行，日志会被严重刷屏。

### 2. Signatures
- 脚本输出回调签名: `type OnOutputFunc func(chunk string)`
- 任务实时日志 SSE: `GET /api/v1/logs/:id/stream`
- 历史日志读取: `TaskLog.Content` / `TaskLog.LogPath`

### 3. Contracts
- `OnOutputFunc` 传递的是原始输出片段，不保证一定是完整一行。
- 片段内允许出现三种边界:
  - `\n`: 正常换行
  - `\r\n`: Windows 风格换行
  - 裸 `\r`: 终端单行覆盖刷新
- `writeSSEData` 只能把 `\r\n` 归一成 `\n`，不能把裸 `\r` 再改成 `\n`。
- **裸 `\r` 必须落在一条 `data:` 行的末尾，不能埋在行中间。** 这是上一条的补充约束，两条同时成立才算正确：
  - SSE 线格式里 CR、LF、CRLF **都是行终止符**。`data: aaa\rbbb\n` 会被标准 `EventSource` 拆成两行：`data: aaa` 和 `bbb`；后半段没有字段名，按规范整行**丢弃** —— `bbb` 静默消失。
  - 实现方式是**按裸 `\r` 分帧**：每碰到一个后面还有内容的裸 `\r`，就让它成为当前帧最后一条 `data:` 行的结尾，**写完帧终止空行**，剩余内容**另起一帧**。字符一个不增不减。
  - ⚠️ **绝不能改成「同一帧里多条以 `\r` 结尾的 `data:` 行」。** SSE 规定同一个 event 的多条 `data:` 行用 `\n` 拼接，那样裸 `\r` 会变成 `\r\n`，而前端 `web/src/utils/ansi.ts` 把 `\r\n` 当**真实换行**，进度条立刻刷屏 —— 正好是本节要防的那个回归。
  - 分帧方案对前端**零改动**：`web/src/utils/sse.ts` 按 `\n\n` 切帧、且对 `data:` 行末尾的 `\r` 刻意不 trim；`LogViewer.vue` 与 `logs/index.vue` 拿到相邻消息是**首尾相接 append**、不补任何分隔符。所以多帧拼起来与改动前的单帧**逐字节一致**。
  - **不变量**：把所有帧的 data 值按顺序首尾相接拼回去，必须逐字节等于输入（`\r\n`→`\n` 除外）。
- 任务执行器 / 调度器把输出写入 `TinyLog` 和日志文件时，必须原样写入片段，不能统一补 `+ "\n"`。

### 4. Validation & Error Matrix
- 输出只包含普通文本 + `\n` -> 正常按多行日志展示
- 输出包含 `\r\n` -> 视为真实换行
- 输出包含裸 `\r` -> 必须保留，交给前端按“覆盖当前行”处理
- 如果在任一后端环节把裸 `\r` 改成 `\n` -> 进度条日志会刷屏，属于行为回归
- 裸 `\r` 埋在 `data:` 行中间（`data: aaa\rbbb`）-> 标准 `EventSource` 把 `bbb` 当成无字段名的行**静默丢弃**，用户看到「进度条后面的输出没了」
- 裸 `\r` 之后另起一帧（`data: aaa\r` + 帧终止空行 + `data: bbb`）-> 内容完整、覆盖刷新语义保持、前端零改动，这是唯一正确的组合
- 裸 `\r` 之后不另起帧、只在同一帧里多写一条 `data:` 行 -> 同帧多条 data 按规范用 `\n` 拼接，裸 `\r` 变成 `\r\n`，进度条重新刷屏，同样是回归

### 5. Good/Base/Bad Cases
- Good: `xx 10%\rxx 20%\rxx 30%\n完成\n` 最终前端只显示一条持续刷新的进度行，再接一条“完成”
- Base: 普通 `print/console.log` 输出不受影响，仍然是逐行日志
- Bad: SSE 层把 `\r` 直接替换为 `\n`，导致 `10%`、`20%`、`30%` 全部堆成独立多行
- Bad: SSE 层只按 `\n` 切行，`\r` 留在 `data:` 行中间 —— 自研解析器看着正常，换成标准 `EventSource`（或中间有严格代理）时 `\r` 后面的内容整段消失，且**没有任何报错**
- Bad: 把 `\r` 后面的内容放进**同一帧**的下一条 `data:` 行（而不是另起一帧）—— 内容回来了，但同帧多条 data 会被 `\n` 拼接，裸 `\r` 变 `\r\n`，进度条重新刷屏

### 6. Tests Required
- 后端测试: `cd server && go test ./...`
- 回归点:
  - `server/handler/log_stream_regression_test.go` 断言 `writeSSEData` 会保留裸 `\r`
  - 同一个文件还要断言**分帧位置**：裸 `\r` 只能出现在某条 `data:` 行的最后一个字符，绝不能出现在行中间（一条断言两件事，否则「保留了但埋在中间」会全绿通过）
  - 任务执行日志链编译和现有 handler/service 测试全部通过

### 7. Wrong vs Correct
#### Wrong
```go
data = strings.ReplaceAll(data, "\r", "\n")
fmt.Fprintf(tinyLog, "%s\n", line)
logMgr.Write(fullLogPath, line+"\n")
```

```go
// 错误：只按 \n 切，裸 \r 被埋在 data: 行中间。
// 自研解析器看着没问题，标准 EventSource 会把 \r 后面的半截当成无字段名的行丢掉。
for _, line := range strings.Split(data, "\n") {
    fmt.Fprintf(w, "data: %s\n", line)
}
```

#### Correct
```go
data = strings.ReplaceAll(data, "\r\n", "\n")
fmt.Fprint(tinyLog, chunk)
logMgr.Write(fullLogPath, chunk)
```

```go
// 正确：\n 照旧在帧内切行；裸 \r 之后【另起一帧】，
// \r 本身留在上一帧最后一条 data: 行的末尾 —— 字符保住了，也不会被 EventSource 吞掉。
// 前端零改动：帧与帧首尾相接 append，拼回去与改动前逐字节一致。
data = strings.ReplaceAll(data, "\r\n", "\n")
for {
    segment, rest := data, ""
    // idx+1 == len(data) 表示 \r 已经在整段末尾，本来就落在行末，不用再切。
    if idx := strings.IndexByte(data, '\r'); idx >= 0 && idx+1 < len(data) {
        segment, rest = data[:idx+1], data[idx+1:]
    }
    for _, line := range strings.Split(segment, "\n") {
        fmt.Fprintf(w, "data: %s\n", line)
    }
    fmt.Fprint(w, "\n") // 帧终止空行必须写在循环内
    if rest == "" {
        return
    }
    data = rest
}
```
