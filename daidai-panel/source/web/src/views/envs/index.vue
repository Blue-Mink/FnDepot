<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick, computed, watch, type CSSProperties } from 'vue'
import { envApi, type EnvNameOption } from '@/api/env'
import { ElMessage, ElMessageBox } from 'element-plus'
import { copyText } from '@/utils/clipboard'
import EnvBatchGroupDialog from './components/EnvBatchGroupDialog.vue'
import EnvBatchRenameDialog from './components/EnvBatchRenameDialog.vue'
import EnvEditDialog from './components/EnvEditDialog.vue'
import EnvImportDialog from './components/EnvImportDialog.vue'
import { useResponsive } from '@/composables/useResponsive'
import DdSplitButton from '@/components/ui/DdSplitButton.vue'
import type { SplitButtonItem } from '@/components/ui/DdSplitButton.vue'

const envTableDensityStorageKey = 'daidai-env-table-density'
const envPageSizeStorageKey = 'daidai-env-page-size'
const envAllFetchBatchSize = 100
const { isMobile } = useResponsive()

type EnvPageSizeSelection = '20' | '50' | '100' | 'all'

type EnvFormModel = {
  id: number
  name: string
  value: string
  remarks: string
  group?: string
  groups: string[]
}

const envList = ref<any[]>([])
const loading = ref(true)
const total = ref(0)
const page = ref(1)
const initialPageSizeSelection = readEnvPageSizeSelection()
const pageSizeSelection = ref<EnvPageSizeSelection>(initialPageSizeSelection)
const pageSize = ref(initialPageSizeSelection === 'all' ? envAllFetchBatchSize : Number(initialPageSizeSelection))
const keyword = ref('')
const groupFilters = ref<string[]>([])
const groups = ref<string[]>([])
// 变量名筛选：与分组筛选并列的一等筛选维度（issue #118 建议 2）。
// 同名多条是刻意功能（多账号场景，见 server/model/env_var.go），所以「按变量名收敛」是高频诉求，
// 而搜索框那套四字段模糊 OR 根本限定不了字段。
const nameFilters = ref<string[]>([])
// 选项里的 count 是【全库同名条数】（服务端 /envs/names 的口径），不跟随当前筛选：
// 它与后端 PUT /envs/by-name 冲突时那句「存在 N 条名为 X 的环境变量」是同一个 N。
const nameOptions = ref<EnvNameOption[]>([])
const selectedIds = ref<number[]>([])
const selectedIdSet = computed(() => new Set(selectedIds.value))
const selectedCountInCurrentPage = computed(() =>
  envList.value.filter((item) => selectedIdSet.value.has(item.id)).length
)
const showAllEnvs = computed(() => pageSizeSelection.value === 'all')
const pinnedCountInCurrentPage = computed(() =>
  envList.value.filter((item) => isTopPinned(item)).length
)
const currentPageOffset = computed(() => (showAllEnvs.value ? 0 : (page.value - 1) * pageSize.value))
const statusFilter = ref<'' | 'enabled' | 'disabled'>('')
const showFooterBar = computed(() => total.value > 0 || selectedCountInCurrentPage.value > 0)
const showPager = computed(() => !showAllEnvs.value && total.value > pageSize.value)
const sortableEnabled = computed(() => envList.value.length >= 2)
const pageSizeOptions: Array<{ label: string; value: EnvPageSizeSelection }> = [
  { label: '20 / 页', value: '20' },
  { label: '50 / 页', value: '50' },
  { label: '100 / 页', value: '100' },
  { label: '全部', value: 'all' }
]
const selectionScopeText = computed(() =>
  showAllEnvs.value ? '批量操作作用于当前已勾选的数据。' : '批量操作仅作用于当前页勾选的数据。'
)
const filteredEnvList = computed(() => {
  if (statusFilter.value === 'enabled') return envList.value.filter(item => item.enabled)
  if (statusFilter.value === 'disabled') return envList.value.filter(item => !item.enabled)
  return envList.value
})
const mobileEmptyDescription = computed(() =>
  statusFilter.value ? '当前筛选条件下暂无环境变量' : '暂无环境变量'
)
const envTableClass = computed(() => ['env-table', 'env-table--' + tableDensity.value])
const envTableHeaderStyle = { background: '#f8fafc', color: '#64748b', fontWeight: 600, fontSize: '13px' }
const tableDensity = ref<'comfortable' | 'compact'>(
  typeof window !== 'undefined' && window.localStorage.getItem(envTableDensityStorageKey) === 'compact'
    ? 'compact'
    : 'comfortable'
)

const showEditDialog = ref(false)
const editDialogMode = ref<'create' | 'edit'>('create')
const currentEditEnv = ref<EnvFormModel | null>(null)
// 提交在途锁：请求返回前弹窗一直开着，不锁按钮连点就会连发 POST 建出多条重复变量
const editSubmitting = ref(false)

const showImportDialog = ref(false)
const importSubmitting = ref(false)

const showExportDialog = ref(false)
const exportFormat = ref('shell')
const exportContent = ref('')
const exportScopeText = computed(() =>
  selectedIds.value.length > 0 ? `已选中的 ${selectedIds.value.length} 项环境变量` : '当前列表中的全部已启用环境变量'
)

const showBatchRenameDialog = ref(false)
const batchRenameSubmitting = ref(false)
const showBatchGroupDialog = ref(false)
const batchGroupSubmitting = ref(false)

const tableRef = ref()
const desktopTableReady = ref(false)
const showDesktopLoadingPlaceholder = computed(
  () => !isMobile.value && (!desktopTableReady.value || (loading.value && envList.value.length === 0))
)
const showDesktopEmptyState = computed(
  () => !isMobile.value && desktopTableReady.value && !loading.value && envList.value.length === 0
)
let sortableInstance: any = null
let sortableLoader: Promise<any> | null = null
let dragPointerY = 0
let dragAutoScrollFrame = 0
let sortableInitFrame = 0
let desktopTableReadyFrame = 0
const groupBadgeStyleCache = new Map<string, CSSProperties>()

function readEnvPageSizeSelection(): EnvPageSizeSelection {
  if (typeof window === 'undefined') {
    return '20'
  }

  const raw = window.localStorage.getItem(envPageSizeStorageKey)
  if (raw === '20' || raw === '50' || raw === '100' || raw === 'all') {
    return raw
  }

  return '20'
}

function persistEnvPageSizeSelection(value: EnvPageSizeSelection) {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(envPageSizeStorageKey, value)
  }
}

function applyEnvPageSizeSelection(value: EnvPageSizeSelection) {
  pageSizeSelection.value = value
  pageSize.value = value === 'all' ? envAllFetchBatchSize : Number(value)
  persistEnvPageSizeSelection(value)
}

function loadSortable() {
  if (!sortableLoader) {
    sortableLoader = import('sortablejs').then((mod) => mod.default)
  }
  return sortableLoader
}

function clearTableSelection() {
  selectedIds.value = []
  tableRef.value?.clearSelection?.()
}

function isSelected(id: number) {
  return selectedIdSet.value.has(id)
}

function toggleSelected(id: number, checked: boolean | string | number) {
  const next = new Set(selectedIds.value)
  if (checked) {
    next.add(id)
  } else {
    next.delete(id)
  }
  selectedIds.value = [...next]
}

function handleDensityChange(value: 'comfortable' | 'compact') {
  tableDensity.value = value
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(envTableDensityStorageKey, value)
  }
}

function buildStringHue(value: string) {
  let hash = 0
  for (const char of value) {
    hash = ((hash << 5) - hash + char.charCodeAt(0)) | 0
  }
  return Math.abs(hash) % 360
}

function getGroupBadgeStyle(group: string): CSSProperties {
  const cacheKey = group || 'default'
  const cached = groupBadgeStyleCache.get(cacheKey)
  if (cached) {
    return cached
  }

  const style = {
    '--group-hue': String(buildStringHue(cacheKey))
  } as CSSProperties
  groupBadgeStyleCache.set(cacheKey, style)
  return style
}

function splitEnvGroups(value: string): string[] {
  return value
    .split(/[,，;；\n\r\t]/)
    .map(group => group.trim())
    .filter((group, index, list) => group !== '' && list.indexOf(group) === index)
}

function normalizeGroupList(value: unknown): string[] {
  if (Array.isArray(value)) {
    return splitEnvGroups(value.filter(item => typeof item === 'string').join(','))
  }
  if (typeof value === 'string') {
    return splitEnvGroups(value)
  }
  return []
}

function normalizeEnvRow(row: any) {
  const rowGroups = normalizeGroupList(row.groups?.length ? row.groups : row.group)
  return {
    ...row,
    group: rowGroups.join(','),
    groups: rowGroups
  }
}

function normalizeEnvRows(rows: any[]) {
  return rows.map(row => normalizeEnvRow(row))
}

function updateDragPointer(evt: any) {
  const pointerEvent = evt?.originalEvent || evt
  if (typeof pointerEvent?.clientY === 'number') {
    dragPointerY = pointerEvent.clientY
    return
  }

  const touch = pointerEvent?.touches?.[0] || pointerEvent?.changedTouches?.[0]
  if (touch && typeof touch.clientY === 'number') {
    dragPointerY = touch.clientY
  }
}

function startDragAutoScroll() {
  stopDragAutoScroll()

  const tick = () => {
    const bodyWrapper = (
      isMobile.value
        ? document.querySelector('.env-mobile-scroll')
        : document.querySelector('.env-table .el-table__body-wrapper')
    ) as HTMLElement | null
    const tableThreshold = 72
    const viewportThreshold = 88
    const tableScrollStep = 22
    const pageScrollStep = 18

    if (bodyWrapper) {
      const rect = bodyWrapper.getBoundingClientRect()
      const canScrollTable = bodyWrapper.scrollHeight > bodyWrapper.clientHeight + 4
      if (canScrollTable) {
        if (dragPointerY < rect.top + tableThreshold && bodyWrapper.scrollTop > 0) {
          bodyWrapper.scrollTop -= tableScrollStep
        } else if (
          dragPointerY > rect.bottom - tableThreshold &&
          bodyWrapper.scrollTop + bodyWrapper.clientHeight < bodyWrapper.scrollHeight
        ) {
          bodyWrapper.scrollTop += tableScrollStep
        }
      }
    }

    if (dragPointerY < viewportThreshold) {
      window.scrollBy({ top: -pageScrollStep, behavior: 'auto' })
    } else if (dragPointerY > window.innerHeight - viewportThreshold) {
      window.scrollBy({ top: pageScrollStep, behavior: 'auto' })
    }

    dragAutoScrollFrame = window.requestAnimationFrame(tick)
  }

  dragAutoScrollFrame = window.requestAnimationFrame(tick)
}

function stopDragAutoScroll() {
  if (dragAutoScrollFrame) {
    window.cancelAnimationFrame(dragAutoScrollFrame)
    dragAutoScrollFrame = 0
  }
}

function clearQueuedSortableInit() {
  if (sortableInitFrame) {
    window.cancelAnimationFrame(sortableInitFrame)
    sortableInitFrame = 0
  }
}

function queueSortableInit() {
  if (typeof window === 'undefined') return
  if (!sortableEnabled.value) {
    if (sortableInstance) {
      sortableInstance.destroy()
      sortableInstance = null
    }
    return
  }
  clearQueuedSortableInit()
  sortableInitFrame = window.requestAnimationFrame(() => {
    sortableInitFrame = 0
    void initSortable()
  })
}

function clearDesktopTableReadyQueue() {
  if (desktopTableReadyFrame) {
    window.cancelAnimationFrame(desktopTableReadyFrame)
    desktopTableReadyFrame = 0
  }
}

function queueDesktopTableReady() {
  if (typeof window === 'undefined' || isMobile.value || desktopTableReady.value) return
  clearDesktopTableReadyQueue()
  desktopTableReadyFrame = window.requestAnimationFrame(() => {
    desktopTableReadyFrame = 0
    desktopTableReady.value = true
  })
}

let loadDataDepth = 0
async function loadData() {
  if (loadDataDepth >= 3) {
    // 防止空页重算递归堆叠，超过 3 层直接中止
    loadDataDepth = 0
    loading.value = false
    return
  }
  loadDataDepth += 1
  loading.value = true
  selectedIds.value = []
  try {
    const params = {
      keyword: keyword.value || undefined,
      // 变量名受 ^[A-Za-z_][A-Za-z0-9_]*$ 约束、不含逗号，所以这里可以放心用逗号拼多值。
      names: nameFilters.value.length > 0 ? nameFilters.value.join(',') : undefined,
      groups: groupFilters.value.length > 0 ? groupFilters.value.join(',') : undefined,
      enabled: statusFilter.value === 'enabled' ? true : statusFilter.value === 'disabled' ? false : undefined
    }

    if (showAllEnvs.value) {
      // 服务端通过 all=1 一次性返回（带 5000 条硬上限保护），避免循环分页造成的等待与滚动卡顿。
      const res = await envApi.list({ ...params, all: 1 })
      envList.value = normalizeEnvRows(res.data || [])
      total.value = res.total || envList.value.length
    } else {
      const res = await envApi.list({
        ...params,
        page: page.value,
        page_size: pageSize.value
      })
      envList.value = normalizeEnvRows(res.data || [])
      total.value = res.total || 0

      if (envList.value.length === 0 && total.value > 0 && page.value > 1) {
        page.value = Math.max(1, Math.ceil(total.value / pageSize.value))
        await loadData()
        return
      }
    }
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || '加载环境变量失败')
  } finally {
    loading.value = false
    loadDataDepth = Math.max(0, loadDataDepth - 1)
  }

  await nextTick()
  queueDesktopTableReady()
  clearTableSelection()
  queueSortableInit()
}

async function loadGroups() {
  try {
    const res = await envApi.groups()
    groups.value = normalizeGroupList(res.data || [])
  } catch {
    // ignore
  }
}

// 变量名下拉的选项源。名字与条数只在「新增 / 改名 / 删除 / 导入」后才会变，
// 所以不跟着翻页、搜索、切筛选一起刷，只在这些真正改动数据的地方补一次。
async function loadNames() {
  try {
    const res = await envApi.names()
    nameOptions.value = Array.isArray(res.data) ? res.data : []
  } catch {
    // ignore
  }
}

onMounted(() => {
  queueDesktopTableReady()
  void loadData()
  void loadGroups()
  void loadNames()
})

watch(isMobile, () => {
  nextTick(() => {
    queueDesktopTableReady()
    queueSortableInit()
  })
})

onBeforeUnmount(() => {
  stopDragAutoScroll()
  clearQueuedSortableInit()
  clearDesktopTableReadyQueue()
  if (sortableInstance) {
    sortableInstance.destroy()
    sortableInstance = null
  }
})

// 拖拽时把被拖行每个单元格宽度锁成固定 px，避免 forceFallback 克隆行脱离表格后列宽塌陷
function lockRowCellWidths(row: HTMLElement | null) {
  if (!row) return
  row.querySelectorAll<HTMLElement>('td').forEach((cell) => {
    const w = cell.offsetWidth
    cell.style.width = `${w}px`
    cell.style.minWidth = `${w}px`
    cell.style.maxWidth = `${w}px`
    cell.style.boxSizing = 'border-box'
  })
}
function unlockRowCellWidths(row: HTMLElement | null) {
  if (!row) return
  row.querySelectorAll<HTMLElement>('td').forEach((cell) => {
    cell.style.width = ''
    cell.style.minWidth = ''
    cell.style.maxWidth = ''
    cell.style.boxSizing = ''
  })
}

async function initSortable() {
  if (sortableInstance) {
    sortableInstance.destroy()
    sortableInstance = null
  }
  if (loading.value || !sortableEnabled.value) return
  const el = document.querySelector(
    isMobile.value
      ? '.env-mobile-list'
      : '.env-table .el-table__body-wrapper tbody'
  )
  if (!el) return
  try {
    const Sortable = await loadSortable()
    sortableInstance = Sortable.create(el as HTMLElement, {
      animation: 220,
      easing: 'cubic-bezier(0.22, 1, 0.36, 1)',
      ghostClass: 'sortable-ghost',
      chosenClass: 'sortable-chosen',
      dragClass: 'sortable-drag',
      forceFallback: true,
      fallbackOnBody: true,
      scroll: true,
      bubbleScroll: true,
      scrollSensitivity: 100,
      scrollSpeed: 18,
      ...(isMobile.value
        ? { handle: '.env-mobile-drag-handle', delay: 350, delayOnTouchOnly: true, touchStartThreshold: 8 }
        : { handle: '.env-drag-handle', delay: 0, touchStartThreshold: 4 }),
      onChoose: (evt: any) => {
        if (!isMobile.value) lockRowCellWidths(evt.item)
      },
      onUnchoose: (evt: any) => {
        if (!isMobile.value) unlockRowCellWidths(evt.item)
      },
      onStart: (evt: any) => {
        if (!isMobile.value) lockRowCellWidths(evt.item)
        updateDragPointer(evt)
        startDragAutoScroll()
      },
      onMove: (evt: any) => {
        updateDragPointer(evt)
      },
      onEnd: async (evt: any) => {
        if (!isMobile.value) unlockRowCellWidths(evt.item)
        stopDragAutoScroll()
        updateDragPointer(evt)

        const { oldIndex, newIndex } = evt
        if (oldIndex === newIndex) return

        const sourceItem = envList.value[oldIndex]
        if (!sourceItem) return

        const movedItem = envList.value.splice(oldIndex, 1)[0]
        envList.value.splice(newIndex, 0, movedItem)
        const nextItem = envList.value[newIndex + 1]
        const sourceSortOrder = Number(sourceItem.sort_order || 0)
        const targetSortOrder = nextItem ? Number(nextItem.sort_order || 0) : sourceSortOrder

        if (targetSortOrder !== sourceSortOrder) {
          ElMessage.warning('置顶区和普通区请分别排序，跨区移动请使用置顶按钮')
          void loadData()
          return
        }

        try {
          await envApi.sort(sourceItem.id, nextItem?.id)
        } catch (err: any) {
          ElMessage.error(err?.response?.data?.error || err?.message || '排序失败')
          void loadData()
        }
      }
    })
  } catch {
    ElMessage.error('拖拽排序组件加载失败')
  }
}

function handleSearch() {
  page.value = 1
  void loadData()
}

function handleGroupSelect() {
  page.value = 1
  void loadData()
}

function handleNameSelect() {
  page.value = 1
  void loadData()
}

// 名称单元格点一下 = 「只看这个变量名的全部条目」。
// 这是同名多条场景里最高频的诉求，做成【替换】而不是【追加】：用户点的是「只看这个」，
// 追加的话原来选中的其它名字还留在结果里，与点击时的心理预期相反。
// 已经就是这一个名字时直接返回，别白刷一次列表。
function handleFilterByName(name: string) {
  if (!name) return
  if (nameFilters.value.length === 1 && nameFilters.value[0] === name) return
  nameFilters.value = [name]
  page.value = 1
  void loadData()
}

function handlePageChange(newPage: number) {
  page.value = newPage
  void loadData()
}

function handlePageSizeChange(newSize: EnvPageSizeSelection) {
  applyEnvPageSizeSelection(newSize)
  page.value = 1
  void loadData()
}

function handlePageSizeSelect(value: string) {
  handlePageSizeChange(value as EnvPageSizeSelection)
}

function openCreate() {
  editDialogMode.value = 'create'
  currentEditEnv.value = { id: 0, name: '', value: '', remarks: '', group: '', groups: [] }
  showEditDialog.value = true
}

function openDuplicate(row: any) {
  editDialogMode.value = 'create'
  currentEditEnv.value = {
    id: 0,
    name: row.name || '',
    value: '',
    remarks: row.remarks || '',
    group: row.group || '',
    groups: normalizeGroupList(row.groups?.length ? row.groups : row.group)
  }
  showEditDialog.value = true
}

function openEdit(row: any) {
  editDialogMode.value = 'edit'
  currentEditEnv.value = {
    id: row.id,
    name: row.name || '',
    value: row.value || '',
    remarks: row.remarks || '',
    group: row.group || '',
    groups: normalizeGroupList(row.groups?.length ? row.groups : row.group)
  }
  showEditDialog.value = true
}

async function handleSave(data: EnvFormModel | EnvFormModel[]) {
  // 校验都在子弹窗里做完了，走到这里必然要发请求，直接置位在途锁
  editSubmitting.value = true
  try {
    if (Array.isArray(data)) {
      await envApi.create(data as any)
      ElMessage.success(`批量创建 ${data.length} 个变量成功`)
    } else if (editDialogMode.value === 'create') {
      await envApi.create(data)
      ElMessage.success('创建成功')
    } else {
      await envApi.update(data.id, {
        name: data.name,
        value: data.value,
        remarks: data.remarks,
        group: data.group,
        groups: data.groups
      })
      ElMessage.success('更新成功')
    }
    showEditDialog.value = false
    void loadData()
    void loadGroups()
    // 新建会多出一条同名记录、编辑可能改掉变量名，两种都会让下拉里的名字或计数过期
    void loadNames()
  } catch {
    ElMessage.error(editDialogMode.value === 'create' ? '创建失败' : '更新失败')
  } finally {
    editSubmitting.value = false
  }
}

function isTopPinned(row: any) {
  return Number(row.sort_order || 0) > 0
}

// 置顶与禁用是两个正交状态，返回空格分隔的多类名。
// Element Plus 把 row-class-name 的返回值整串作为**一项** push 进 Vue 的 class 数组，
// Vue 再把数组各项拼成最终的 class 字符串（只做拼接、不拆空格，也不需要拆：
// 空格分隔的多类名写进 class 属性本来就天然生效），
// 所以「又置顶又禁用」的行两个类都会挂上（谁的底色赢由样式表里的书写顺序决定，见 .env-row-disabled）。
function getRowClassName({ row }: { row: any }) {
  const classes: string[] = []
  if (isTopPinned(row)) classes.push('env-row-pinned')
  if (!row.enabled) classes.push('env-row-disabled')
  return classes.join(' ')
}

async function handleToggleTop(row: any) {
  try {
    if (isTopPinned(row)) {
      await envApi.cancelTop(row.id)
      ElMessage.success('已取消置顶')
    } else {
      await envApi.moveToTop(row.id)
      ElMessage.success('已置顶')
    }
    void loadData()
  } catch {
    ElMessage.error('操作失败')
  }
}

async function handleDelete(id: number) {
  try {
    await ElMessageBox.confirm('确定要删除该环境变量吗？', '确认删除', { type: 'warning' })
    await envApi.delete(id)
    ElMessage.success('删除成功')
    void loadData()
    void loadNames()
  } catch {
    // cancelled
  }
}

async function handleToggle(row: any) {
  try {
    const enabling = !row.enabled
    await ElMessageBox.confirm(
      enabling
        ? `确认启用环境变量 ${row.name} 吗？`
        : `确认禁用环境变量 ${row.name} 吗？禁用后脚本将无法读取该变量。`,
      enabling ? '启用确认' : '禁用确认',
      { type: enabling ? 'info' : 'warning' }
    )
    if (row.enabled) {
      await envApi.disable(row.id)
    } else {
      await envApi.enable(row.id)
    }
    ElMessage.success(row.enabled ? '已禁用' : '已启用')
    void loadData()
  } catch (err: any) {
    if (err === 'cancel' || err?.toString?.() === 'cancel') return
    ElMessage.error('操作失败')
  }
}

/**
 * 操作列 Split Button 的菜单项（按行生成）。
 *
 * 主体是「编辑」——最常用，且点错了只是打开一个弹窗，代价最小。
 * 「删除」不可撤销，必须留在菜单里并加 divided + danger，绝不能占主体位置。
 * 「置顶 / 取消置顶」是同一个 handleToggleTop 的两面，用 visible 按 row 状态互斥，
 * 避免出现「已置顶的行菜单里还挂着置顶」。
 *
 * 🔴 这里【不要】再补「启用 / 禁用」：它已经外置成操作列里的一级按钮（谁上了一级谁就不在菜单里），
 * 补进来就会变成「点了外面的禁用，展开 ▾ 里还挂着一个禁用」。
 */
function buildEnvActionItems(row: any): SplitButtonItem[] {
  const pinned = isTopPinned(row)
  return [
    { key: 'duplicate', label: '复制同名变量' },
    { key: 'top', label: '置顶', visible: !pinned },
    { key: 'cancel-top', label: '取消置顶', visible: pinned },
    { key: 'delete', label: '删除', danger: true, divided: true }
  ]
}

function onEnvAction(key: string, row: any) {
  if (key === 'duplicate') openDuplicate(row)
  else if (key === 'top' || key === 'cancel-top') void handleToggleTop(row)
  else if (key === 'delete') void handleDelete(row.id)
}

async function handleBatchDelete() {
  if (selectedIds.value.length === 0) return
  try {
    await ElMessageBox.confirm(`确定要删除选中的 ${selectedIds.value.length} 个环境变量吗？`, '批量删除', { type: 'warning' })
    await envApi.batchDelete(selectedIds.value)
    ElMessage.success('批量删除成功')
    clearTableSelection()
    void loadData()
    void loadNames()
  } catch {
    // cancelled
  }
}

async function handleBatchGroup() {
  if (selectedIds.value.length === 0) return
  showBatchGroupDialog.value = true
}

function handleBatchRename() {
  if (selectedIds.value.length === 0) return
  showBatchRenameDialog.value = true
}

async function confirmBatchRename(payload: { name: string }) {
  // 在途锁：弹窗要等请求成功才关，期间连点会重复提交
  batchRenameSubmitting.value = true
  try {
    const res = await envApi.batchRename(selectedIds.value, payload.name)
    ElMessage.success(res.message || '批量改名成功')
    showBatchRenameDialog.value = false
    clearTableSelection()
    void loadData()
    void loadNames()
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || err?.message || '批量改名失败')
  } finally {
    batchRenameSubmitting.value = false
  }
}

async function confirmBatchGroup(groups: string[]) {
  // 在途锁：同上，弹窗在请求期间仍可点
  batchGroupSubmitting.value = true
  try {
    await envApi.batchSetGroup(selectedIds.value, normalizeGroupList(groups))
    ElMessage.success('批量分组成功')
    showBatchGroupDialog.value = false
    clearTableSelection()
    void loadData()
    void loadGroups()
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || err?.message || '批量分组失败')
  } finally {
    batchGroupSubmitting.value = false
  }
}

async function handleBatchEnable() {
  if (selectedIds.value.length === 0) return
  try {
    await ElMessageBox.confirm(`确认启用选中的 ${selectedIds.value.length} 个环境变量吗？`, '批量启用', { type: 'info' })
    await envApi.batchEnable(selectedIds.value)
    ElMessage.success('批量启用成功')
    void loadData()
  } catch (err: any) {
    if (err === 'cancel' || err?.toString?.() === 'cancel') return
    ElMessage.error('批量启用失败')
  }
}

async function handleBatchDisable() {
  if (selectedIds.value.length === 0) return
  try {
    await ElMessageBox.confirm(`确认禁用选中的 ${selectedIds.value.length} 个环境变量吗？`, '批量禁用', { type: 'warning' })
    await envApi.batchDisable(selectedIds.value)
    ElMessage.success('批量禁用成功')
    void loadData()
  } catch (err: any) {
    if (err === 'cancel' || err?.toString?.() === 'cancel') return
    ElMessage.error('批量禁用失败')
  }
}

function handleSelectionChange(rows: any[]) {
  selectedIds.value = rows.map(r => r.id)
}

async function handleImport(payload: { envs: any[]; mode: string }) {
  // 在途锁：JSON 体积大时导入耗时长，不锁按钮连点会把同一份数据导多遍
  importSubmitting.value = true
  try {
    const res = await envApi.import(payload.envs, payload.mode)
    ElMessage.success(res.message)
    showImportDialog.value = false
    void loadData()
    void loadGroups()
    void loadNames()
  } catch {
    ElMessage.error('导入失败')
  } finally {
    importSubmitting.value = false
  }
}

async function handleExportAll() {
  try {
    const exportIds = selectedIds.value.length > 0 ? [...selectedIds.value] : undefined
    const res = await envApi.exportAll(exportIds)
    const json = JSON.stringify(res.data, null, 2)
    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = exportIds?.length ? `env_vars_selected_${exportIds.length}.json` : 'env_vars.json'
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    ElMessage.error('导出失败')
  }
}

async function handleExportFiles() {
  showExportDialog.value = true
  try {
    const exportIds = selectedIds.value.length > 0 ? [...selectedIds.value] : undefined
    const enabledOnly = exportIds == null
    const res = await envApi.exportFiles(exportFormat.value, enabledOnly, exportIds)
    exportContent.value = res.data[exportFormat.value] || ''
  } catch {
    ElMessage.error('导出失败')
  }
}

/**
 * 工具条「导出」Split Button 的菜单项。
 *
 * 主体走 JSON（handleExportAll）：它是与「导入」配对的往返格式，原下拉里也排在第一位，
 * 且不依赖 exportFormat 这个 ref，点错的代价只是多下载一个文件。
 * Shell / JS / Python 三种只是打开预览弹窗，收进菜单；「导入」是反方向的操作，
 * 沿用原来的 divided 分隔线与其余项隔开。
 */
const exportActionItems: SplitButtonItem[] = [
  { key: 'shell', label: '导出 Shell' },
  { key: 'js', label: '导出 JS' },
  { key: 'python', label: '导出 Python' },
  { key: 'import', label: '导入', divided: true }
]

function onExportAction(key: string) {
  if (key === 'import') {
    showImportDialog.value = true
    return
  }
  exportFormat.value = key
  void handleExportFiles()
}

async function refreshExport() {
  try {
    const exportIds = selectedIds.value.length > 0 ? [...selectedIds.value] : undefined
    const enabledOnly = exportIds == null
    const res = await envApi.exportFiles(exportFormat.value, enabledOnly, exportIds)
    exportContent.value = res.data[exportFormat.value] || ''
  } catch {
    // ignore
  }
}

async function copyExport() {
  try {
    await copyText(exportContent.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请检查浏览器权限或站点访问方式')
  }
}

async function copyEnvValue(value: string) {
  try {
    await copyText(value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请检查浏览器权限或站点访问方式')
  }
}

function formatDateTime(t: string | null) {
  if (!t) return '-'
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}  ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function handleStatusFilter(value: '' | 'enabled' | 'disabled') {
  if (statusFilter.value === value) {
    return
  }
  statusFilter.value = value
  page.value = 1
  void loadData()
}
</script>

<template>
  <div class="envs-page dd-fixed-page dd-page-hide-heading">
    <div class="toolbar">
      <!-- 左槽是恒在的容器（flex:1 + 固定高度下限），内部的「筛选区」与「批量区」两支【都常驻 DOM】，
           叠放在同一个 1×1 网格格子里，只用 visibility 切换显示：勾一下就让批量按钮就地显形，
           筛选控件同一帧立即隐藏（transition 只列了 opacity，visibility 是即时生效的，
           所以只有【进场】那一支有淡入，退场是硬切）。这是刻意的：两支叠在同一个格子里，
           真做交叉淡出会有一段两排控件互相透视的重影，别为了「对称」把 visibility 加进 transition。
           左槽自身的 flex 尺寸全程不变，右区不会被 space-between 甩位；
           更关键的是左槽高度恒等于 max(筛选区高度, 批量区高度)，与当前显示哪一支无关 ⇒ 勾选/取消永不改变工具栏高度。
           本页两支的换行阈值不一样，所以窄窗口下【两个方向都可能出现】——
           筛选区原来是「3 个 tab + 260px 搜索框 + 220px 分组筛选」≈ 724px 定宽；
           v3.2.5 加入「变量名筛选」后按定宽算会涨到 ~966px，1280 宽 + 侧栏展开时必然换行，
           所以三个输入控件改成【可伸缩】：flex-basis 取收缩下限（160/150/150）、max-width 取理想宽（260/220/220），
           换行阈值 = 230(tabs) + 3×12(gap) + 160 + 150 + 150 ≈ 726px，与加控件之前【几乎不变】；
           宽屏有余量时再长回 260/220/220，看起来和以前一样。详见样式里 &__search / &__group-filter / &__name-filter 的注释。
           批量区则是 6 个按钮 + 计数且带 flex-wrap——
           以前只留一支在 DOM 里的话，勾选那一刻左槽忽高忽矮，dd-fixed-page 下 .table-card 是 flex:1 1 0，
           工具栏差多少表格就反向补多少 ⇒ 列表跳一下。换行点又由内容宽决定，而内容宽随侧栏展开/收起漂 156px
           （220px vs 64px），媒体查询锁不住，只能让两支同时参与撑高。
           visibility: hidden 自带「不可点、不进 Tab 序、不进无障碍树」，不需要再加 inert / aria-hidden。
           切到批量区的条件只按「当前列表里真实存在的行」算（selectedCountInCurrentPage 拿
           selectedIdSet 过 envList），不是 selectedIds.length 这个可能含已失效 id 的原始数组。 -->
      <div class="toolbar__left">
        <div class="toolbar__filters" :class="{ 'is-swapped-out': selectedCountInCurrentPage > 0 }">
          <div class="status-tabs">
            <button :class="['status-tab', { active: statusFilter === '' }]" @click="handleStatusFilter('')">全部变量</button>
            <button :class="['status-tab', { active: statusFilter === 'enabled' }]" @click="handleStatusFilter('enabled')">已启用</button>
            <button :class="['status-tab', { active: statusFilter === 'disabled' }]" @click="handleStatusFilter('disabled')">已禁用</button>
          </div>
          <el-input
            v-model="keyword"
            placeholder="搜索变量名、值、备注或分组"
            clearable
            class="toolbar__search"
            @keyup.enter="handleSearch"
            @clear="handleSearch"
          >
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-select
            v-model="groupFilters"
            placeholder="分组筛选"
            multiple
            collapse-tags
            collapse-tags-tooltip
            clearable
            class="toolbar__group-filter"
            @change="handleGroupSelect"
          >
            <el-option v-for="g in groups" :key="g" :label="g" :value="g" />
          </el-select>
          <!-- 🔴 filterable 是必须的，不要照抄上面分组筛选那一个：分组通常只有个位数，
               变量名却可能上百个（同名多条是刻意功能，每个账号一条），不能打字过滤就只能一路滚。
               标签渲染成「JD_COOKIE (3)」，括号里是全库同名条数，与后端 by-name 冲突文案里的 N 同源。 -->
          <el-select
            v-model="nameFilters"
            placeholder="变量名筛选"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            clearable
            class="toolbar__name-filter"
            @change="handleNameSelect"
          >
            <el-option
              v-for="option in nameOptions"
              :key="option.name"
              :label="`${option.name} (${option.count})`"
              :value="option.name"
            />
          </el-select>
        </div>
        <div class="batch-actions" :class="{ 'is-swapped-out': !(selectedCountInCurrentPage > 0) }">
          <!-- selectionScopeText 已经按分页模式区分过「全部 / 仅当前页」的措辞，挂成 title：
               动手前就该知道这几个按钮到底作用在哪些数据上。 -->
          <span class="batch-actions__count" :title="selectionScopeText">
            已选 {{ selectedCountInCurrentPage }} 项
          </span>
          <el-button @click="handleBatchEnable">批量启用</el-button>
          <!-- 禁用会让变量在脚本里直接取不到值，属于破坏性变更，用 danger plain 与中性按钮区分；
               又刻意排在「批量删除」前面隔着改名/分组两个中性按钮，避免两个红按钮挨在一起被误点。 -->
          <el-button type="danger" plain @click="handleBatchDisable">批量禁用</el-button>
          <el-button @click="handleBatchRename">批量改名</el-button>
          <el-button @click="handleBatchGroup">批量分组</el-button>
          <!-- 删除是不可逆的，用实心 danger 压过 plain 的「批量禁用」，也与 tasks / logs 两页的
               「批量删除」保持同一形态：三页里最醒目的永远是删除。 -->
          <el-button type="danger" @click="handleBatchDelete">批量删除</el-button>
          <!-- 批量区显形期间搜索框/分组筛选虽然还在 DOM 里，但已被 visibility 挡住不能点，
               这里是退出多选态的唯一出口，不能省 -->
          <el-button @click="clearTableSelection">取消选择</el-button>
        </div>
      </div>
      <div class="toolbar__right">
        <!-- 原来是一个「…」更多下拉：主体只能展开菜单，最常用的导出永远要点两次，
             按钮上也看不出这里能干什么。改成真正的 Split Button：
             主体「导出」直接下载 JSON（默认/往返格式），其余格式与导入留在菜单里。 -->
        <DdSplitButton
          label="导出"
          type="default"
          size="default"
          :items="exportActionItems"
          @click="handleExportAll"
          @command="onExportAction"
        />
        <el-button type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon> 新建变量
        </el-button>
      </div>
    </div>

    <div v-if="isMobile" class="env-mobile-scroll">
      <div class="dd-mobile-list env-mobile-list">
        <div
          v-for="row in filteredEnvList"
          :key="row.id"
          class="dd-mobile-card env-card"
          :class="{
            'env-card--pinned': isTopPinned(row),
            'env-card--disabled': !row.enabled
          }"
        >
          <div class="dd-mobile-card__header">
            <div class="dd-mobile-card__title-wrap">
              <div class="env-card__title-row">
                <div class="dd-mobile-card__selection">
                  <button
                    v-if="sortableEnabled"
                    class="env-mobile-drag-handle"
                    type="button"
                    aria-label="长按拖动排序"
                    @click.stop
                  >
                    <el-icon :size="16"><Rank /></el-icon>
                  </button>
                  <el-checkbox :model-value="isSelected(row.id)" @change="toggleSelected(row.id, $event)" />
                  <div class="env-name-wrap">
                    <!-- 状态圆点：卡片里原来的「状态」字段（switch + 文字）已删掉，
                         启用/禁用改由名称前这枚 8×8 圆点 + 操作区的启用/禁用按钮承载，
                         与桌面表格完全同一套东西。title / aria-label 的必要性见下方 .env-status-dot 的注释。 -->
                    <span
                      class="env-status-dot"
                      :class="{ 'is-enabled': row.enabled }"
                      role="img"
                      :title="row.enabled ? '已启用' : '已禁用'"
                      :aria-label="row.enabled ? '已启用' : '已禁用'"
                    />
                    <!-- 与桌面表格同一套结构（同样的类、同样的 handler）：名称文本是 <span>、
                         「只看这个变量名」由后面那颗图标按钮承担，理由见桌面表格那一处的注释。
                         触屏这边收益更直接：长按 <span> 才会弹出系统的文本选择/复制浮层，
                         长按 <button> 不会（而卡片的长按拖拽只绑在 .env-mobile-drag-handle 上，不抢这个手势）。
                         这里不套 el-tooltip：触屏没有 hover，同卡片里「值」的复制按钮也是裸按钮，保持一致。 -->
                    <span class="env-name">{{ row.name }}</span>
                    <el-button
                      class="env-name-filter-btn"
                      size="small"
                      link
                      :aria-label="`只看变量名 ${row.name} 的全部条目`"
                      @click.stop="handleFilterByName(row.name)"
                    >
                      <el-icon :size="14"><Search /></el-icon>
                    </el-button>
                    <span v-if="isTopPinned(row)" class="pinned-chip">
                      <el-icon><Top /></el-icon>
                      置顶
                    </span>
                  </div>
                </div>
                <div class="env-card__tools">
                  <div v-if="row.groups.length > 0" class="group-pill-list">
                    <span
                      v-for="group in row.groups"
                      :key="group"
                      class="group-pill"
                      :style="getGroupBadgeStyle(group)"
                    >
                      <span class="group-dot" />
                      <span class="group-pill__text">{{ group }}</span>
                    </span>
                  </div>
                  <span v-else class="env-empty-text">未分组</span>
                </div>
              </div>
            </div>
          </div>

          <div class="dd-mobile-card__body">
            <div class="dd-mobile-card__grid">
              <div class="dd-mobile-card__field dd-mobile-card__field--full">
                <span class="dd-mobile-card__label">值</span>
                <div class="dd-mobile-card__value env-value-cell">
                  <span class="env-value-text">{{ row.value || '-' }}</span>
                  <el-button v-if="row.value" size="small" link @click.stop="copyEnvValue(row.value)">
                    <el-icon :size="14"><CopyDocument /></el-icon>
                  </el-button>
                </div>
              </div>
              <div class="dd-mobile-card__field dd-mobile-card__field--full">
                <span class="dd-mobile-card__label">备注</span>
                <span class="dd-mobile-card__value env-remarks-text">{{ row.remarks || '-' }}</span>
              </div>
              <!-- 原来这里还有一格「状态」（switch + 启用/禁用文字），已删除：状态改由标题行的圆点表达、
                   切换改由下面操作区的「启用 / 禁用」按钮执行，与桌面端同源。
                   少一格不影响布局：.dd-mobile-card__grid 基态是 2 列，但 ≤768px 那条媒体查询把它压成
                   单列，而移动卡片本身只在 isMobile（width ≤ 768）时才渲染 ⇒ 实际永远是单列纵向堆叠，
                   剩下的「值 / 备注 / 更新时间」照旧一行一格。 -->
              <div class="dd-mobile-card__field">
                <span class="dd-mobile-card__label">更新时间</span>
                <span class="dd-mobile-card__value time-text">{{ formatDateTime(row.updated_at) }}</span>
              </div>
            </div>

            <div class="dd-mobile-card__actions env-card__actions">
              <el-button size="small" type="primary" @click="openEdit(row)">编辑</el-button>
              <!-- 与桌面操作列同一个按钮、同一套 type/plain 组合、同一个 handleToggle：
                   启用态显示「禁用」= danger plain，禁用态显示「启用」= 不写 type（EP default 白底）。
                   移动端原来用 el-switch，桌面改成按钮后两端就成了两套东西，必须跟着换。 -->
              <el-button
                size="small"
                :type="row.enabled ? 'danger' : 'default'"
                :plain="row.enabled"
                @click="handleToggle(row)"
              >
                {{ row.enabled ? '禁用' : '启用' }}
              </el-button>
              <el-button size="small" @click="openDuplicate(row)">复制</el-button>
              <el-button
                size="small"
                :type="isTopPinned(row) ? 'info' : 'warning'"
                @click="handleToggleTop(row)"
              >
                {{ isTopPinned(row) ? '取消置顶' : '置顶' }}
              </el-button>
              <!-- 删除必须是【实心】danger：同一张卡片里「禁用」已经占了 danger + plain，
                   删除若也写 plain，两个红描边白底按钮外观逐字相同，用户分不出哪个不可逆。
                   与本页工具栏「批量禁用（plain）/ 批量删除（实心）」以及 design-system §4.2
                   是同一条层级规则：一组按钮里最醒目的永远留给不可逆的那个。 -->
              <el-button size="small" type="danger" @click="handleDelete(row.id)">删除</el-button>
            </div>
          </div>
        </div>

        <el-empty v-if="!loading && filteredEnvList.length === 0" :description="mobileEmptyDescription" />
      </div>
    </div>

    <div v-else-if="showDesktopLoadingPlaceholder" class="env-desktop-state env-desktop-state--loading" aria-hidden="true">
      <div class="env-skeleton env-skeleton--title" />
      <div class="env-skeleton env-skeleton--toolbar" />
      <div v-for="n in 6" :key="`env-skeleton-${n}`" class="env-skeleton env-skeleton--row" />
    </div>

    <div v-else-if="showDesktopEmptyState" class="env-desktop-state">
      <el-empty description="暂无环境变量">
        <template #description>
          <div class="env-empty-copy">
            <strong>暂无环境变量</strong>
            <span>可以直接新建变量，或导入已有的 JSON 配置。</span>
          </div>
        </template>
        <div class="env-empty-actions">
          <el-button type="primary" @click="openCreate">新建环境变量</el-button>
          <el-button @click="showImportDialog = true">导入 JSON</el-button>
        </div>
      </el-empty>
    </div>

    <div v-else class="table-card">
      <el-table
        ref="tableRef"
        :data="filteredEnvList"
        v-loading="loading"
        @selection-change="handleSelectionChange"
        :row-class-name="getRowClassName"
        :class="envTableClass"
        :header-cell-style="envTableHeaderStyle"
        row-key="id"
        style="width: 100%"
      >
        <el-table-column type="selection" width="44" />
        <!-- min-width 188 → 204 → 230，两笔账都记在这里。
             .env-name-wrap 是 flex + gap:8px，多插一个子元素就多出一份 gap，
             所以每一笔净增都是「新元素自身宽度」+「新增的那一份 gap 8px」，不是只有元素自身：
               188 → 204：名称【前】多了一枚 8px 状态圆点 → 8 + 8 = 16px；
               204 → 230：名称【后】多了一颗「只看这个变量名」图标按钮 →
                          按钮自身 18px（图标 14 + .env-name-filter-btn 的 padding 2×2）+ 8 = 26px。
             不补回来的话名称的可见宽度会净减同样多，长变量名的省略号会提前出现。 -->
        <el-table-column prop="name" label="名称" min-width="230">
          <template #default="{ row }">
            <div class="env-name-wrap">
              <!-- 状态圆点：独立的「状态」列已删除，启用/禁用状态改由这枚圆点表达
                   （筛选仍由工具栏的 .status-tabs 承担，切换由操作列的按钮执行）。
                   title / aria-label 的必要性见下方 .env-status-dot 的注释。 -->
              <span
                class="env-status-dot"
                :class="{ 'is-enabled': row.enabled }"
                role="img"
                :title="row.enabled ? '已启用' : '已禁用'"
                :aria-label="row.enabled ? '已启用' : '已禁用'"
              />
              <!-- 🔴 名称文本必须是 <span>，别再改回 <button>：这一列没有常驻复制按钮，
                   鼠标划选就是复制变量名的唯一路径（想不划选就只剩「打开编辑弹窗从 input 里复制」），
                   而 <button> 这类表单控件浏览器不从它的 mousedown 起始文本选区，
                   双击（选词的自然手势）还会连触发两次点击 —— 第一次设筛选、第二次被
                   handleFilterByName 的等值 guard 挡掉，用户得到的是「列表被筛了」而不是「选中了一个词」。
                   :title 挂全名：名称超长时这一格只剩省略号，不挂就没有任何办法看到完整变量名
                   （「值」「备注」两列本来也是这么挂的）。 -->
              <span class="env-name" :title="row.name">{{ row.name }}</span>
              <!-- 「点一下 = 只看这个变量名的全部条目」这个入口没有取消，只是从名称文本上外移到这颗独立图标按钮：
                   入口还在、还是点一下，但它落在名称文本【之外】，划选名称不会再顺带把列表筛掉。
                   用 el-button（渲染出来的就是 <button>）而不是给 span 挂 @click —— 可点的东西必须能 Tab 聚焦、
                   能回车触发、能被读屏念成按钮；图标按钮没有可见文字，所以另挂 aria-label 作为无障碍名称
                   （el-tooltip 只负责鼠标悬停时的可见提示，构不成元素的无障碍名称）。
                   尺寸 / 配色 / 过渡与「值」列的 .env-copy-btn 逐条对齐，同一张表里的行内图标按钮保持一款。
                   @click.stop 是防御性的：本行没有行级点击，但拖拽克隆/未来的行点击都可能冒泡上来。 -->
              <el-tooltip content="只看这个变量名" placement="top">
                <el-button
                  class="env-name-filter-btn"
                  size="small"
                  link
                  :aria-label="`只看变量名 ${row.name} 的全部条目`"
                  @click.stop="handleFilterByName(row.name)"
                >
                  <el-icon :size="14"><Search /></el-icon>
                </el-button>
              </el-tooltip>
              <span v-if="isTopPinned(row)" class="pinned-chip">
                <el-icon><Top /></el-icon>
                置顶
              </span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="value" label="值" min-width="280">
          <template #default="{ row }">
            <div class="env-value-cell">
              <span class="env-value-text" :title="row.value || ''">{{ row.value || '-' }}</span>
              <el-tooltip v-if="row.value" content="复制" placement="top">
                <el-button class="env-copy-btn" size="small" link @click.stop="copyEnvValue(row.value)">
                  <el-icon :size="14"><CopyDocument /></el-icon>
                </el-button>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="remarks" label="备注" min-width="180">
          <template #default="{ row }">
            <span class="env-remarks-text" :title="row.remarks || ''">{{ row.remarks || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="group" label="分组" min-width="180" align="center">
          <template #default="{ row }">
            <div v-if="row.groups.length > 0" class="group-pill-list group-pill-list--table">
              <span
                v-for="group in row.groups"
                :key="group"
                class="group-pill"
                :style="getGroupBadgeStyle(group)"
              >
                <span class="group-dot" />
                <span class="group-pill__text">{{ group }}</span>
              </span>
            </div>
            <span v-else class="env-empty-text">未分组</span>
          </template>
        </el-table-column>
        <el-table-column width="40" align="center" class-name="env-drag-col">
          <template #default>
            <el-icon class="env-drag-handle"><Rank /></el-icon>
          </template>
        </el-table-column>
        <!-- 原来这里是一整列「状态」（92px：switch + 启用/禁用文字），已删除：
             状态由名称列前的圆点表达、切换由操作列的「启用 / 禁用」按钮执行，一处状态一处开关。
             筛选口径不变，仍由工具栏的 .status-tabs（全部 / 已启用 / 已禁用）承担。 -->
        <el-table-column label="更新时间" width="168" align="center">
          <template #default="{ row }">
            <span class="time-text">{{ formatDateTime(row.updated_at) }}</span>
          </template>
        </el-table-column>
        <!-- 原来是 4 个纯图标按钮，每个各套一层 el-tooltip：图标本身不自解释，
             而且「删除」和「编辑」同尺寸并排、误点代价完全不同。
             改成 Split Button：主体是最常用且可逆的「编辑」，其余收进菜单，
             删除标红并用分隔线隔开，置顶/取消置顶靠 visible 互斥。

             列宽 172（v3.2.0 从 110 重算，与定时任务页取同一个值）：
             EP 的 .el-table .cell 是 padding:0 12px + overflow:hidden，可用内容宽 = 172 - 24 = 148px。
             这一格现在是两个元素：左边 Split Button（编辑 ▾），右边外置的「禁用 / 启用」普通按钮。
             实测口径（size="small"，浏览器量出来的，不是估的）：
               主体 = 中文字数 × 12（字宽）+ 22（EP small 的 padding 5px 11px）+ 2（边框）
               caret = 32，且【不受 size 影响】：EP 的 el-dropdown 根节点只挂 `el-dropdown` + `is-disabled`，
                 不带 size 修饰类，`.el-dropdown--small .el-dropdown__caret-button{width:24px}` 根本匹配不上。
                 ⚠️ 本文件旧注释按那条规则算成 24px，是错的（会系统性偏小约 14px），此处一并订正 ——
                 别再照 24px 把列宽收窄回去。
               el-button-group 组内相邻按钮还有 -1px 的负边距
             「编辑」= 2×12+22+2 = 48 → 48+32-1 = 79px；「禁用」/「启用」同为 2 字 = 48px；
             中间 gap 4px（.action-btns 已把 EP 的 `.el-button + .el-button` 外边距清零，间距只由 gap 决定，
             不会两份叠加）。合计 79 + 4 + 48 = 131px，148 - 131 = 17px 余量。
             按钮组一旦超出可用宽，.cell 就会变成可滚动容器，点右边的按钮时整行会被滚偏且不复位。

             fixed="right"：与定时任务页一致，窄窗口横向滚动时操作列常驻可见。 -->
        <el-table-column label="操作" width="172" fixed="right" align="center">
          <template #default="{ row }">
            <div class="action-btns">
              <DdSplitButton
                label="编辑"
                type="primary"
                size="small"
                :items="buildEnvActionItems(row)"
                @click="openEdit(row)"
                @command="(key: string) => onEnvAction(key, row)"
              />
              <!-- 「启用 / 禁用」外置成一级按钮：状态列删掉后这是唯一的切换入口，埋进 ▾ 菜单要点两下。
                   两个状态互斥（谁上了一级谁就不在菜单里），buildEnvActionItems 里因此没有同名项。
                   type/plain 组合与工具栏的「批量启用 / 批量禁用」逐字一致：
                   禁用 = danger + plain（白底红字红描边），启用 = default（EP 白底）。
                   直接复用 handleToggle —— 二次确认、错误提示、loadData 全在里面，不要再写一份。

                   ⚠️ design-system §4.2 的内容约定是「危险按钮不放最外侧」（甩到边缘的光标最容易落在最外那个）。
                   这里是用户在 #109-4 里明确要求的位置，属于一次有意识的让步，理由：
                   handleToggle 带 ElMessageBox 二次确认，点错的代价是按一下 Esc；
                   真正不可逆的「删除」仍然待在 Split 菜单里（danger + divided），没有被提到外侧。 -->
              <el-button
                size="small"
                :type="row.enabled ? 'danger' : 'default'"
                :plain="row.enabled"
                @click="handleToggle(row)"
              >
                {{ row.enabled ? '禁用' : '启用' }}
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- showFooterBar = 有数据，或者虽然列表空了但还留着勾选。
         一条数据都没有（且没勾选）时这条底栏只剩「共 0 条数据」和一个作用不到任何数据的
         每页条数选择器，正下方还压着空态自己的指路文案，纯属噪声——直接不渲染。
         .dd-fixed-page 下 .table-card 是 flex:1 1 0，少一条底栏只是把高度让给表格，不会塌。 -->
    <div v-if="showFooterBar" class="pagination-bar">
      <span class="pagination-total">共 {{ total }} 条数据</span>
      <div class="pagination-actions">
        <div class="page-size-picker">
          <span class="page-size-picker__label">每页</span>
          <el-select
            :model-value="pageSizeSelection"
            size="small"
            style="width: 112px"
            @change="handlePageSizeSelect"
          >
            <el-option
              v-for="option in pageSizeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </div>
        <el-pagination
          v-if="showPager"
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="prev, pager, next"
          @current-change="handlePageChange"
        />
      </div>
    </div>

    <el-dialog v-model="showExportDialog" title="导出环境变量" width="600px" :fullscreen="isMobile">
      <div class="export-format-switch">
        <el-radio-group v-model="exportFormat" @change="refreshExport">
          <el-radio-button value="shell">Shell</el-radio-button>
          <el-radio-button value="js">JavaScript</el-radio-button>
          <el-radio-button value="python">Python</el-radio-button>
        </el-radio-group>
        <el-button size="small" @click="copyExport">
          <el-icon><CopyDocument /></el-icon>复制
        </el-button>
      </div>
      <el-alert
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 12px"
        :title="`导出范围：${exportScopeText}`"
      />
      <pre class="export-preview">{{ exportContent }}</pre>
    </el-dialog>

    <EnvEditDialog
      v-model="showEditDialog"
      :mode="editDialogMode"
      :initial-data="currentEditEnv"
      :groups="groups"
      :submitting="editSubmitting"
      @save="handleSave"
    />

    <EnvImportDialog
      v-model="showImportDialog"
      :submitting="importSubmitting"
      @import="handleImport"
    />

    <EnvBatchRenameDialog
      v-model="showBatchRenameDialog"
      :submitting="batchRenameSubmitting"
      @confirm="confirmBatchRename"
    />

    <EnvBatchGroupDialog
      v-model="showBatchGroupDialog"
      :groups="groups"
      :submitting="batchGroupSubmitting"
      @confirm="confirmBatchGroup"
    />
  </div>
</template>

<style scoped lang="scss">
.envs-page {
  padding: 0;
}

/* ---- Page Header ---- */
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 18px;
  gap: 16px;

  h2 {
    margin: 0;
    font-size: 22px;
    font-weight: 700;
    color: var(--el-text-color-primary);
    line-height: 1.3;
  }

  .page-subtitle {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    margin: 4px 0 0;
  }

  .header-actions {
    display: flex;
    gap: 10px;
    flex-shrink: 0;
  }
}

/* ---- Toolbar ---- */
// 工具条：与定时任务页/执行日志页/订阅页对齐——上下统一间距、左右两区一行排布、gap 一致；
// 分组筛选（特有元素）沿用与搜索框相同的 12px 行内间距，宽度保持业务所需的 220px。
.toolbar {
  display: flex;
  justify-content: space-between;
  // 刻意【不】用 align-items: center（与 logs 页统一）：左槽任一支换成两行时，
  // center 会把只有 32px 的右区整个垂直居中到左槽的中线上，
  // 右侧按钮的中心比左区第一行低约 20px，看起来就是两边对不齐。
  // 改成 flex-start + 右区自己撑到 39px（= status-tabs 高度）并内部居中，两者都对齐到左区【第一行】的中心线。
  align-items: flex-start;
  margin: 14px 0;
  gap: 12px;
  flex-wrap: wrap;

  // 左槽：1×1 网格，筛选区与批量区【叠放在同一个格子里】，两支都常驻 DOM。
  // 这样左槽高度恒等于 max(两支高度)，勾选/取消勾选永远不改变工具栏高度。
  // align-items 必须是 start 不能是 center：任一支换成两行时，
  // center 会把只有一行的另一支垂直居中到两行的中线上，切过去时整排控件上下错位。
  // min-height 取 status-tabs 的实测高度 39px——批量按钮只有 32px，没有这个下限的话
  // 两支都是单行时整条工具栏会比筛选态矮 7px。
  &__left {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: start;
    flex: 1;
    min-width: 0;
    min-height: 39px;
  }

  // 筛选区：原来直接挂在 __left 上的那套横排（状态分段 + 搜索框 + 分组筛选），
  // 现在下沉一层与批量区叠放，间距/换行规则原样搬过来。
  &__filters {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    min-width: 0;
  }

  &__right {
    display: flex;
    align-items: center;
    gap: 10px;
    // 与左区第一行对齐的另一半：撑到同样的 39px，内部 center 让 32px 的按钮落在同一条中线上
    min-height: 39px;
  }

  // 三个输入控件：定宽 → 可伸缩。
  //
  // v3.2.5 加了「变量名筛选」，按原来的定宽（260 + 220 + 新增 220）筛选区自然宽会从 ~724px 涨到 ~966px，
  // 而 1280 宽 + 侧栏展开时左槽只有约 805px（1280 − 221 侧栏 − 40 页面内边距 − ~202 右区 − 12 gap），
  // 必然换成两行、白吃掉表格 ~50px 高度。
  //
  // 解法是让 flex-basis 取【收缩下限】、max-width 取【理想宽】：
  // 换行是按 flex-basis（hypothetical main size）算的，所以阈值只由下限决定 ——
  //   230(tabs) + 3×12(gap) + 160 + 150 + 150 ≈ 726px，和加控件之前的 ~724px 基本持平，换行点没有被推高；
  // 而有余量时 flex-grow 会把它们长回 260 / 220 / 220，宽屏观感与以前一致。
  // grow 权重 2:1:1 是刻意的：余量优先补给搜索框，它是最常用、最吃宽度的那个。
  //
  // 🔴 不要改回 width：写死宽度换行阈值就重新变成 966px，1280 + 展开侧栏必然两行。
  // 🔴 也不要只给搜索框设 flex 而让另外两个保持定宽：换行阈值取的是三者 basis 之和，漏一个就白做。
  &__search {
    flex: 2 1 160px;
    max-width: 260px;
  }

  &__group-filter {
    flex: 1 1 150px;
    max-width: 220px;
  }

  &__name-filter {
    flex: 1 1 150px;
    max-width: 220px;
  }
}

// 状态分段控件：与定时任务页/执行日志页/订阅页一致的灰底槽 + 选中态白底品牌色 + 1px 边框
.status-tabs {
  display: inline-flex;
  background: var(--el-fill-color-light);
  // 分段控件的灰底槽 → control 档。
  // 全站 13 处分段槽（含 global.scss 的通用类 .dd-seg-group）统一走 control：
  // 槽只比槽内的项高 6px（padding 3px），槽外圈取 surface(10px)、内层项取 control(6px)
  // 的话，选中项白底的四角会从灰底槽里探出来，露出一圈错位的角。同档才严丝合缝。
  border-radius: var(--dd-radius-control);
  padding: 3px;
  gap: 2px;
}

.status-tab {
  padding: 6px 14px;
  // 槽内的分段项 → control 档
  border-radius: var(--dd-radius-control);
  // 未选中态用透明边框占位，选中态只换边框颜色，避免尺寸跳动
  border: 1px solid transparent;
  background: transparent;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition:
    color var(--dd-motion-fast) var(--dd-ease-standard),
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    border-color var(--dd-motion-fast) var(--dd-ease-standard);
  white-space: nowrap;

  &:hover {
    color: var(--el-text-color-primary);
  }

  &.active {
    background: var(--el-bg-color);
    color: var(--el-color-primary);
    border-color: var(--el-border-color-lighter);
    font-weight: 600;
  }
}

// 批量操作条：搬进左槽后可用宽度由左槽给（右边还站着导出/新建），窄窗口下这六个按钮排不下时
// 必须允许换行，否则会横向溢出被裁掉；换行后的两行高度也会被左槽如实吃进去（见下面的叠放规则），
// 所以勾选前后工具栏依然等高。
// 移动端竖排也吃这条 flex-wrap，不再在 768px 断点里重复写一遍。
.batch-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  // 与 .toolbar__right 站同一条基线：右区也是 min-height:39px + 内部 center，中心线在 19.5px。
  // 批量条本身只有 32px，而左槽是 align-items: start（任一支换两行时不能把另一支压到中线去），
  // 不补这个下限它就贴在网格行顶端、中心线只有 16px，勾选后整排批量按钮会比右侧按钮高 3.5px。
  // 39px 本来就是左槽的 min-height，补上不会改变左槽高度，高度不变式照旧成立。
  min-height: 39px;
}

// 勾选数：纯文字级提示，用次级色，不跟旁边那排实体按钮抢视觉重量
.batch-actions__count {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
  cursor: default;
}

// 左槽的两支叠放在同一个网格格子里：谁都不脱离文档流，所以两支都在为左槽撑高，
// 左槽高度 = max(两支高度)，切换时高度恒定不变，表格不会被工具栏推着重排。
// 切换只做 opacity：两个分支宽高差得远，一旦做尺寸过渡，左槽每帧变尺寸就会推着右侧的导出/新建来回移动，
// 而两支本身都是 flex-wrap 容器、每帧还要重算换行，整行会持续重排；opacity 不参与布局计算。
// 时长走令牌，prefers-reduced-motion 下自动降为 1ms 即等效关闭。
.toolbar__filters,
.batch-actions {
  grid-area: 1 / 1;
  min-width: 0;
  transition: opacity var(--dd-motion-fast) var(--dd-ease-standard);
}

// 当前不该显示的那一支：visibility: hidden 已经同时挡掉鼠标、Tab 焦点和读屏，
// 不要再叠 inert / aria-hidden / pointer-events；也【不能】改成 display: none，
// 那样它就不再撑高左槽，高度不变式立刻失效（桌面端会退回勾选时表格跳动）。
// 注意过渡是【单向】的：上面的 transition 只列了 opacity，visibility 不在其中、切换那一帧立即生效，
// 所以退场的那一支是硬切、只有进场的那一支有淡入。这是刻意的——两支叠在同一个格子里，
// 真做交叉淡出会有一段两排控件互相透视的重影，别为了「对称」把 visibility 加进 transition。
.is-swapped-out {
  opacity: 0;
  visibility: hidden;
}

// 本页原来有一份 .dd-status-switch-* 过渡（服务于状态列/移动卡片里「启用 ⇄ 禁用」那段文字的淡入淡出），
// 随状态列与移动端 switch 一起删除。其余用到同名过渡的页面（tasks / logs / deps / users /
// subscriptions / notifications / config-file）各自在自己的 scoped 样式里定义了一份，不受影响。

/* ---- Table Card ---- */
// 表格卡：无阴影，仅用 1px 边框与页面底色区分（dd-fixed-page 下的 flex + 内部滚动由全局规则接管）
.table-card {
  background: var(--el-bg-color);
  // 表格容器 → surface 档
  border-radius: var(--dd-radius-surface);
  border: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
}

/* ---- Pagination Bar ---- */
// 分页条：与定时任务页/执行日志页/订阅页一致的间距收敛（margin-top 14px）
.pagination-bar {
  margin-top: 14px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 0 4px;
}

.pagination-total {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.pagination-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}

.page-size-picker {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.page-size-picker__label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

/* ---- Table internals ---- */
:deep(.el-table) {
  // 边框统一走令牌，明暗自动适配（原写死浅灰会在暗色串色）
  --el-table-border-color: var(--el-border-color-lighter);

  .el-table__header-wrapper th {
    border-bottom: 1px solid var(--el-border-color-light);
  }

  .el-table__row td {
    border-bottom: 1px solid var(--el-border-color-lighter);
  }

  .el-table__cell {
    padding: 12px 0;
  }
}

:deep(.env-table--compact .el-table__cell) {
  padding-top: 8px;
  padding-bottom: 8px;
}

:deep(.env-table--compact .env-value-text),
:deep(.env-table--compact .env-remarks-text),
:deep(.env-table--compact .env-name) {
  font-size: 12px;
}

:deep(.env-table--compact .time-text) {
  font-size: 11px;
}

:deep(.env-table--compact .pinned-chip),
:deep(.env-table--compact .group-pill) {
  padding-top: 2px;
  padding-bottom: 2px;
  font-size: 11px;
}

/* ---- Mobile ---- */
.env-mobile-scroll {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

// 桌面空态/骨架容器（特有元素）：纯色底 + 1px 边框，明暗自动适配；保留自定义空态结构
.env-desktop-state {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 360px;
  padding: 28px 24px;
  border: 1px solid var(--el-border-color-lighter);
  // 它站在表格卡的位置上（空态/加载态占位）→ 与 .table-card 同走 surface 档
  border-radius: var(--dd-radius-surface);
  background: var(--el-bg-color);
}

.env-desktop-state--loading {
  display: grid;
  align-content: start;
  justify-items: stretch;
  gap: 16px;
}

// 骨架屏（特有元素）：底色走 --el-fill-color 系令牌，微光用半透明表面色，明暗皆不串色
.env-skeleton {
  position: relative;
  overflow: hidden;
  // 骨架条是标题/工具栏/表格行的占位，尺寸都是控件量级 → control 档
  border-radius: var(--dd-radius-control);
  background: var(--el-fill-color);
}

.env-skeleton::after {
  content: '';
  position: absolute;
  inset: 0;
  transform: translateX(-100%);
  background: linear-gradient(
    90deg,
    transparent 0%,
    color-mix(in srgb, var(--el-bg-color) 70%, transparent) 48%,
    transparent 100%
  );
  animation: env-skeleton-shimmer 1.35s ease-in-out infinite;
}

.env-skeleton--title {
  width: min(320px, 32%);
  height: 22px;
}

.env-skeleton--toolbar {
  width: min(460px, 48%);
  height: 40px;
  margin-bottom: 8px;
}

.env-skeleton--row {
  width: 100%;
  height: 58px;
}

.env-empty-copy {
  display: inline-flex;
  flex-direction: column;
  gap: 6px;
  color: var(--el-text-color-secondary);
}

.env-empty-copy strong {
  font-size: 16px;
  color: var(--el-text-color-primary);
}

.env-empty-copy span {
  font-size: 13px;
}

.env-empty-actions {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

/* ---- Mobile Card ---- */
.env-card__title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

// 移动端拖拽手柄（特有元素）：色板已走令牌，过渡统一到 motion/ease 令牌
.env-mobile-drag-handle {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-light);
  // 30×30 的图标按钮 → control 档
  border-radius: var(--dd-radius-control);
  color: var(--el-text-color-secondary);
  cursor: grab;
  touch-action: none;
  transition:
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    color var(--dd-motion-fast) var(--dd-ease-standard),
    border-color var(--dd-motion-fast) var(--dd-ease-standard);
  -webkit-user-select: none;
  user-select: none;
  padding: 0;
}

.env-mobile-drag-handle:active {
  cursor: grabbing;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  border-color: var(--el-color-primary-light-7);
}

.env-card__tools {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  min-width: 0;
}

// 移动卡片的操作区分栏：一行三个（gap 8px ⇒ 基准宽 33.33% - 6px 差不多正好三列）。
// v3.2.0 起按钮从 4 个变成 5 个（编辑 / 禁用·启用 / 复制 / 置顶 / 删除），排成 3 + 2 两行，
// 第二行两个靠 flex-grow 各自摊到约一半——比原来 ≤768px 那条 `calc(50% - 4px)` 的
// 2 + 2 + 1 好看：那样第 5 个会孤零零地占满一整行，而落在第 5 位的正好是 danger 的「删除」，
// 视觉重量最重的按钮反而最显眼。所以 ≤768px 那条覆写已删除，两档共用这一条。
// 极窄机型（320px）核算：卡片内容宽约 296px，三列每列约 93px，
// 最长的「取消置顶」= 4×12 + 22（EP small 内边距）= 70px，仍放得下。
.env-card__actions > * {
  flex: 1 1 calc(33.33% - 6px);
}

// 禁用移动卡：与桌面禁用行同一套弱化（浅底 + 文本降一档），完整理由见下方
// 「---- Disabled Row ----」那段注释。
// global.scss 的 `.dd-mobile-card { background: var(--el-bg-color) }` 是 (0,1,0)，
// 这条 scoped 之后是 (0,2,0)，压得过。刻意排在 .env-card--pinned 之前：
// 那条目前只改 border-color / box-shadow、不改 background，当前虽不冲突，
// 但保持与桌面一致的「置顶写在后面、置顶赢」顺序，以后给置顶加底色时不用再回来调。
.env-card--disabled {
  background: var(--el-fill-color-lighter);
}

// 置顶移动卡（特有元素）：只保留置顶语义的橙色左缘标识，不再叠加环境光阴影
.env-card--pinned {
  border-color: rgba(245, 166, 35, 0.28);
  box-shadow: inset 4px 0 0 #f5a623;
}

/* ---- Cell Styles ---- */
// 表格操作列：类名与定时任务页/依赖管理页统一叫 .action-btns（原来叫 .action-group，
// 是本页独有的一个名字，改名只是为了三页一致，行为不变）——居中排布 + 4px 间隙。
.action-btns {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;

  // EP 自带 `.el-button + .el-button { margin-left: 12px }` 会叠加在上面的 flex gap 上。
  // 撑破列宽后 .cell 的 overflow:hidden 变成可滚动容器，点右侧按钮会触发 scrollIntoView
  // 让整行左移且不复位——这是本列最容易踩的坑，所以间距统一交给 gap
  //（与 tasks / deps / subscriptions 三页一致）。
  //
  // Split Button 内部就是相邻的两个 .el-button（主体 + caret），这条选择器照样命中；
  // EP 的 el-button-group 自己也置 0，两处一致，任何一处改动都不会让 caret 被推开。
  // 现在格子里还多了外置的「禁用 / 启用」，它与 Split Button 之间的间距也只由 gap:4px 决定。
  :deep(.el-button + .el-button) {
    margin-left: 0;
  }

  // 原来这里还有一条 `:deep(.el-button) { padding: 4px 8px }` 的收窄覆写，是当年为了把
  // Split Button 塞进 110px 列宽才加的。列宽已按 172 重算（见上方操作列注释），
  // 定时任务页也在 v3.0.x 去掉了同一条，这里跟着删掉、改回 EP small 档默认的 5px 11px ——
  // 列宽估算用的正是这个默认值，把它改回收窄档会让上面那笔账对不上。
}

.env-name-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

// 名称本体是一段【纯文本】（<span>），不是按钮也不是链接：可划选、可复制是硬要求，
// 详见模板里那条 🔴 注释。所以这里既没有 cursor:pointer 也没有 hover 下划线 ——
// 名称本身不可点，给它挂可点反馈只会骗人（真正可点的是旁边的 .env-name-filter-btn）。
// 保留 --el-color-primary 是它一直以来的取色（等宽字 + 主题色 = 标识符强调，不是链接语义）。
//
// flex 从 `1`（= 1 1 0%）改成 `0 1 auto`：名称后面新增了 .env-name-filter-btn，
// 名称若继续【撑满】整格，那颗按钮会被顶到名称列的最右缘、离它要操作的名字十几个字远，
// 还紧贴着「值」列的文字，看着像是下一列的东西。改成不 grow 之后按钮紧跟在名字后面，
// 与「值」列「文本 + 复制按钮」的排法一致。
// 🔴 min-width:0 才是省略号能生效的真正前提（允许 flex 项收缩到 min-content 以下），别删；
//    flex-shrink 保持默认的 1，所以名字变长时照样收缩出省略号。
// 🔴 原来靠 flex:1 把「置顶」标签顶到右缘的效果没有丢，改由 .env-name-filter-btn 的
//    margin-right:auto 承担（见下条规则），两处要一起看。
.env-name {
  min-width: 0;
  flex: 0 1 auto;
  font-family: var(--dd-font-mono);
  font-size: 13px;
  color: var(--el-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

// 「只看这个变量名」按钮：桌面表格与移动卡片共用这一个类（跟 .env-name 一样两端同源）。
// 除 margin-right 外，其余声明与「值」列的 .env-copy-btn 逐字相同 —— 同一张表里的行内图标按钮
// 只有一款外观；padding 2px 也正是名称列 min-width 那笔账里按 18px 计的来源（图标 14 + 2×2），
// 改动它要回去同步改列宽。
// 没有 hover/opacity 门控：与 .env-copy-btn 同一条不变量（触屏没有 hover，且透明元素照样进 Tab 焦点序）。
// 焦点环不用自己写：EP 的 el-button 自带 :focus-visible 样式，不像原来那个裸 <button> 需要手动补。
.env-name-filter-btn {
  flex-shrink: 0;
  // 接手 .env-name 原来那份 flex:1 的活：本格的剩余空间全归这条 auto 边距，
  // 于是按钮贴着名字、后面的「置顶」标签仍然靠在最右缘（与改动前逐像素一致）。
  // 没有「置顶」标签的行看不出区别（空白本来就在右边），别当成冗余删掉。
  margin-right: auto;
  color: var(--el-text-color-secondary);
  padding: 2px;
  // 只留颜色过渡；时长走令牌而不是写死毫秒数，理由同 .env-copy-btn（写死的绕不过 reduced-motion 降级）
  transition: color var(--dd-motion-fast) var(--dd-ease-standard);
}

// 置顶标签：纯色底 + 1px 边框（原胶囊渐变与内描边已去掉）
.pinned-chip {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  // 走 control 而不是 pill：它和分组标签 .group-pill 是同一格里的一组同级小标签
  // （紧凑模式那条规则也是把两者一起收的），做成胶囊会跟旁边的分组标签形状打架。
  border-radius: var(--dd-radius-control);
  font-size: 12px;
  font-weight: 700;
  color: #8a4b00;
  background: #ffd66b;
  border: 1px solid rgba(196, 118, 0, 0.28);
}

.env-value-text,
.env-remarks-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.env-value-cell {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.env-value-text {
  font-family: var(--dd-font-mono);
  color: var(--el-text-color-primary);
}

// 值列的复制按钮：常驻显示（v3.2.0 / #109-4）。
// 原来是 `opacity: 0` + 一条 `:deep(.el-table__row:hover) .env-copy-btn { opacity: 1 }`，
// 即整页唯一一个「鼠标移上去才出现」的控件，两条已一并删除：
//   1) 触摸屏没有 hover，平板/触屏笔记本上这个按钮根本摸不出来；
//   2) opacity:0 的元素照常进 Tab 焦点序，键盘用户会聚焦到一个看不见的按钮上；
//   3) 移动端卡片里的同款复制按钮本来就是常驻的，两端不一致。
// 顺带说明：本页操作栏（.action-btns）一直就是常驻的，没有任何 hover/opacity 门控 ——
// 那是要守住的不变量，别顺手给它加 hover 显隐。
.env-copy-btn {
  flex-shrink: 0;
  color: var(--el-text-color-secondary);
  padding: 2px;
  // 只留颜色过渡（EP link 按钮 hover 换色）。时长走令牌而不是写死毫秒数：
  // 写死的会绕过 prefers-reduced-motion 降级，令牌在降级媒体查询里会被压到 1ms。
  transition: color var(--dd-motion-fast) var(--dd-ease-standard);
}

.env-remarks-text {
  color: var(--el-text-color-regular);
}

.env-empty-text {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

// 分组标签：纯色底；原来的内描边改成真实 1px 边框，保证浅底在白色表格上仍有分隔
.group-pill {
  --group-hue: 210;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  padding: 4px 10px;
  // 名字里虽然叫 pill，但它是分组标签（tag 一类）→ control 档，和 .pinned-chip 保持同档
  border-radius: var(--dd-radius-control);
  font-size: 12px;
  font-weight: 600;
  color: hsl(var(--group-hue) 55% 32%);
  background: hsl(var(--group-hue) 85% 96%);
  border: 1px solid hsl(var(--group-hue) 72% 78% / 0.7);
}

.group-pill-list {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
  min-width: 0;
}

.group-pill-list--table {
  justify-content: center;
}

.group-pill__text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

// 分组色标
.group-dot {
  width: 7px;
  height: 7px;
  // 🔴 固定 2px，既不进圆形白名单、也不吃 control 档（v3.1.0 新立的「≤12px 小色标」判据）。
  //
  // 它的颜色由 --group-hue（按分组名派生）决定，**与运行状态无关**，表达的是「分类」
  // 而不是「这个东西现在是什么状态」—— 语义上就不该和 .pulse-dot / .env-status 那类
  // 状态灯长得一样。原来写 50% 是照着 .pulse-dot 抄的，理由并不成立。
  //
  // 但也不能改成吃 control：7×7 的盒子上，6px 半径会触发 CSS 的圆角等比收缩
  // （一条边上两个半径之和 12px > 边长 7px ⇒ 全部夹到 3.5px），结果还是一个正圆，
  // 等于什么都没改。只有写死一个小于半边长的值才稳得住方形轮廓。
  border-radius: 2px;
  background: hsl(var(--group-hue) 72% 48%);
  flex-shrink: 0;
}

// 环境变量状态灯：名称前的 8×8 圆点，绿 = 已启用 / 红 = 已禁用（桌面表格与移动卡片同用）。
// 它接替了原来那一整列「状态」（el-switch + 启用/禁用文字），随之删掉的还有
// .env-status-cell / .env-status-inline / .env-status-text 三个类和本页那份
// .dd-status-switch-* 过渡（切换动效跟着 switch 一起没了，其余页面各有自己的一份，不受影响）。
//
// 🔴 固定 50%：这是 design-system §1 的「状态圆点」白名单，不是漏改，圆角扫描别把它当漏网点改掉。
//    两个理由：
//    a) 圆点 = 运行状态是全行业约定；而同一格里 7×7 的 .group-dot 反过来刻意写死 2px 方角来表达
//       「分类」（见它自己那段注释）。两者一圆一方，正是靠形状把「分类色标」与「状态灯」分开的，
//       任何一边改形状都会让它俩撞脸。
//    b) 就算改成吃 control(6px) 也是白改：8×8 的盒子上，一条边两个半径之和 12px > 边长 8px，
//       会触发 CSS 圆角等比收缩、全部夹到 4px = 还是正圆，绕远路得到同一结果还多一次误改机会。
//
// 取色用 --el-color-success / --el-color-danger 两个语义令牌，暗色主题自动适配；
// 不要写死 #10b981 / #ef4444 之类，写死的在暗色下不跟随、会串色。
// 红绿是最典型的色觉障碍撞色对，只靠颜色传达状态对红绿色盲用户等于没有状态，
// 所以模板上必须同时挂 title（鼠标悬停）与 role="img" + aria-label（读屏）——
// role 不能省：光有 aria-label 的裸 <span> 不是可访问对象，读屏一般不会念出来。
.env-status-dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--el-color-danger);
}

.env-status-dot.is-enabled {
  background: var(--el-color-success);
}

.time-text {
  font-family: var(--dd-font-mono);
  font-size: 12px;
  color: var(--el-text-color-regular);
}

// 桌面拖拽手柄（特有元素）：色板已走令牌，过渡统一到 motion/ease 令牌
.env-drag-handle {
  cursor: grab;
  color: var(--el-text-color-placeholder);
  font-size: 16px;
  transition: color var(--dd-motion-fast) var(--dd-ease-standard);

  &:hover {
    color: var(--el-text-color-secondary);
  }

  &:active {
    cursor: grabbing;
  }
}

:deep(.env-drag-col) {
  padding: 0 !important;
}

/* ---- Disabled Row ---- */
// 禁用行的整行弱化（issue #109 追评：「禁用的变量文字淡一点，更突出已禁用状态，
// 但不确定会不会影响可读性」）。提出者的担心是对的，所以这里**不写整行 opacity**，
// 下面是实算的数据：
//
// 明色下表格行底是 --el-fill-color-blank(#fff)，按 WCAG 相对亮度算出的基线对比度是
//   .env-name        --el-color-primary(#409eff)      2.78:1  ← 本来就不到 AA 的 4.5:1
//   .env-value-text  --el-text-color-primary(#303133) 13.02:1
//   .env-remarks-text / .time-text
//                    --el-text-color-regular(#606266)  6.11:1
// 整行叠 opacity 之后（前景与白底混合再算）：
//   α=0.75 → primary 5.88 / regular 3.49
//   α=0.85 → primary 8.08 / regular 4.33   ← 备注仍然不到 4.5，已经破线
//   α=0.90 → primary 9.51 / regular 4.84   ← 到这一档人眼已经看不出「淡了」
// 也就是「看得出被调暗」和「读得清」在明色下不能同时成立。暗色余量很大，
// 但不能为暗色牺牲明色。
//
// 改用【浅底 + 文本降一档语义令牌】：弱化感来自「彩色 → 中性灰」的色相变化，不是变淡。
//   名称 --el-color-primary(2.78:1) → --el-text-color-regular(在 #fafafa 上 5.85:1)，对比度反而升了
//   值   --el-text-color-primary(12.47:1) → --el-text-color-regular(5.85:1)，明显降一档且仍过 AA
//   备注 / 更新时间 已经是 regular = 触底，不再往下压（降到 secondary 只有 2.95:1）
// 红点与操作列的「启用」按钮保持原样，它们仍是主要的状态载体；
// 分组标签 / 复制按钮 / 拖拽手柄一律不动 —— 前者与启用状态正交，
// 后两者在禁用行上仍然可点，调暗一个能点的控件会误导用户以为它失效了。
//
// 🔴 绝不要改成给 tr / td 写 opacity：操作列是 fixed="right"，EP 的固定列是
//    `position: sticky !important` + z-index，`opacity < 1` 会造出新的层叠上下文、打乱固定列层级。
// 🔴 选择器里的 `.el-table` 与下面置顶行那条同理，是为了压过 EP 的
//    `td.el-table-fixed-column--right { background: inherit }`(0,2,2)，别删。
// 🔴 本块必须排在 `.env-row-pinned` **之前**：两者特异性同为 (0,3,1)，靠书写顺序让置顶赢
//    —— 既置顶又禁用的行仍显示橙色底（置顶是用户主动标记，语义优先级更高）。
// 🔴 不要用 --el-fill-color-light：那正是 EP 的行 hover 底色，用了会让禁用行看起来像被永久悬停。
// 悬停禁用行时 EP 的 hover 底(0,3,2；暗色下还带 !important)会盖过这条 —— 与置顶行一致，
// 是期望行为，别去 !important 强压，强压会让用户失去「鼠标停在哪一行」的反馈。
:deep(.el-table .env-row-disabled > td) {
  background: var(--el-fill-color-lighter);
}

// 桌面与移动共用同一条文本降档规则（两端本来就共用 .env-name / .env-value-text 两个类）
:deep(.env-row-disabled) .env-name,
:deep(.env-row-disabled) .env-value-text,
.env-card--disabled .env-name,
.env-card--disabled .env-value-text {
  color: var(--el-text-color-regular);
}

/* ---- Pinned Row ---- */
// 置顶行：整行纯色淡橙底（原来的横向渐变已去掉），左缘 4px 橙色标识保留。
//
// 🔴 选择器里的 `.el-table` 不是可有可无的层级修饰，是为了压过 EP 的固定列规则，别顺手删掉：
// 操作列加 fixed="right" 之后，EP 会给这一格补上
// `.el-table__body-wrapper tr td.el-table-fixed-column--right { background: inherit }`（特异性 0,2,2），
// 而本规则原来是 `:deep(.env-row-pinned > td)` = `[data-v-x] .env-row-pinned > td`（0,2,1），
// 类数相同、元素数少一个 ⇒ 输给 EP，置顶行最右边那一格会退回普通行底色，整行淡橙底缺一块。
// 加上 `.el-table` 后是 (0,3,1)，类数 3 > 2 直接赢下 EP 那条。
// 同时它仍然【输给】EP 的行 hover（`.el-table__body tr.hover-row > td.el-table__cell` = 0,3,2），
// 所以置顶行悬停仍会正常变成 hover 底色，与改动前一致。
:deep(.el-table .env-row-pinned > td) {
  background: color-mix(in srgb, #ffd66b 12%, var(--el-table-tr-bg-color));
}

:deep(.env-row-pinned > td:first-child) {
  box-shadow: inset 4px 0 0 #f5a623;
}

/* ---- Export Dialog ---- */
.export-format-switch {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.export-preview {
  background: var(--el-bg-color-page);
  // 导出预览是弹窗里的一整块代码面板 → surface 档
  border-radius: var(--dd-radius-surface);
  padding: 16px;
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.6;
  max-height: 400px;
  overflow-y: auto;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}

@keyframes env-skeleton-shimmer {
  100% {
    transform: translateX(100%);
  }
}

/* ---- Responsive: 768px (Mobile) ---- */
@media screen and (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
    margin-bottom: 14px;

    h2 { font-size: 18px; }

    .header-actions {
      width: 100%;
      flex-wrap: wrap;
    }
  }

  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;

    // 竖排下左槽由内容自然撑高：桌面那条 39px 下限是为了对齐单行工具栏，
    // 这里筛选控件本来就要堆四行（状态分段 / 搜索框 / 分组筛选 / 变量名筛选），留着只会在批量区那一支下面多出一截空白。
    // 左槽是 1×1 网格、子项默认就铺满整列宽，align-items 从 start 放回 stretch 让还显示着的那一支填满行高。
    // 注意：这条 min-height:0 与下面那条 display:none 是配套的——藏起来的那一支被拿出流之后，
    // 左槽高度就完全由还显示着的那一支说了算，不该再被 39px 顶着。
    &__left {
      align-items: stretch;
      min-height: 0;
    }

    &__filters {
      flex-direction: column;
      gap: 10px;
    }

    // 🔴 竖排下必须把 flex 收回 `0 0 auto`：主轴变成【纵向】后，桌面那套
    // `flex: 2 1 160px` 里的 160px 就变成了【高度】基准，grow 还会把控件按余量拉高，
    // 三个筛选控件会被撑成一柱高块。max-width 同理要放开，否则 220px 会把满宽控件截短。
    &__search,
    &__group-filter,
    &__name-filter {
      flex: 0 0 auto;
      width: 100% !important;
      max-width: none;
    }

    &__right {
      justify-content: flex-end;
    }
  }

  // 移动端把藏起来的那一支直接从流里拿掉。
  // 桌面端留着它是为了锁死工具栏高度（dd-fixed-page 是定高 flex 列，工具栏差多少表格就反向补多少），
  // 但 dd-fixed-page 只在 ≥769px 生效，移动端是普通文档流、表格不会被工具栏挤压，
  // 留着竖排的筛选区（状态分段 + 搜索框 + 分组筛选 + 变量名筛选各占一行）会在批量态白占一大截空高。
  // 这条【只能】落在 ≤768px 内：写到外面桌面端就退回今天的跳动。
  .toolbar__filters.is-swapped-out,
  .batch-actions.is-swapped-out {
    display: none;
  }

  .status-tabs {
    width: 100%;
    overflow-x: auto;
  }

  .pagination-bar {
    flex-direction: column;
    align-items: stretch;
  }

  .pagination-actions {
    justify-content: space-between;
  }

  .page-size-picker {
    width: 100%;
    justify-content: space-between;
  }

  .env-card__title-row {
    flex-direction: column;
  }

  .env-card__tools {
    width: 100%;
    justify-content: space-between;
  }

  // 原来这里有一条 `.env-card__actions > * { flex: 1 1 calc(50% - 4px) }`（4 个按钮排成 2×2），
  // 按钮加到 5 个后已删除，统一走基态的 33.33%（3 + 2），理由见 .env-card__actions 那段注释。
}

// ===== 入场动画 =====
// 与定时任务页/执行日志页/订阅页统一：只对卡片级容器（工具条 / 表格卡 / 移动列表）做克制的淡入上移 + 轻微错落；
// 不给表格每一行或每张移动卡做 stagger；骨架屏/桌面空态（.env-desktop-state）不参与入场，避免与其自身加载/空态冲突。
// 时长走令牌，prefers-reduced-motion 时令牌自动降为 1ms 即等效关闭。
@keyframes dd-envs-rise-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.toolbar,
.table-card,
.dd-mobile-list {
  animation: dd-envs-rise-in var(--dd-motion-page) var(--dd-ease-decelerate) both;
}

// 轻微错落：工具条先入，表格卡/移动列表略晚
.table-card,
.dd-mobile-list {
  animation-delay: 60ms;
}
</style>

<style lang="scss">
/* 这些类由 sortablejs 在运行时动态加到 el-table 行 / body 上的拖拽克隆上，
   不在本组件 scoped 作用域内，必须用全局样式才能命中。 */

/* 被拖起的克隆行：不做放大与投影，改为实底 + 主色描边标出正在拖动的行。
   用 outline 而不是 border：el-table 是 border-collapse: separate，tr 上的 border 不会绘制。 */
.sortable-drag {
  background: var(--el-bg-color);
  opacity: 1 !important;
  // 保持 0，不吃令牌：这是铺满表格宽度的一整行 <tr>，贴着 table-card 的内边；
  // 给它加圆角只会在行两端与表格边框之间露出缺口（且 border-collapse:separate 下 tr 圆角几乎不可见）。
  border-radius: 0;
  cursor: grabbing;
  outline: 1px solid var(--el-color-primary);
  outline-offset: -1px;
}

/* 落点占位：高亮"插槽"，清楚指示会落到哪（主色浅底本身已足够区分，不再叠内描边） */
.sortable-ghost {
  background: var(--el-color-primary-light-9) !important;
  // 保持 0，不吃令牌：与 .sortable-drag 同理，它是贴边铺满的落点占位行
  border-radius: 0;
}
.sortable-ghost > td {
  opacity: 0.25;
}

/* 被选中的原行 */
.sortable-chosen {
  cursor: grabbing;
}
</style>
