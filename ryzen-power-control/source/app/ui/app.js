const BASE = "/app/ryzen-power-control";
const $ = (id) => document.getElementById(id);
let refreshing = false;
let powerDraftDirty = false;
let targetLimits = null;
let noticeHoldUntil = 0;
let appReady = false;
let cpuGateBlocked = false;
let versionCheckStarted = false;
let detectedCpuFamily = "Unknown";
let currentLog = "";
const LIMIT_KEYS = ["stapm", "fast", "slow"];
const LIMIT_LABELS = { stapm: "STAPM", fast: "PPT 瞬时", slow: "PPT 慢速" };
let supportedLimitKeys = [];
const CPU_FAMILY_KEY = "ryzen-power-control.cpu-family";

function watts(value) {
  return Number(value).toFixed(1);
}

function isNewerVersion(latest, current) {
  const pattern = /^\d+(?:\.\d+){2}$/;
  if (!pattern.test(latest) || !pattern.test(current)) return false;
  const latestParts = latest.split(".").map(Number);
  const currentParts = current.split(".").map(Number);
  for (let i = 0; i < latestParts.length; i += 1) {
    if (latestParts[i] !== currentParts[i]) return latestParts[i] > currentParts[i];
  }
  return false;
}

async function checkForNewVersion(currentVersion) {
  try {
    const response = await fetch(
      `https://raw.githubusercontent.com/LANMIN-X/RyzenAdj-for-fnOS/main/LATEST_VERSION?t=${Date.now()}`,
      { cache: "no-store" },
    );
    if (!response.ok) throw new Error(`版本文件请求失败 (${response.status})`);
    const latestVersion = (await response.text()).trim();
    const updateAvailable = isNewerVersion(latestVersion, currentVersion);
    const versionLink = $("app-version");
    if (updateAvailable) {
      versionLink.href = "https://github.com/LANMIN-X/RyzenAdj-for-fnOS/releases";
      versionLink.target = "_blank";
      versionLink.rel = "noopener noreferrer";
    } else {
      versionLink.removeAttribute("href");
      versionLink.removeAttribute("target");
      versionLink.removeAttribute("rel");
    }
    $("update-indicator").hidden = !updateAvailable;
  } catch (error) {
    $("app-version").removeAttribute("href");
    $("app-version").removeAttribute("target");
    $("app-version").removeAttribute("rel");
    $("update-indicator").hidden = true;
    console.warn("检查应用更新失败", error);
  }
}

function showNotice(message, error = false, success = false) {
  const box = $("notice");
  box.textContent = message;
  box.classList.toggle("alert-error", error);
  box.classList.toggle("alert-success", success);
  noticeHoldUntil = Date.now() + 5000;
}

function showPolledNotice(message, error = false) {
  if (Date.now() < noticeHoldUntil) return;
  const box = $("notice");
  box.textContent = message;
  box.classList.toggle("alert-error", error);
  box.classList.remove("alert-success");
  if (error) noticeHoldUntil = Date.now() + 5000;
}

function setMeter(id, value, min, max) {
  const meter = $(id).parentElement;
  const percent = Math.min(100, Math.max(0, ((value - min) / (max - min)) * 100));
  $(id).style.width = `${percent}%`;
  meter.setAttribute("aria-valuenow", String(Math.round(value)));
}

async function request(path, body) {
  const options = body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
  const response = await fetch(BASE + path, options);
  const data = await response.json();
  if (!response.ok || data.ok === false) {
    throw new Error(data.error || `请求失败 (${response.status})`);
  }
  return data;
}

function showCpuChecking() {
  $("cpu-gate-title").textContent = "正在检查处理器";
  $("cpu-gate-message").textContent = "正在读取 CPU 支持信息…";
  $("cpu-gate-series").hidden = true;
  $("cpu-gate-power").hidden = true;
  $("cpu-gate-support").hidden = true;
  $("cpu-gate-actions").hidden = true;
  $("cpu-gate-unsupported-actions").hidden = true;
  $("cpu-gate-retry-row").hidden = true;
  openCpuGate();
}

function openCpuGate() {
  const gate = $("cpu-gate");
  if (!gate.open) gate.showModal();
  gate.focus();
}

function prepareCpuGate(data) {
  detectedCpuFamily = data.cpu_family || "Unknown";
  const support = data.power_limit_support || {};
  supportedLimitKeys = LIMIT_KEYS.filter((key) => support[key]);
  if (data.cpu_supported) {
    let acceptedFamily = "";
    try {
      acceptedFamily = localStorage.getItem(CPU_FAMILY_KEY) || "";
    } catch (error) {
      console.warn("无法读取 CPU 支持确认状态", error);
    }
    if (acceptedFamily === detectedCpuFamily) {
      appReady = true;
      $("app-shell").hidden = false;
      $("cpu-gate").close();
      return true;
    }
    $("cpu-gate-title").textContent = "处理器支持检查";
    $("cpu-gate-message").textContent = "RyzenAdj 支持当前处理器。";
    $("cpu-gate-series").textContent = `当前处理器系列：${detectedCpuFamily} · ${data.cpu_series}`;
    $("cpu-gate-series").hidden = false;
    const supported = supportedLimitKeys.map((key) => LIMIT_LABELS[key]);
    const unavailable = LIMIT_KEYS.filter((key) => !support[key]).map((key) => LIMIT_LABELS[key]);
    $("cpu-gate-power").textContent = `可调整：${supported.join("、")}${unavailable.length ? `；不可调整：${unavailable.join("、")}` : ""}`;
    $("cpu-gate-power").hidden = false;
    $("cpu-gate-support").hidden = true;
    $("cpu-gate-actions").hidden = false;
    $("cpu-gate-unsupported-actions").hidden = true;
    $("cpu-gate-retry-row").hidden = true;
  } else {
    $("cpu-gate-title").textContent = "当前处理器暂不支持";
    $("cpu-gate-message").textContent = data.cpu_error
      ? `RyzenAdj 仅适用于部分 AMD Ryzen APU / 移动处理器。当前无法读取处理器信息：${data.cpu_error}`
      : "RyzenAdj 仅适用于部分 AMD Ryzen APU / 移动处理器。";
    $("cpu-gate-series").textContent = detectedCpuFamily === "Unknown"
      ? "无法识别当前处理器系列。"
      : `检测到处理器系列：${detectedCpuFamily}`;
    $("cpu-gate-series").hidden = false;
    $("cpu-gate-power").hidden = true;
    $("cpu-gate-support").hidden = false;
    $("cpu-gate-actions").hidden = true;
    $("cpu-gate-unsupported-actions").hidden = false;
    $("cpu-gate-retry-row").hidden = true;
  }
  cpuGateBlocked = true;
  openCpuGate();
  return false;
}

function showCpuCheckError(error) {
  cpuGateBlocked = true;
  $("cpu-gate-title").textContent = "无法检测处理器支持情况";
  $("cpu-gate-message").textContent = error.message;
  $("cpu-gate-series").hidden = true;
  $("cpu-gate-power").hidden = true;
  $("cpu-gate-support").hidden = true;
  $("cpu-gate-actions").hidden = true;
  $("cpu-gate-unsupported-actions").hidden = true;
  $("cpu-gate-retry-row").hidden = false;
  openCpuGate();
}

function setTestStatus(test) {
  const status = $("test-result");
  const wrap = status.parentElement;
  wrap.classList.toggle("active", test.running);
  wrap.classList.toggle("complete", !test.running && test.result === "测试完成");
  wrap.classList.toggle("idle", !test.running && test.result !== "测试完成");
  status.textContent = test.running
    ? `运行中 · 剩余 ${test.remaining_seconds} 秒 · ${test.workers} 个工作进程`
    : (test.result || "尚未运行");
  const button = $("toggle-test");
  button.dataset.running = String(test.running);
  button.textContent = test.running ? "停止测试" : "开始测试";
  button.classList.toggle("btn-primary", !test.running);
  button.classList.toggle("btn-error", test.running);
  $("duration").disabled = test.running;
  $("test-temperature").disabled = test.running;
  $("duration-slider").disabled = test.running;
}

async function refresh() {
  if (refreshing || cpuGateBlocked) return;
  refreshing = true;
  try {
    const data = await request("/api/status");
    if (!versionCheckStarted) {
      versionCheckStarted = true;
      checkForNewVersion(data.version);
    }
    if (!appReady && !prepareCpuGate(data)) return;
    $("machine").textContent = `内核 RyzenAdj ${data.ryzenadj_version}`;
    $("app-version").textContent = `应用版本 v${data.version}`;
    $("limit-stapm").textContent = watts(data.limits.stapm);
    $("limit-fast").textContent = watts(data.limits.fast);
    $("limit-slow").textContent = watts(data.limits.slow);
    setMeter("bar-stapm", data.limits.stapm, data.limit_range.min, data.limit_range.max);
    setMeter("bar-fast", data.limits.fast, data.limit_range.min, data.limit_range.max);
    setMeter("bar-slow", data.limits.slow, data.limit_range.min, data.limit_range.max);
    $("hero-limit").textContent = watts(data.limits.fast);
    const powerPercent = Math.min(100, Math.max(0, (data.power.fast / data.limits.fast) * 100));
    const meter = $("power-meter");
    meter.setAttribute("aria-valuenow", String(Math.round(powerPercent)));
    $("power-meter-progress").style.strokeDashoffset = String(100 - powerPercent);
    $("power-percent").textContent = `${Math.round(powerPercent)}%`;
    $("power-fast").textContent = watts(data.power.fast);
    $("power-stapm").textContent = watts(data.power.stapm);
    $("power-slow").textContent = watts(data.power.slow);
    $("power-socket").textContent = data.power.socket == null ? "—" : watts(data.power.socket);
    $("power-apu").textContent = data.power.apu == null ? "—" : watts(data.power.apu);
    $("temperature").textContent = Number(data.temperature).toFixed(1);
    const frequency = data.cpu_frequency;
    $("cpu-frequency").textContent = frequency?.mhz == null ? "—" : (frequency.mhz / 1000).toFixed(2);
    $("cpu-frequency").title = frequency?.error || "当前各核心采样频率中的最高值";
    const support = data.power_limit_support || {};
    supportedLimitKeys = LIMIT_KEYS.filter((key) => support[key]);
    const baseline = data.baseline;
    for (const button of document.querySelectorAll(".restore-bios-button")) {
      button.disabled = !baseline || supportedLimitKeys.length === 0;
    }
    if (baseline) {
      $("baseline").textContent = `STAPM ${watts(baseline.stapm)} / 快速 ${watts(baseline.fast)} / 慢速 ${watts(baseline.slow)} W`;
    } else {
      $("baseline").textContent = "未记录";
    }
    $("watts").min = data.limit_range.min;
    $("watts").max = data.limit_range.max;
    $("watts-slider").min = data.limit_range.min;
    $("watts-slider").max = data.limit_range.max;
    for (const key of LIMIT_KEYS) {
      $(`limit-${key}-slider`).min = data.limit_range.min;
      $(`limit-${key}-slider`).max = data.limit_range.max;
    }
    $("limit-range").textContent = `${data.limit_range.min}–${data.limit_range.max} W`;
    for (const key of LIMIT_KEYS) {
      $(`limit-${key}-unavailable`).hidden = Boolean(support[key]);
    }
    for (const id of ["bar-stapm", "bar-fast", "bar-slow"]) {
      $(id).parentElement.setAttribute("aria-valuemax", String(data.limit_range.max));
    }
    $("power-enabled").checked = data.control_enabled;
    for (const control of $("set-form").querySelectorAll("input, button[type=submit]")) {
      control.disabled = !data.control_enabled;
    }
    for (const key of LIMIT_KEYS) {
      $(`limit-${key}-slider`).disabled = !data.control_enabled || !support[key];
    }
    for (const button of $("set-form").querySelectorAll("button[type=submit]")) {
      button.disabled = !data.control_enabled || supportedLimitKeys.length === 0;
    }
    $("control-status").textContent = data.control_enabled
      ? (data.separate_limits ? "接管中 · 独立设置" : `接管中 · ${data.target_watts} W`)
      : (baseline ? "未接管 · 基准已恢复" : "未接管 · 未记录基准");
    targetLimits = data.target_limits || (data.target_watts == null
      ? data.limits
      : Object.fromEntries(LIMIT_KEYS.map((key) => [key, data.target_watts])));
    if (powerDraftDirty && data.control_enabled) {
      const sameMode = $("separate-limits").checked === Boolean(data.separate_limits);
      const sameValues = data.separate_limits
        ? supportedLimitKeys.every((key) => Number($(`limit-${key}-slider`).value) === Math.round(targetLimits[key]))
        : Number($("watts").value) === data.target_watts;
      if (sameMode && sameValues) powerDraftDirty = false;
    }
    if (!powerDraftDirty) {
      $("separate-limits").checked = Boolean(data.separate_limits);
      $("shared-limit-controls").hidden = data.separate_limits;
      $("separate-limit-controls").hidden = !data.separate_limits;
      $("adjust-description").textContent = data.separate_limits
        ? `分别设置 ${supportedLimitKeys.map((key) => LIMIT_LABELS[key]).join("、")}。`
        : `同时应用到 ${supportedLimitKeys.map((key) => LIMIT_LABELS[key]).join("、")}。`;
      const sharedWatts = data.target_watts ?? Math.round(targetLimits[supportedLimitKeys[0]] ?? data.limits.fast);
      $("watts").value = String(sharedWatts);
      $("watts-slider").value = String(sharedWatts);
      for (const key of LIMIT_KEYS) {
        const value = Math.round(targetLimits[key]);
        $(`limit-${key}-slider`).value = String(value);
        $(`limit-${key}-value`).textContent = `${watts(value)} W`;
      }
    }
    setTestStatus(data.test);
    if (data.baseline_error) {
      showPolledNotice(data.baseline_error, true);
    } else {
      const controlStatus = data.control_enabled
        ? (data.separate_limits ? "独立功耗墙接管中" : `功耗接管中 · ${data.target_watts} W`)
        : "未接管";
      showPolledNotice(`${data.cpu_family} · ${data.cpu_series} · ${controlStatus}`);
    }
  } catch (error) {
    if (appReady) showPolledNotice(`连接失败：${error.message}`, true);
    else showCpuCheckError(error);
  } finally {
    refreshing = false;
  }
}

async function runAction(action, success) {
  showNotice("正在更新…");
  try {
    await action();
    showNotice(success, false, true);
    await refresh();
  } catch (error) {
    showNotice(error.message, true);
  }
}

$("cpu-gate-enter").addEventListener("click", () => {
  try {
    localStorage.setItem(CPU_FAMILY_KEY, detectedCpuFamily);
  } catch (error) {
    console.warn("无法保存 CPU 支持确认状态", error);
  }
  cpuGateBlocked = false;
  appReady = true;
  $("app-shell").hidden = false;
  $("cpu-gate").close();
  refresh();
});

function forceEnterCpuGate() {
  cpuGateBlocked = false;
  appReady = true;
  $("app-shell").hidden = false;
  $("cpu-gate").close();
  refresh();
}

$("cpu-gate-cancel").addEventListener("click", forceEnterCpuGate);
$("cpu-gate-close").addEventListener("click", forceEnterCpuGate);

$("cpu-gate-retry").addEventListener("click", () => {
  cpuGateBlocked = false;
  showCpuChecking();
  refresh();
});

$("cpu-gate").addEventListener("cancel", (event) => event.preventDefault());

async function showLogs() {
  const dialog = $("logs-dialog");
  dialog.showModal();
  currentLog = "";
  $("download-logs").disabled = true;
  $("clear-logs").disabled = true;
  $("ryzenadj-log").textContent = "正在读取日志…";
  try {
    const logs = await request("/api/logs");
    currentLog = [logs.ryzenadj, logs.kernel].filter(Boolean).join("\n\n");
    $("ryzenadj-log").textContent = currentLog || "本次运行暂无日志。";
    $("download-logs").disabled = !currentLog;
  } catch (error) {
    $("ryzenadj-log").textContent = `读取日志失败：${error.message}`;
  } finally {
    $("clear-logs").disabled = false;
  }
}

$("download-logs").addEventListener("click", () => {
  if (!currentLog) return;
  const url = URL.createObjectURL(new Blob([currentLog], { type: "text/plain;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = "ryzenadj-logs.txt";
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
});

$("view-logs").addEventListener("click", showLogs);
$("clear-logs").addEventListener("click", async () => {
  $("clear-logs").disabled = true;
  try {
    await request("/api/logs/clear", {});
    currentLog = "";
    $("download-logs").disabled = true;
    $("ryzenadj-log").textContent = "应用日志已清理，后续操作会重新记录。系统内核日志仍可在重新打开弹窗后查看。";
  } catch (error) {
    $("ryzenadj-log").textContent = `清理日志失败：${error.message}`;
  } finally {
    $("clear-logs").disabled = false;
  }
});
$("close-logs").addEventListener("click", () => $("logs-dialog").close());
$("close-logs-footer").addEventListener("click", () => $("logs-dialog").close());

function confirmLowPower() {
  const dialog = $("low-power-dialog");
  if (dialog.open) return Promise.resolve(false);
  const button = $("low-power-apply");
  const label = button.querySelector(".semi-button-content");
  const deadline = performance.now() + 5000;
  dialog.returnValue = "";
  const update = () => {
    const remaining = Math.max(0, Math.ceil((deadline - performance.now()) / 1000));
    button.disabled = remaining > 0;
    button.classList.toggle("semi-button-disabled", button.disabled);
    button.setAttribute("aria-disabled", String(button.disabled));
    label.textContent = remaining ? `同意应用(${remaining}s)` : "同意应用";
  };
  update();
  return new Promise((resolve) => {
    const timer = setInterval(update, 250);
    dialog.addEventListener("close", () => {
      clearInterval(timer);
      resolve(dialog.returnValue === "apply");
    }, { once: true });
    dialog.showModal();
  });
}

$("low-power-cancel").addEventListener("click", () => $("low-power-dialog").close());
$("low-power-close").addEventListener("click", () => $("low-power-dialog").close());
$("low-power-apply").addEventListener("click", () => {
  if (!$("low-power-apply").disabled) $("low-power-dialog").close("apply");
});

$("set-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  let body;
  let success;
  if (supportedLimitKeys.length === 0) {
    showNotice("当前处理器没有可调整的功耗项。", true);
    return;
  }
  if ($("separate-limits").checked) {
    const limits = Object.fromEntries(supportedLimitKeys.map((key) => [key, Number($(`limit-${key}-slider`).value)]));
    for (const key of supportedLimitKeys) {
      const input = $(`limit-${key}-slider`);
      const value = limits[key];
      if (!Number.isInteger(value) || value < Number(input.min) || value > Number(input.max)) {
        showNotice(`${input.getAttribute("aria-label")}需为 ${input.min}–${input.max} 之间的整数瓦数。`, true);
        return;
      }
    }
    body = { limits };
    success = `${supportedLimitKeys.map((key) => LIMIT_LABELS[key]).join("、")}功耗墙已分别设置。`;
  } else {
    const value = Number($("watts").value);
    const min = Number($("watts").min);
    const max = Number($("watts").max);
    if (!Number.isInteger(value) || value < min || value > max) {
      showNotice(`请输入 ${min}–${max} 之间的整数瓦数。`, true);
      return;
    }
    body = { watts: value };
    success = `${supportedLimitKeys.map((key) => LIMIT_LABELS[key]).join("、")}功耗墙已设置为 ${value} W。`;
  }
  const limits = body.limits ? Object.values(body.limits) : [body.watts];
  if (limits.some((value) => value < 5) && !await confirmLowPower()) return;
  runAction(async () => {
    await request("/api/power/set", body);
    powerDraftDirty = false;
  }, success);
});

$("power-enabled").addEventListener("change", (event) => {
  const enabled = event.currentTarget.checked;
  powerDraftDirty = false;
  runAction(
    () => request("/api/power/control", { enabled }),
    enabled ? "功耗接管已开启。" : "已恢复记录的功耗基准，接管已关闭。",
  );
});

for (const button of document.querySelectorAll(".restore-bios-button")) {
  button.addEventListener("click", () => runAction(async () => {
    await request("/api/power/restore", {});
    powerDraftDirty = false;
  }, "已还原 BIOS 三项功耗墙，滑块已同步。"));
}

$("watts-slider").addEventListener("input", (event) => {
  powerDraftDirty = true;
  $("watts").value = event.currentTarget.value;
});

$("watts").addEventListener("input", (event) => {
  powerDraftDirty = true;
  const value = Number(event.currentTarget.value);
  if (Number.isFinite(value)) {
    $("watts-slider").value = String(Math.min(
      Number($("watts-slider").max),
      Math.max(Number($("watts-slider").min), value),
    ));
  }
});

for (const key of LIMIT_KEYS) {
  const slider = $(`limit-${key}-slider`);
  slider.addEventListener("input", () => {
    powerDraftDirty = true;
    $(`limit-${key}-value`).textContent = `${watts(slider.value)} W`;
  });
}

$("separate-limits").addEventListener("change", (event) => {
  powerDraftDirty = true;
  const separate = event.currentTarget.checked;
  $("shared-limit-controls").hidden = separate;
  $("separate-limit-controls").hidden = !separate;
  $("adjust-description").textContent = separate
    ? `分别设置 ${supportedLimitKeys.map((key) => LIMIT_LABELS[key]).join("、")}。`
    : `同时应用到 ${supportedLimitKeys.map((key) => LIMIT_LABELS[key]).join("、")}。`;
  const watts = separate
    ? Number($("watts").value)
    : Number($(`limit-${supportedLimitKeys[0]}-slider`).value);
  if (!separate) {
    $("watts").value = String(watts);
    $("watts-slider").value = String(watts);
  } else {
    for (const key of supportedLimitKeys) {
      $(`limit-${key}-slider`).value = String(watts);
      $(`limit-${key}-value`).textContent = `${watts.toFixed(1)} W`;
    }
  }
});

function updateTestHint() {
  const seconds = $("duration").value || "—";
  const temperature = $("test-temperature").value || "—";
  $("test-hint").textContent = `持续时间 ${seconds}秒，${temperature}°C 自动停止`;
}

$("test-temperature").addEventListener("input", updateTestHint);
updateTestHint();

$("duration-slider").addEventListener("input", (event) => {
  $("duration").value = event.currentTarget.value;
  updateTestHint();
});

$("duration").addEventListener("input", (event) => {
  updateTestHint();
  const value = Number(event.currentTarget.value);
  if (Number.isFinite(value)) {
    $("duration-slider").value = String(Math.min(
      Number($("duration-slider").max),
      Math.max(Number($("duration-slider").min), value),
    ));
  }
});

$("toggle-test").addEventListener("click", () => {
  if ($("toggle-test").dataset.running === "true") {
    runAction(() => request("/api/test/stop", {}), "负载测试已停止。");
    return;
  }

  const input = $("duration");
  const seconds = Number(input.value);
  const min = Number(input.min);
  const max = Number(input.max);
  if (!Number.isInteger(seconds) || seconds < min || seconds > max) {
    showNotice(`请输入 ${min}–${max} 之间的整数秒数。`, true);
    return;
  }
  const temperatureInput = $("test-temperature");
  const temperature_limit = Number(temperatureInput.value);
  if (!Number.isInteger(temperature_limit) || temperature_limit < Number(temperatureInput.min)
      || temperature_limit > Number(temperatureInput.max)) {
    showNotice(`请输入 ${temperatureInput.min}–${temperatureInput.max}°C 之间的整数温度。`, true);
    return;
  }
  runAction(() => request("/api/test/start", { seconds, temperature_limit }), "全核负载测试已启动。");
});

showCpuChecking();
refresh();
window.setInterval(refresh, 2000);
