<script setup lang="ts">
import { computed } from "vue";
import { formatDateTime } from "@/utils/datetime";

const props = defineProps<{
  version: string;
  lastCheckTime: string;
  autoUpdateEnabled: boolean;
}>();

const emit = defineEmits<{
  "update:autoUpdateEnabled": [value: boolean];
}>();

// 空值文案沿用「从未检查」，比统一的 "-" 更能说明是「一次都没查过」而不是「查了但没记下来」
const lastCheckDisplay = computed(() =>
  formatDateTime(props.lastCheckTime, "从未检查"),
);

// 下次检查是【未来时间】，只能用绝对时间：formatRelativeTime 只描述过去
const nextCheckDisplay = computed(() => {
  if (!props.lastCheckTime) return "-";
  const next = new Date(props.lastCheckTime).getTime() + 24 * 60 * 60 * 1000;
  return formatDateTime(next);
});

const statusText = computed(() => {
  return props.autoUpdateEnabled ? "系统已是最新版本" : "未开启自动检查";
});
</script>

<template>
  <el-card shadow="never" class="usc">
    <div class="usc-layout">
      <div class="usc-header">
        <span class="usc-title">系统更新设置</span>
        <span class="usc-subtitle"
          >保持系统为最新版本以获得更好的稳定性和性能</span
        >
      </div>

      <div class="usc-switch-card">
        <div class="usc-switch-icon-wrap">
          <svg
            class="usc-switch-svg"
            viewBox="0 0 24 24"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              d="M12 4V2M12 4C7.58 4 4 7.58 4 12H2M12 4C16.42 4 20 7.58 20 12H22M12 22V20M12 20C16.42 20 20 16.42 20 12M12 20C7.58 20 4 16.42 4 12"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
            />
            <path
              d="M15 9L9 15M9 9L15 15"
              stroke="currentColor"
              stroke-width="0"
            />
            <path
              d="M12 8V12L14 14"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </div>
        <div class="usc-switch-body">
          <div class="usc-switch-title">自动更新</div>
          <div class="usc-switch-desc">
            每 24
            小时自动检查一次新版本，检测到新版本后会在空闲时段尝试更新（不下载生效无变化）。
          </div>
        </div>
        <el-switch
          :model-value="autoUpdateEnabled"
          inline-prompt
          active-text="开"
          inactive-text="关"
          @change="(val: boolean) => emit('update:autoUpdateEnabled', val)"
        />
      </div>

      <div class="usc-footer">
        <div class="usc-footer-item">
          <span class="usc-footer-dot usc-footer-dot--blue"></span>
          <div class="usc-footer-content">
            <span class="usc-footer-label">最后检查时间</span>
            <span class="usc-footer-value">{{ lastCheckDisplay }}</span>
          </div>
        </div>
        <div class="usc-footer-item">
          <span class="usc-footer-dot usc-footer-dot--green"></span>
          <div class="usc-footer-content">
            <span class="usc-footer-label">当前状态</span>
            <span class="usc-footer-value usc-footer-value--status">{{
              statusText
            }}</span>
          </div>
        </div>
        <div class="usc-footer-item">
          <span class="usc-footer-dot usc-footer-dot--cyan"></span>
          <div class="usc-footer-content">
            <span class="usc-footer-label">下次检查时间</span>
            <span class="usc-footer-value">{{ nextCheckDisplay }}</span>
          </div>
        </div>
      </div>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
.usc {
  // 卡片本体属容器类表面 → surface 档
  border-radius: var(--dd-radius-surface);
  // 扁平化：不再用投影制造浮起，仅靠 1px 描边与页面底色区分
  border: 1px solid var(--el-border-color-lighter);
  height: 100%;

  :deep(.el-card__body) {
    padding: 0;
    height: 100%;
  }
}

.usc-layout {
  padding: 24px;
  display: flex;
  flex-direction: column;
  height: 100%;
}

.usc-header {
  margin-bottom: 20px;
}

.usc-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  display: block;
  margin-bottom: 4px;
}

.usc-subtitle {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.usc-switch-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px;
  // 卡片内的独立区块（四周有 24px 留白、不贴边）→ surface 档
  border-radius: var(--dd-radius-surface);
  // 扁平化：装饰渐变换成主色浅底纯色
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-border-color-lighter);
  margin-bottom: 20px;
  flex: 1;
}

.usc-switch-icon-wrap {
  width: 40px;
  height: 40px;
  // 40×40 图标色底属控件类表面 → control 档（不做成正圆，避免与头像混淆）
  border-radius: var(--dd-radius-control);
  // 扁平化：装饰渐变与辉光换成主色纯底
  background: var(--el-color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.usc-switch-svg {
  width: 20px;
  height: 20px;
  color: #fff;
}

.usc-switch-body {
  flex: 1;
  min-width: 0;
}

.usc-switch-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 2px;
}

.usc-switch-desc {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}

.usc-footer {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-top: auto;
}

.usc-footer-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.usc-footer-dot {
  width: 8px;
  height: 8px;
  // 🔴 固定 2px，既不进圆形白名单、也不吃 control 档（v3.1.0 新立的「≤12px 小色标」判据）。
  //
  // 原注释说它是「状态灯」，但模板里这三个点是写死的三个装饰 bullet
  // （--blue / --green / --cyan 分别钉在「最后检查时间 / 当前状态 / 下次检查时间」三行上），
  // 颜色永远不随 autoUpdateEnabled 或任何运行状态变——绿点在「未开启自动检查」时照样是绿的。
  // 它表达的是「这是第几项信息」而不是「现在是什么状态」，做成正圆反而是误导性的状态灯。
  //
  // 也不能改成吃 control：8×8 的盒子上 control 的 6px 会被 CSS 圆角等比收缩夹到 4px，
  // 结果仍是正圆，等于没改。写死一个小于半边长的值才稳得住方形轮廓。
  border-radius: 2px;
  flex-shrink: 0;
  margin-top: 4px;

  &--blue {
    background: #3b82f6;
  }
  &--green {
    background: #10b981;
  }
  &--cyan {
    background: #36cfc9;
  }
}

.usc-footer-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.usc-footer-label {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.usc-footer-value {
  font-size: 12px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  font-family: var(--dd-font-mono, monospace);

  &--status {
    color: #10b981;
    font-family: var(--dd-font-ui), sans-serif;
    font-weight: 600;
  }
}

@media (max-width: 768px) {
  .usc-footer {
    grid-template-columns: 1fr;
    gap: 10px;
  }

  .usc-switch-card {
    flex-direction: column;
    align-items: stretch;
    text-align: center;
  }

  .usc-switch-icon-wrap {
    align-self: center;
  }
}
</style>
