<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { taskViewApi, type TaskView, type TaskViewFilter, type TaskViewSortRule } from '@/api/taskView'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Close, Edit, Setting } from '@element-plus/icons-vue'
import { useResponsive } from '@/composables/useResponsive'
import ViewManagementDialog from './ViewManagementDialog.vue'

const emit = defineEmits<{
  'view-change': [filters: TaskViewFilter[], sortRules: TaskViewSortRule[]]
}>()

const { dialogFullscreen } = useResponsive()
const views = ref<TaskView[]>([])
// null 有两层含义：一是「不带任何视图筛选」，二是「全部」标签处于高亮。
// 「全部」被隐藏后第二层含义失效 —— 此时仍可能短暂停在 null（比如刚删掉当前选中的视图），
// 标签栏不会有任何高亮项，但 emit 出去的 filters 同样是空数组、列表不带筛选，两者是一致的。
// 随后的 applyViewFallback() 会把它落到第一个可见视图。
const activeViewId = ref<number | null>(null)
const showDialog = ref(false)
// 提交在途锁：弹窗要等请求成功才关，期间不锁按钮连点就会建出多个同名视图。
// 写法与同目录 ViewManagementDialog 的 saving 保持一致。
const saving = ref(false)
const isEditMode = ref(false)
const editingViewId = ref<number | null>(null)
const showManagementDialog = ref(false)

const visibleViews = computed(() => views.value.filter(view => !view.hidden))

// 「全部」是模板里硬编码的内置筛选项，库里没有对应行，后端的 hidden 字段够不到它，
// 它的显隐只能落到本地存储。默认 '0'（显示），老用户升级后第一眼观感不变。
const VIEW_ALL_HIDDEN_STORAGE_KEY = 'dd:tasks:view_all_hidden'

function readStoredViewAllHidden() {
  if (typeof window === 'undefined') {
    return false
  }
  try {
    return window.localStorage.getItem(VIEW_ALL_HIDDEN_STORAGE_KEY) === '1'
  } catch {
    // 隐私模式下读 localStorage 会直接抛错，必须吞掉：否则整个 setup 挂掉、标签栏整块白
    return false
  }
}

function persistViewAllHidden(hidden: boolean) {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(VIEW_ALL_HIDDEN_STORAGE_KEY, hidden ? '1' : '0')
  } catch {
    // 存储不可用只影响「下次进页还记不记得」，不该阻断当前这次交互
  }
}

const allTabHidden = ref(readStoredViewAllHidden())

// 保底规则：一个可见视图都没有时，忽略隐藏设置强制把「全部」放回来。
// 否则标签栏只剩右侧两个图标按钮，用户既没有可点的筛选项、也退不回不带筛选的状态。
const showAllTab = computed(() => !allTabHidden.value || visibleViews.value.length === 0)

const filterFields = [
  { value: 'command', label: '命令' },
  { value: 'name', label: '名称' },
  { value: 'cron_expression', label: '定时规则' },
  { value: 'status', label: '状态' },
  { value: 'labels', label: '标签' },
  { value: 'subscription', label: '订阅' }
]

const statusOptions = [
  { label: '已启用 / 空闲中', value: '1' },
  { label: '已禁用', value: '0' },
  { label: '运行中', value: '2' },
  { label: '排队中', value: '0.5' },
]

const filterOperators = [
  { value: 'contains', label: '包含' },
  { value: 'not_contains', label: '不包含' },
  { value: 'equals', label: '等于' },
  { value: 'not_equals', label: '不等于' }
]

const sortDirections = [
  { value: 'asc', label: '升序' },
  { value: 'desc', label: '降序' }
]

const editForm = ref({
  name: '',
  filters: [{ field: 'command', operator: 'contains', value: '' }] as TaskViewFilter[],
  sortRules: [] as TaskViewSortRule[]
})

async function loadViews() {
  try {
    views.value = await taskViewApi.list()
  } catch {
    views.value = []
  }
  applyViewFallback()
}

// 视图列表或「全部」显隐变化后校正当前高亮项，三处硬回退点（首次加载 / 管理弹窗保存 / 删除视图）
// 都汇到这里，避免各写一份规则跑偏：
//   1. 当前选中的视图被删除或被隐藏了 —— 不能继续停在一个看不见的标签上；
//   2. 「全部」被隐藏而 activeViewId 还停在 null —— null 是「全部」的高亮语义，
//      必须落到第一个可见视图，否则标签栏一个高亮都没有；
//   3. 一个可见视图都没有时 showAllTab 会强制把「全部」放回来，此时停在 null 反而是对的。
function applyViewFallback() {
  if (activeViewId.value !== null) {
    const current = views.value.find(v => v.id === activeViewId.value)
    if (current && !current.hidden) {
      return
    }
  } else if (showAllTab.value) {
    return
  }
  // 走到这里说明当前高亮项已经不可见：优先回落「全部」，「全部」也被隐藏时落到第一个可见视图
  selectView(showAllTab.value ? null : (visibleViews.value[0]?.id ?? null))
}

function openManagementDialog() {
  showManagementDialog.value = true
}

async function handleManagementSaved(allHidden: boolean) {
  // 「全部」不进 taskViewApi.reorder 的提交列表，弹窗只把结果回传上来，由这里写本地存储
  allTabHidden.value = allHidden
  persistViewAllHidden(allHidden)
  await loadViews()
}

function selectView(viewId: number | null) {
  activeViewId.value = viewId
  if (!viewId) {
    emit('view-change', [], [])
    return
  }
  const view = views.value.find(v => v.id === viewId)
  if (view) {
    try {
      const filters = JSON.parse(view.filters || '[]')
      const sortRules = JSON.parse(view.sort_rules || '[]')
      emit('view-change', filters, sortRules)
    } catch {
      emit('view-change', [], [])
    }
  }
}

function openCreateDialog() {
  isEditMode.value = false
  editingViewId.value = null
  editForm.value = {
    name: '',
    filters: [{ field: 'command', operator: 'contains', value: '' }],
    sortRules: []
  }
  showDialog.value = true
}

function openEditDialog(view: TaskView) {
  isEditMode.value = true
  editingViewId.value = view.id
  let filters: TaskViewFilter[] = []
  let sortRules: TaskViewSortRule[] = []
  try {
    filters = JSON.parse(view.filters || '[]')
  } catch { /* ignore */ }
  try {
    sortRules = JSON.parse(view.sort_rules || '[]')
  } catch { /* ignore */ }
  if (filters.length === 0) {
    filters = [{ field: 'command', operator: 'contains', value: '' }]
  }
  editForm.value = {
    name: view.name,
    filters,
    sortRules
  }
  showDialog.value = true
}

function addFilter() {
  editForm.value.filters.push({ field: 'command', operator: 'contains', value: '' })
}

function removeFilter(index: number) {
  editForm.value.filters.splice(index, 1)
}

function addSortRule() {
  editForm.value.sortRules.push({ field: 'name', direction: 'asc' })
}

function removeSortRule(index: number) {
  editForm.value.sortRules.splice(index, 1)
}

function isStatusField(filter: TaskViewFilter) {
  return filter.field === 'status'
}

async function handleSave() {
  if (!editForm.value.name.trim()) {
    ElMessage.warning('请输入视图名称')
    return
  }
  const validFilters = editForm.value.filters.filter(f => f.value.trim() !== '')
  if (validFilters.length === 0) {
    ElMessage.warning('请至少添加一个有效的筛选条件')
    return
  }
  // 两处校验早退都过了才置位，否则校验失败会把按钮永久锁死
  saving.value = true
  try {
    const payload = {
      name: editForm.value.name,
      filters: JSON.stringify(validFilters),
      sort_rules: JSON.stringify(editForm.value.sortRules)
    }
    if (isEditMode.value && editingViewId.value) {
      await taskViewApi.update(editingViewId.value, payload)
      ElMessage.success('视图更新成功')
      // If editing the active view, re-apply its filters
      if (activeViewId.value === editingViewId.value) {
        emit('view-change', validFilters, editForm.value.sortRules)
      }
    } else {
      await taskViewApi.create(payload)
      ElMessage.success('视图创建成功')
    }
    showDialog.value = false
    await loadViews()
  } catch (err: any) {
    // task_views.name 现在是唯一索引，后端把冲突翻译成了面向用户的中文 400
    // （新建与改名两条路径都会返回「同名任务视图已存在」）。
    // 这里必须优先取后端文案，否则那条提示在 UI 上是死的，用户只会看到笼统的「创建失败」。
    ElMessage.error(err?.response?.data?.error || (isEditMode.value ? '更新失败' : '创建失败'))
  } finally {
    saving.value = false
  }
}

async function handleDelete(viewId: number) {
  try {
    await ElMessageBox.confirm('确认删除该视图吗？', '删除确认', { type: 'warning' })
    await doDeleteView(viewId)
  } catch {}
}

async function doDeleteView(viewId: number) {
  try {
    await taskViewApi.delete(viewId)
    ElMessage.success('已删除')
    if (activeViewId.value === viewId) {
      // 先清掉高亮与筛选（被删的视图不该继续作用于列表），
      // 具体落到「全部」还是第一个可见视图，交给刷新后的 applyViewFallback() 统一判断，
      // 因为此刻 views 里还留着刚删掉的那一条，就地挑「第一个可见视图」可能挑到它自己。
      selectView(null)
    }
    await loadViews()
  } catch {}
}

onMounted(loadViews)

defineExpose({ loadViews })
</script>

<template>
  <div class="view-manager">
    <!-- 槽内只放筛选项（全部 + 各视图），动作按钮（新建 / 视图管理）放槽外：
         「选哪一个」与「做什么」语义分开，顺带消掉了原来「全部」24px、视图 32px 的高度不一致 -->
    <div class="view-tabs">
      <div class="view-seg">
        <button
          v-if="showAllTab"
          :class="['view-tab', { active: activeViewId === null }]"
          @click="selectView(null)"
        >
          全部
        </button>
        <button
          v-for="view in visibleViews"
          :key="view.id"
          :class="['view-tab', { active: activeViewId === view.id }]"
          @click="selectView(view.id)"
        >
          {{ view.name }}
        </button>
      </div>
      <div class="view-tabs__actions">
        <el-tooltip content="新建视图" placement="top">
          <el-button @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip v-if="views.length > 0" content="视图管理" placement="top">
          <el-button @click="openManagementDialog">
            <el-icon><Setting /></el-icon>
          </el-button>
        </el-tooltip>
      </div>
    </div>

    <ViewManagementDialog
      v-model="showManagementDialog"
      :views="views"
      :all-hidden="allTabHidden"
      @saved="handleManagementSaved"
      @edit="openEditDialog"
      @delete="doDeleteView"
    />

    <el-dialog
      v-model="showDialog"
      :title="isEditMode ? '编辑视图' : '创建视图'"
      width="600px"
      :fullscreen="dialogFullscreen"
      :lock-scroll="false"
    >
      <el-form :label-width="dialogFullscreen ? 'auto' : '90px'" :label-position="dialogFullscreen ? 'top' : 'right'">
        <el-form-item label="视图名称" required>
          <el-input v-model="editForm.name" placeholder="请输入视图名称" />
        </el-form-item>

        <el-form-item label="筛选条件" required>
          <div class="filter-list">
            <div v-for="(filter, index) in editForm.filters" :key="index" class="filter-row">
              <el-select v-model="filter.field" style="width: 120px" size="small" @change="filter.value = ''">
                <el-option v-for="f in filterFields" :key="f.value" :label="f.label" :value="f.value" />
              </el-select>
              <el-select v-model="filter.operator" style="width: 100px" size="small">
                <el-option v-for="op in filterOperators" :key="op.value" :label="op.label" :value="op.value" />
              </el-select>
              <el-select
                v-if="isStatusField(filter)"
                v-model="filter.value"
                placeholder="选择状态"
                size="small"
                style="flex: 1"
              >
                <el-option v-for="opt in statusOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
              </el-select>
              <el-input v-else v-model="filter.value" placeholder="请输入内容" size="small" style="flex: 1" />
              <el-button v-if="editForm.filters.length > 1" :icon="Delete" size="small" @click="removeFilter(index)" />
            </div>
            <el-button size="small" type="primary" link @click="addFilter">+ 新增筛选条件</el-button>
          </div>
        </el-form-item>

        <el-form-item label="排序方式">
          <div class="filter-list">
            <div v-for="(rule, index) in editForm.sortRules" :key="index" class="filter-row">
              <el-select v-model="rule.field" style="width: 120px" size="small">
                <el-option v-for="f in filterFields" :key="f.value" :label="f.label" :value="f.value" />
              </el-select>
              <el-select v-model="rule.direction" style="width: 100px" size="small">
                <el-option v-for="d in sortDirections" :key="d.value" :label="d.label" :value="d.value" />
              </el-select>
              <el-button :icon="Delete" size="small" @click="removeSortRule(index)" />
            </div>
            <el-button size="small" type="primary" link @click="addSortRule">+ 新增排序方式</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="saving" @click="handleSave">{{ isEditMode ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.view-manager {
  margin-bottom: 12px;
}

.view-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

// 灰底槽：数值逐条对齐 tasks/index.vue 的 .status-tabs，让上下两排分段控件观感一致
.view-seg {
  display: inline-flex;
  background: var(--el-fill-color-light);
  // 灰底槽取 control 档，与全站通用类 .dd-seg-group、tasks/index.vue 的 .status-tabs 同档。
  // 槽 padding 3px ⇒ 内侧曲率 = 槽圆角 - 3px；取 control(6px) 时内侧 3px 小于项的 6px，
  // 选中项的角稳稳落在槽内侧曲线之内；取 surface(10px) 时内侧 7px 反而大于 6px，会露出错位。
  border-radius: var(--dd-radius-control);
  padding: 3px;
  gap: 2px;
  // 视图数量由用户决定、可能很多，这里保留原来的换行而不是照抄 status-tabs 的单行排布。
  // 取舍：换行会让灰底槽变成两行、把下方工具栏推低；改成横向滚动虽然能锁死高度，
  // 但滚动条会遮住选中项、也没有溢出提示，权衡后选「宁可换行也别把视图藏起来」。
  flex-wrap: wrap;
  // inline-flex 的基准宽是 max-content，窄屏下不加这条会顶破容器让页面横向滚
  max-width: 100%;
}

.view-tab {
  padding: 6px 14px;
  // 槽内的分段项 → control 档
  border-radius: var(--dd-radius-control);
  // 透明描边占位，选中时只换 border-color，避免出现 1px 的尺寸跳动
  border: 1px solid transparent;
  background: transparent;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  // 时长/缓动只能取令牌：写死毫秒会绕过 prefers-reduced-motion 下把令牌压到 1ms 的降级
  transition:
    color var(--dd-motion-fast) var(--dd-ease-standard),
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    border-color var(--dd-motion-fast) var(--dd-ease-standard);
  white-space: nowrap;

  &:hover {
    color: var(--el-text-color-primary);
  }

  // 从 el-button 换成原生 button 后丢了 EP 的焦点环，键盘走查会看不出焦点落在哪一项。
  // offset 取负值让焦点环画在按钮内侧：槽内 gap 只有 2px，正向外扩会压到相邻项上。
  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--el-color-primary) 45%, transparent);
    outline-offset: -1px;
    // 焦点环沿 border-radius 描边，要跟分段项本身同档，否则圆角模式下环是方的、项是圆的
    border-radius: var(--dd-radius-control);
  }

  &.active {
    background: var(--el-bg-color);
    color: var(--el-color-primary);
    border-color: var(--el-border-color-lighter);
    font-weight: 600;
  }
}

// 动作按钮在槽外，高度对齐灰底槽（3 + 33 + 3 = 39px），
// 不再出现原来「图标按钮 24px vs 分段控件 39px」那种半截高的落差。
.view-tabs__actions {
  display: flex;
  align-items: center;
  gap: 6px;

  // 锚点是自己模板里的元素，压 el-button 用 :deep()；
  // 同时清掉 EP 自带的 .el-button + .el-button{margin-left:12px}，间距只由 gap 决定
  :deep(.el-button) {
    height: 39px;
    width: 39px;
    padding: 0;
    margin-left: 0;
  }
}

.filter-list {
  width: 100%;
}

.filter-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

@media (max-width: 768px) {
  .filter-row {
    flex-wrap: wrap;
  }
}
</style>
