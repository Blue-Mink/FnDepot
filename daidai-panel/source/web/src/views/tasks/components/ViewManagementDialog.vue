<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Rank, View, Hide, Edit, Delete, Lock } from '@element-plus/icons-vue'
import { taskViewApi, type TaskView } from '@/api/taskView'
import { useResponsive } from '@/composables/useResponsive'

interface ManagedView extends TaskView {
  // Local working copy of `hidden` that may diverge from the saved value until
  // the user clicks 保存. The original `hidden` is kept on the TaskView shape.
  _hidden: boolean
}

const props = defineProps<{
  modelValue: boolean
  views: TaskView[]
  // 「全部」的当前显隐状态。它是标签栏里硬编码的内置项、库里没有对应行，
  // 所以只能由父组件从 localStorage 读来传进来，再由本弹窗把结果原样回传。
  allHidden: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [allHidden: boolean]
  edit: [view: TaskView]
  delete: [viewId: number]
}>()

const { dialogFullscreen } = useResponsive()
const saving = ref(false)
const managed = ref<ManagedView[]>([])
// 「全部」显隐的本地工作副本，和各视图的 _hidden 一样，点保存前不落地
const allTabHidden = ref(props.allHidden)
const listContainer = ref<HTMLElement | null>(null)
let sortableInstance: any = null
let sortableLoader: Promise<any> | null = null

// 计数只统计自定义视图，刻意不把「全部」算进去：它不占 sort_order、不能删除、也不在下面的列表里，
// 算进去会让「共 N 个」与用户数得到的行数对不上。文案里直接写明「自定义视图」，避免口径歧义。
const hiddenCount = computed(() => managed.value.filter(v => v._hidden).length)
const visibleCount = computed(() => managed.value.length - hiddenCount.value)

function cloneToManaged(source: TaskView[]): ManagedView[] {
  return source.map(v => ({ ...v, _hidden: Boolean(v.hidden) }))
}

function loadSortable() {
  if (!sortableLoader) {
    sortableLoader = import('sortablejs').then(mod => mod.default)
  }
  return sortableLoader
}

async function initSortable() {
  teardownSortable()
  if (!listContainer.value) return
  try {
    const Sortable = await loadSortable()
    sortableInstance = Sortable.create(listContainer.value, {
      animation: 150,
      handle: '.view-drag-handle',
      ghostClass: 'view-row-ghost',
      chosenClass: 'view-row-chosen',
      dragClass: 'view-row-dragging',
      forceFallback: true,
      onEnd: (evt: any) => {
        const { oldIndex, newIndex } = evt
        if (oldIndex == null || newIndex == null || oldIndex === newIndex) return
        const [moved] = managed.value.splice(oldIndex, 1)
        if (!moved) return
        managed.value.splice(newIndex, 0, moved)
      }
    })
  } catch (err) {
    console.warn('failed to init sortable for view manager', err)
  }
}

function teardownSortable() {
  if (sortableInstance) {
    sortableInstance.destroy()
    sortableInstance = null
  }
}

function toggleHidden(view: ManagedView) {
  view._hidden = !view._hidden
}

function handleEdit(view: ManagedView) {
  emit('edit', view as TaskView)
  emit('update:modelValue', false)
}

async function handleDelete(view: ManagedView) {
  try {
    await ElMessageBox.confirm(`确认删除视图「${view.name}」吗？`, '删除确认', { type: 'warning' })
    emit('delete', view.id)
  } catch {
    // cancelled
  }
}

async function handleSave() {
  saving.value = true
  try {
    // 「全部」不参与 sort_order 重编号、也不进 reorder 的提交列表 —— 库里根本没有它这一行，
    // 它的显隐由父组件写本地存储。所以一个自定义视图都没有时也仍然要走到下面的 emit。
    if (managed.value.length > 0) {
      // Dense re-numbering keeps sort_order contiguous and mirrors the
      // visible list order.
      const payload = managed.value.map((view, index) => ({
        id: view.id,
        sort_order: (index + 1) * 10,
        hidden: view._hidden
      }))
      await taskViewApi.reorder(payload)
    }
    ElMessage.success('视图设置已保存')
    emit('saved', allTabHidden.value)
    emit('update:modelValue', false)
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || err?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function handleClose(visible: boolean) {
  emit('update:modelValue', visible)
}

watch(
  () => props.modelValue,
  async (open) => {
    if (open) {
      managed.value = cloneToManaged(props.views)
      // 每次打开都从父组件当前值重新取，避免上次取消掉的改动残留在工作副本里
      allTabHidden.value = props.allHidden
      await nextTick()
      await initSortable()
    } else {
      teardownSortable()
    }
  }
)

watch(
  () => props.views,
  (next) => {
    if (props.modelValue) {
      managed.value = cloneToManaged(next)
    }
  },
  { deep: true }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="视图管理"
    width="520px"
    :fullscreen="dialogFullscreen"
    :lock-scroll="false"
    :close-on-click-modal="false"
    @update:model-value="handleClose"
  >
    <div class="view-manager-hint">
      拖动左侧把手调整顺序，右侧开关控制是否在标签栏展示；「全部」是内置项，只能改显隐。
      <!-- 计数刻意只报自定义视图的口径，并在文案里写明，避免与下方列表行数对不上 -->
      <span class="view-manager-counts">自定义视图 {{ managed.length }} 个 · 显示 {{ visibleCount }} · 隐藏 {{ hiddenCount }}</span>
    </div>

    <!-- 「全部」是标签栏里硬编码的内置筛选项，库里没有对应行：
         不可拖拽（不占 sort_order）、不可编辑（没有筛选条件）、不可删除，只给一个显示开关。
         放在 listContainer 之外，否则 sortable 会把它算进拖拽索引，和 managed 的下标对不上。 -->
    <div
      class="view-manager-row is-builtin"
      :class="{ 'is-hidden': allTabHidden }"
    >
      <span class="view-drag-handle is-locked" title="内置项，不参与排序">
        <el-icon><Lock /></el-icon>
      </span>
      <span class="view-row-name">
        全部
        <span class="view-row-note">内置项，不带任何筛选条件</span>
        <!-- 自定义视图全隐藏时标签栏会保底把「全部」放回来，这里提前讲清楚，
             免得用户保存后以为开关没生效 -->
        <span v-if="allTabHidden && visibleCount === 0" class="view-row-note is-warning">
          自定义视图全部隐藏时，标签栏仍会保底显示「全部」
        </span>
      </span>
      <div class="view-row-actions">
        <el-tooltip :content="allTabHidden ? '在标签栏隐藏' : '在标签栏显示'" placement="top">
          <el-switch
            :model-value="!allTabHidden"
            inline-prompt
            :active-icon="View"
            :inactive-icon="Hide"
            @update:model-value="allTabHidden = !allTabHidden"
          />
        </el-tooltip>
      </div>
    </div>

    <div v-if="managed.length === 0" class="view-manager-empty">
      还没有任何自定义视图，先从标签栏右侧的「+」创建一个试试。
    </div>
    <div v-else ref="listContainer" class="view-manager-list">
      <div
        v-for="view in managed"
        :key="view.id"
        class="view-manager-row"
        :class="{ 'is-hidden': view._hidden }"
        :data-id="view.id"
      >
        <span class="view-drag-handle" :title="'拖动排序'">
          <el-icon><Rank /></el-icon>
        </span>
        <span class="view-row-name">{{ view.name }}</span>
        <div class="view-row-actions">
          <el-tooltip content="编辑" placement="top">
            <el-button size="small" type="primary" plain @click="handleEdit(view)">
              <el-icon><Edit /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip content="删除" placement="top">
            <el-button size="small" type="danger" plain @click="handleDelete(view)">
              <el-icon><Delete /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :content="view._hidden ? '在标签栏隐藏' : '在标签栏显示'" placement="top">
            <el-switch
              :model-value="!view._hidden"
              inline-prompt
              :active-icon="View"
              :inactive-icon="Hide"
              @update:model-value="toggleHidden(view)"
            />
          </el-tooltip>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button @click="handleClose(false)">取消</el-button>
      <!-- 不再按 managed.length 禁用：一个自定义视图都没有时，「全部」的显隐开关仍然要能保存 -->
      <el-button
        type="primary"
        :loading="saving"
        @click="handleSave"
      >
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.view-manager-empty {
  // 上方现在恒定有一行「全部」固定项，留出间距免得两块贴在一起
  padding: 20px 0 4px;
  text-align: center;
  color: var(--el-text-color-secondary);
}

.view-manager-hint {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 12px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.view-manager-counts {
  color: var(--el-text-color-placeholder);
}

.view-manager-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  // 「全部」固定项排在列表之外、不跟着一起滚，这里补一段间距接上
  margin-top: 6px;
  max-height: 420px;
  overflow-y: auto;
  padding-right: 4px;
}

.view-manager-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  background: var(--el-fill-color-lighter);
  border: 1px solid transparent;
  // 视图列表里的一行卡片（行间有 6px 间隔，不是贴边内嵌）→ surface 档
  border-radius: var(--dd-radius-surface);
  transition: border-color 0.15s, background 0.15s, opacity 0.15s;

  &:hover {
    border-color: var(--el-border-color);
  }

  &.is-hidden {
    opacity: 0.55;
  }

  // 「全部」固定项：换成卡片底 + 常亮 1px 描边与下方可拖拽的视图行区分开（不靠阴影/圆角）。
  // hover 也不点亮描边 —— 它没有拖拽/编辑/删除，点亮反而会误导成「这行能拖」。
  &.is-builtin,
  &.is-builtin:hover {
    background: var(--el-bg-color);
    border-color: var(--el-border-color-lighter);
  }
}

.view-drag-handle {
  cursor: grab;
  color: var(--el-text-color-placeholder);
  display: inline-flex;
  align-items: center;
  padding: 2px;

  &:active {
    cursor: grabbing;
  }

  // 内置项的把手位只用来占位对齐，光标保持默认，免得用户以为它也能拖
  &.is-locked,
  &.is-locked:active {
    cursor: default;
  }
}

.view-row-name {
  flex: 1;
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  word-break: break-word;
}

// 内置项的次级说明：字号与字重都降一档与名称拉开层级。
// 这里靠 display:block 自己换行，而不是把 .view-row-name 改成 column 弹性容器 ——
// 那会改掉所有自定义视图行的布局模型，为一行说明文字不值当。
.view-row-note {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  font-weight: 400;
  line-height: 1.4;
  color: var(--el-text-color-secondary);

  &.is-warning {
    color: var(--el-color-warning);
  }
}

.view-row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.view-row-ghost {
  opacity: 0.4;
  background: var(--el-color-primary-light-9);
}

.view-row-chosen {
  background: var(--el-fill-color);
}

/* 拖拽中不再靠阴影浮起，改用描边标出当前拖动的行 */
.view-row-dragging {
  border-color: var(--el-border-color);
}
</style>
