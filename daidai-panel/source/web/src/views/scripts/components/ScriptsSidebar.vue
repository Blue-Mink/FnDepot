<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  DocumentAdd,
  Fold,
  FolderAdd,
  Refresh,
  Search,
  Upload,
  VideoPlay
} from '@element-plus/icons-vue'
import ScriptTreeNode from './ScriptTreeNode.vue'
import DdSplitButton from '@/components/ui/DdSplitButton.vue'
import type { SplitButtonItem } from '@/components/ui/DdSplitButton.vue'
import type { TreeNode } from '../types'

const props = defineProps<{
  isMobile: boolean
  mobileShowEditor: boolean
  treeLoading: boolean
  fileTree: TreeNode[]
  allowDrag: (draggingNode: any) => boolean
  allowDrop: (draggingNode: any, dropNode: any, type: string) => boolean
  onOpenCreateFile: () => void
  onOpenCreateDir: () => void
  onOpenUpload: () => void
  onOpenCodeRunner: () => void
  onRefresh: () => void | Promise<void>
  onNodeClick: (data: TreeNode) => void | Promise<void>
  onNodeDrop: (draggingNode: any, dropNode: any, dropType: string) => void | Promise<void>
  onOpenRename: (path: string) => void
  onDelete: (path: string, isDir: boolean) => void | Promise<void>
  onMoveToRoot?: (path: string, isDir: boolean) => void | Promise<void>
  /** 收起目录树。只在桌面宽屏可用，≤1024 由 mobileShowEditor 的互斥模式负责藏列表 */
  onToggleCollapse: () => void
}>()

const treeRef = ref()
const searchKeyword = ref('')

/**
 * 「新建」Split Button 的菜单项。
 *
 * 主体是「新建文件」——侧边栏这三个入口里最高频的一个，而且点错了只是弹出一个
 * 创建表单，可以直接关掉，代价最小。「上传文件」会带来外部文件落盘，语义比前两个重，
 * 沿用原下拉里的 divided 与前两项分开。这三项都不是不可逆操作，因此没有 danger 项。
 */
const newActionItems: SplitButtonItem[] = [
  { key: 'dir', label: '新建目录', icon: FolderAdd },
  { key: 'upload', label: '上传文件', icon: Upload, divided: true }
]

function onNewAction(key: string) {
  if (key === 'dir') props.onOpenCreateDir()
  else if (key === 'upload') props.onOpenUpload()
}

function filterNode(value: string, data: TreeNode) {
  if (!value) return true
  return (data.title || '').toLowerCase().includes(value.toLowerCase())
}

watch(searchKeyword, (val) => {
  treeRef.value?.filter(val)
})
</script>

<template>
  <aside class="scripts-sidebar" :class="{ mobile: isMobile }" v-show="!isMobile || !mobileShowEditor">
    <header class="sidebar-top">
      <div class="sidebar-search">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索文件或目录"
          clearable
          :prefix-icon="Search"
          class="sidebar-search-input"
        />
      </div>

      <div class="sidebar-toolbar">
        <div class="sidebar-toolbar-label">
          <span class="label-main">脚本文件</span>
        </div>
        <div class="sidebar-toolbar-actions">
          <!-- 原来是「新建 ▾」：主体点了只是展开菜单，最常用的「新建文件」还要再点一次，
               chevron 也是手写的。改成真正的 Split Button——主体直接新建文件，
               另外两项收进 caret 菜单。 -->
          <DdSplitButton
            class="new-split-button"
            label="新建文件"
            :icon="DocumentAdd"
            type="primary"
            size="small"
            :items="newActionItems"
            @click="onOpenCreateFile"
            @command="onNewAction"
          />

          <el-tooltip content="刷新" placement="bottom">
            <button class="icon-btn" aria-label="刷新" @click="onRefresh">
              <el-icon :size="15"><Refresh /></el-icon>
            </button>
          </el-tooltip>

          <!-- 收起目录树。只在桌面宽屏渲染：≤1024（isMobile 这个 prop 传的是 isCompactLayout）
               已经有「文件列表 ↔ 编辑器」互斥模式，再给一个收起按钮只会让两套逻辑打架。
               v-if 挂在 tooltip 上而不是 button 上，否则隐藏时 tooltip 会剩一个空触发器。 -->
          <el-tooltip v-if="!isMobile" content="收起文件树" placement="bottom">
            <button class="icon-btn" aria-label="收起文件树" @click="onToggleCollapse">
              <el-icon :size="15"><Fold /></el-icon>
            </button>
          </el-tooltip>
        </div>
      </div>
    </header>

    <div class="sidebar-tree" v-loading="treeLoading">
      <el-tree
        ref="treeRef"
        :data="fileTree"
        node-key="key"
        :props="{ children: 'children', label: 'title' }"
        :highlight-current="true"
        :expand-on-click-node="true"
        :filter-node-method="filterNode"
        draggable
        :allow-drag="allowDrag"
        :allow-drop="allowDrop"
        empty-text="暂无脚本文件"
        @node-drop="onNodeDrop"
        @node-click="onNodeClick"
      >
        <template #default="{ data }">
          <ScriptTreeNode :data="data" :on-open-rename="onOpenRename" :on-delete="onDelete" :on-move-to-root="onMoveToRoot" />
        </template>
      </el-tree>
    </div>

    <footer class="sidebar-footer">
      <button class="runner-card" @click="onOpenCodeRunner" aria-label="打开代码运行器">
        <div class="runner-card-icon">
          <el-icon :size="16"><VideoPlay /></el-icon>
        </div>
        <div class="runner-card-body">
          <span class="runner-card-title">代码运行器</span>
          <span class="runner-card-desc">粘贴片段即刻执行</span>
        </div>
      </button>
    </footer>
  </aside>
</template>

<style scoped lang="scss">
.scripts-sidebar {
  /* 宽度的单一真源在 index.vue 的 .scripts-workspace（--scripts-sidebar-width），折叠时那边置 0px。
     这里必须消费同一个变量：min-width 是 flex-basis 的下限，还写死 300px 的话
     index.vue 那边把 flex-basis 收到 0 也没用，卡片会被这一行顶住收不起来。
     min-width 同样要过渡 —— 若它瞬间跳回 300px，展开动画会被立刻夹到终点，看着像没有动画。 */
  width: var(--scripts-sidebar-width, 300px);
  min-width: var(--scripts-sidebar-width, 300px);
  transition:
    width var(--dd-motion-normal) var(--dd-ease-standard),
    min-width var(--dd-motion-normal) var(--dd-ease-standard);
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0;
  padding: 0;
  /* 卡片表面/边框由 index.vue 的 :deep(.scripts-sidebar) 统一负责，
     这里不再画 border-right 分隔线（两卡独立后无需分隔） */
  background: var(--el-bg-color);
  box-sizing: border-box;
  font-family: var(--dd-font-ui);
  overflow: hidden;
}

.sidebar-top {
  display: flex;
  border-bottom: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 80%, transparent);
  padding-bottom: 10px;
  position: relative;
  flex-direction: column;
  gap: 10px;
  flex-shrink: 0;
  padding: 16px 14px 0;
}

.sidebar-search-input {
  :deep(.el-input__wrapper) {
    // 输入框属控件类表面 → control 档
    border-radius: var(--dd-radius-control);
    padding: 4px 12px;
    box-shadow: 0 0 0 1px var(--el-border-color-lighter) inset;
    transition: box-shadow 0.2s, background 0.2s;
    background: var(--el-fill-color-light);
  }

  :deep(.el-input__wrapper.is-focus) {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--el-color-primary) 45%, transparent) inset;
    background: var(--el-bg-color);
  }

  :deep(.el-input__inner) {
    font-size: 13px;
    font-family: var(--dd-font-ui);
  }
}

.sidebar-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0 0;
}

.sidebar-toolbar-label {
  display: flex;
  align-items: baseline;
  gap: 6px;

  .label-main {
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--el-text-color-secondary);
  }
}

.sidebar-toolbar-actions {
  display: flex;
  padding: 2px;
  // 工具条按钮组的灰底槽 → control 档（与槽内按钮同档，圆角一致才不会露出内外错位的角）
  border-radius: var(--dd-radius-control);
  background: color-mix(in srgb, var(--el-fill-color-light) 84%, transparent);
  align-items: center;
  gap: 6px;
}

/* class 落到 DdSplitButton 的根节点（EP 的 div.el-dropdown）上。
   EP small 档按钮高 24px，会比右边 30px 的 .icon-btn 矮一截，工具条看着参差不齐，
   所以这里只把两半按钮的高度和字号拉回原先「新建」按钮的 30px / 12.5px。
   圆角与 caret 中缝的可见度由 global.scss 的 Split Button 一节统一负责，
   chevron 也由组件自带，不需要再手写。 */
.new-split-button {
  :deep(.el-button) {
    // 这里刻意【不写】border-radius：split-button 内部是 el-button-group，
    // 只有最外两个角该跟着 --dd-radius-control 变圆、中缝两侧必须保持直角，
    // 这套 first-child/last-child 规则由 EP 自己按 --el-border-radius-base 算好了。
    // 在这里补一句 border-radius 会把两半四个角一起改掉，中缝直接糊成一整条。
    height: 30px;
    font-size: 12.5px;
    font-weight: 500;
  }
}

.icon-btn {
  width: 30px;
  height: 30px;
  padding: 0;
  border: 1px solid var(--el-border-color-lighter);
  background: transparent;
  // 图标按钮属控件类表面 → control 档（与 ScriptsEditorPane 的 .sidebar-expand-btn 同档）
  border-radius: var(--dd-radius-control);
  color: var(--el-text-color-secondary);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s, background 0.15s, border-color 0.15s;

  &:hover {
    color: var(--el-color-primary);
    border-color: color-mix(in srgb, var(--el-color-primary) 40%, var(--el-border-color-lighter));
    background: color-mix(in srgb, var(--el-color-primary) 6%, transparent);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--el-color-primary) 50%, transparent);
    outline-offset: 1px;
  }
}

.sidebar-tree {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: 8px 10px 10px;

  :deep(.el-tree) {
    background: transparent;
    color: inherit;
    min-width: 0;
  }

  :deep(.el-tree-node),
  :deep(.el-tree-node__children) {
    min-width: 0;
  }

  :deep(.el-tree-node__content) {
    height: 34px;
    min-width: 0;
    padding-left: 4px;
    // 目录树的行是可点小块（hover / 选中都会上底色）→ control 档
    border-radius: var(--dd-radius-control);
    transition: background 0.15s;
    font-size: 13px;
    overflow: hidden;
  }

  :deep(.el-tree-node__content:hover) {
    background: var(--el-fill-color-light);
  }

  :deep(.el-tree-node.is-current > .el-tree-node__content) {
    background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
    position: relative;

    &::before {
      content: '';
      position: absolute;
      left: 0;
      top: 6px;
      bottom: 6px;
      width: 2.5px;
      // 装饰性细指示条 → pill 档，与 api-docs 的 .api-card 竖条、SponsorWall 的 .title-dot
      // 归为同一类，全站这一类统一走 pill。
      //
      // ⚠️ 原注释写的「圆角化后会缩成一个小圆点、彻底看不出是条」是错的：
      //    CSS 的圆角等比收缩只会把半径夹到 min(边长)/2，2.5px 宽的条最多得到 1.25px 半径，
      //    结果是一条【圆头细条】而不是圆点——条的长度（22px）一点没变。
      //    细条天然就是胶囊形，pill 与 control 在这个宽度下渲染结果相同，写 pill 只是让归类显式。
      border-radius: var(--dd-radius-pill);
      background: var(--el-color-primary);
    }
  }

  :deep(.el-tree-node.is-drop-inner > .el-tree-node__content) {
    background: color-mix(in srgb, var(--el-color-primary) 12%, transparent);
    outline: 2px dashed var(--el-color-primary);
    outline-offset: -2px;
  }

  :deep(.el-tree__drop-indicator) {
    height: 2px;
    background: var(--el-color-primary);
    // 🔴 保持 0，不吃令牌 —— 它与上面那条选中标识条【看着像同类，实则不是】：
    // 那条是「当前在哪一项」的装饰性标识（走 pill），这条是「松手会插到哪两行之间」的
    // 落点指示线，语义上等同于文本光标。方头两端才能把落点的横向范围指得干脆，
    // 端头一旦收圆，线的两头会先淡出再消失，看起来像没对齐任何一行。
    //
    // ⚠️ 原注释说「吃圆角会把两端磨成尖角」是错的（圆角只会变圆不会变尖）。
    //    实际吃 control 的话，2px 高会把半径夹到 1px——差别很小，但方向是错的，
    //    所以这里是按语义显式保持 0，不是因为「反正看不出来」。
    border-radius: 0;
  }

  :deep(.el-tree-node.is-dragging > .el-tree-node__content) {
    opacity: 0.4;
  }

  :deep(.el-tree-node__expand-icon) {
    color: var(--el-text-color-placeholder);
    font-size: 12px;
  }
}

.sidebar-footer {
  flex-shrink: 0;
  padding: 10px 14px 14px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.runner-card {
  width: 100%;
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  // 运行器入口是侧栏底部独立成块的卡片（带边框、有自己的留白）→ surface 档
  border-radius: var(--dd-radius-surface);
  background: var(--el-fill-color-light);
  color: inherit;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, border-color 0.15s;

  &:hover {
    background: color-mix(in srgb, var(--scripts-accent, #22c55e) 8%, var(--el-fill-color-light));
    border-color: color-mix(in srgb, var(--scripts-accent, #22c55e) 40%, var(--el-border-color-lighter));
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--scripts-accent, #22c55e) 70%, transparent);
    outline-offset: 2px;
  }
}

.runner-card-icon {
  width: 30px;
  height: 30px;
  // 30×30 的图标色底属控件类表面 → control 档
  border-radius: var(--dd-radius-control);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--scripts-accent, #22c55e);
  background: color-mix(in srgb, var(--scripts-accent, #22c55e) 12%, transparent);
  flex-shrink: 0;
}

.runner-card-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.runner-card-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  line-height: 1.2;
}

.runner-card-desc {
  font-size: 11.5px;
  color: var(--el-text-color-secondary);
  line-height: 1.2;
  letter-spacing: 0.1px;
}

.scripts-sidebar.mobile {
  width: 100%;
  min-width: 0;
  min-height: 0;

  .sidebar-toolbar {
    padding: 0;
  }

  :deep(.tree-node) {
    .tree-node-actions,
    .tree-node-ext {
      opacity: 1;
    }
  }
}
</style>


<style lang="scss">
html.dark {
  /* 暗色卡面/边框由 --el-bg-color 与 index.vue 的卡片样式自动适配，
     此处仅保留内部分区线与控件底色的暗色覆盖 */
  .scripts-sidebar .sidebar-top,
  .scripts-sidebar .sidebar-footer {
    border-color: rgba(255,255,255,0.08);
  }

  .scripts-sidebar .sidebar-toolbar-actions,
  .scripts-sidebar .runner-card {
    background: color-mix(in srgb, var(--el-bg-color-overlay) 92%, black);
  }
}
</style>
