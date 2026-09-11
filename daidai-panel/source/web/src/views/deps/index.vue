<template>
  <div class="deps-page dd-scroll-page dd-page-hide-heading">
    <div class="page-header">
      <div>
        <h2 class="page-title-with-icon"><el-icon><Box /></el-icon><span>依赖管理</span></h2>
        <p class="page-subtitle">
          管理运行时所需的依赖包和系统软件，确保依赖版本和任务稳定运行
        </p>
      </div>
    </div>

    <!-- Android 面具版：一键安装 Python / Node 解释器 -->
    <el-card
      v-if="androidStatus && androidStatus.supported"
      class="android-runtime-card"
      shadow="never"
    >
      <template #header>
        <div class="android-runtime-header">
          <span>
            <el-icon><Cpu /></el-icon>
            Android 脚本运行时 <el-tag size="small" type="info">面具版</el-tag>
          </span>
          <span class="android-runtime-meta">
            架构 {{ androidStatus.arch }} · 安装目录 {{ androidStatus.bin_dir }}
            <el-tag
              v-if="androidStatus.termux_detected"
              size="small"
              type="success"
              >已检测 Termux</el-tag
            >
          </span>
        </div>
      </template>

      <div class="android-runtime-tip">
        <el-alert type="info" :closable="false" show-icon>
          面具环境没有
          apt/apk，脚本解释器需要手动安装。点击下方按钮会把运行时下载解压到
          <code>{{ androidStatus.bin_dir }}</code
          >，随后 Python/Node 脚本即可运行。 如果装了 Termux，面板也会自动识别
          <code>/data/data/com.termux/files/usr/bin</code> 里的解释器。
        </el-alert>
      </div>

      <el-row :gutter="16" class="android-runtime-grid">
        <el-col
          v-for="item in androidStatus.runtimes"
          :key="item.name"
          :xs="24"
          :sm="12"
        >
          <div class="runtime-item">
            <div class="runtime-item__head">
              <b>{{ item.name }}</b>
              <el-tag v-if="item.installed" type="success" size="small"
                >已安装</el-tag
              >
              <el-tag v-else type="warning" size="small">未安装</el-tag>
            </div>
            <div class="runtime-item__meta">
              <div v-if="item.installed">
                <div>
                  路径: <code>{{ item.path }}</code>
                </div>
                <div v-if="item.version">版本: {{ item.version }}</div>
              </div>
              <div v-else>
                <template v-if="presetFor(item.name)">
                  将下载 {{ presetFor(item.name)?.label }}（约
                  {{ presetFor(item.name)?.size_mb }}MB）
                  <div
                    v-if="presetFor(item.name)?.note"
                    class="runtime-item__note"
                  >
                    提示：{{ presetFor(item.name)?.note }}
                  </div>
                </template>
                <template v-else>
                  当前架构 {{ androidStatus.arch }} 暂无预置下载源
                </template>
              </div>
            </div>
            <div class="runtime-item__actions">
              <el-button
                v-if="!item.installed"
                type="primary"
                size="small"
                :loading="androidInstallingName === item.name"
                :disabled="!presetFor(item.name)"
                @click="installAndroidRuntime(item.name)"
              >
                一键安装
              </el-button>
              <el-button
                v-else
                size="small"
                :loading="androidInstallingName === item.name"
                @click="installAndroidRuntime(item.name)"
              >
                重新安装
              </el-button>
              <el-button
                v-if="item.installed"
                type="danger"
                size="small"
                plain
                @click="uninstallAndroidRuntime(item.name)"
              >
                移除
              </el-button>
            </div>
          </div>
        </el-col>
      </el-row>

      <div v-if="androidInstallLog.length" class="android-runtime-log">
        <div class="android-runtime-log__title">
          安装日志
          <el-button link size="small" @click="androidInstallLog = []"
            >清空</el-button
          >
        </div>
        <pre v-html="androidInstallLogHtml"></pre>
      </div>
    </el-card>

    <div class="deps-tabs">
      <!-- 类型页签上各挂一个自己的失败数。三者之和 == 侧栏「依赖管理」角标，
           用户看到角标 9 时能直接看出是哪一类、各几个，不用挨个切标签去凑。
           与右边那排状态角标（level="info" + show-zero 的中性计数）刻意不同：
           这三个是「需要用户处理」的告警，所以用 danger，且为 0 时整个消失，
           免得三个红 0 常驻抢注意力。 -->
      <div class="status-tabs">
        <button
          :class="['status-tab', { active: activeTab === 'nodejs' }]"
          @click="
            activeTab = 'nodejs';
            depsPage = 1;
            loadData();
          "
        >
          Node.js
          <DdBadge
            :value="failedByType.nodejs"
            level="danger"
            title="Node.js 下安装失败的依赖数"
          />
        </button>
        <button
          :class="['status-tab', { active: activeTab === 'python' }]"
          @click="
            activeTab = 'python';
            depsPage = 1;
            loadData();
          "
        >
          Python3
          <DdBadge
            :value="failedByType.python"
            level="danger"
            title="Python 下安装失败的依赖数（含所有 Python 版本）"
          />
        </button>
        <button
          :class="['status-tab', { active: activeTab === 'linux' }]"
          @click="
            activeTab = 'linux';
            depsPage = 1;
            loadData();
          "
        >
          Linux
          <DdBadge
            :value="failedByType.linux"
            level="danger"
            title="Linux 下安装失败的依赖数"
          />
        </button>
      </div>
      <!-- 状态筛选是【纯前端】的：点这三个按钮只改 statusFilter + 复位页码，不重新请求
           （对比左边的 Node/Python/Linux 组，那一组每次都 loadData()）。
           depsApi.list() 一次就把当前运行时的全量依赖拉回来，分页也是前端 slice，
           所以角标里的数字是真实总数，不是拿当前页凑的。 -->
      <div class="status-tabs status-tabs--filter">
        <button
          :class="['status-tab', { active: statusFilter === '' }]"
          @click="
            statusFilter = '';
            depsPage = 1;
          "
        >
          全部
          <DdBadge
            :value="allCount"
            level="info"
            show-zero
            title="当前列表的依赖总数"
          />
        </button>
        <button
          :class="[
            'status-tab status-tab--success',
            { active: statusFilter === 'installed' },
          ]"
          @click="
            statusFilter = statusFilter === 'installed' ? '' : 'installed';
            depsPage = 1;
          "
        >
          已安装
          <DdBadge
            :value="installedCount"
            level="info"
            show-zero
            title="已安装的依赖数"
          />
        </button>
        <button
          :class="[
            'status-tab status-tab--danger',
            { active: statusFilter === 'failed' },
          ]"
          @click="
            statusFilter = statusFilter === 'failed' ? '' : 'failed';
            depsPage = 1;
          "
        >
          失败
          <DdBadge
            :value="failedCount"
            level="info"
            show-zero
            title="安装失败的依赖数"
          />
        </button>
      </div>
    </div>

    <div class="toolbar">
      <div class="toolbar__left">
        <!-- 原来这里平铺 5 个按钮（新增依赖 / 刷新 / 批量重装 / 导出清单 / 镜像源设置），
             主次不分、横着吃掉半条工具条。改成 Split Button：
             主体是「新增依赖」——它只打开一个弹窗，是这 5 个里点错代价最小的一个；
             其余 4 项收进菜单。原按钮的 :loading 在菜单项里没有对应表达，
             降级成 disabled（见 toolbarActionItems），避免刷新/导出进行中被重复点。 -->
        <DdSplitButton
          label="新增依赖"
          :icon="Plus"
          type="primary"
          size="default"
          :items="toolbarActionItems"
          @click="
            createType = activeTab;
            showCreateDialog = true;
          "
          @command="onToolbarAction"
        />
      </div>
      <div class="toolbar__right">
        <el-select
          v-if="activeTab === 'python'"
          v-model="pythonVersion"
          class="toolbar__python-version"
          placeholder="Python 版本"
          @change="handlePythonVersionChange"
        >
          <el-option
            v-for="runtime in pythonRuntimes"
            :key="runtime.version"
            :label="
              runtime.default ? `${runtime.label}（默认）` : runtime.label
            "
            :value="runtime.version"
          >
            <div class="python-runtime-option">
              <span>{{ runtime.label }}</span>
              <el-tag v-if="runtime.default" size="small" type="success"
                >默认</el-tag
              >
              <el-tag v-else-if="runtime.venv_healthy" size="small" type="info"
                >已初始化</el-tag
              >
            </div>
          </el-option>
        </el-select>
        <el-button
          v-if="activeTab === 'python'"
          @click="setCurrentPythonDefault"
          :disabled="pythonVersion === pythonDefaultVersion"
        >
          设为默认
        </el-button>
        <el-input
          v-model="searchKeyword"
          placeholder="搜索依赖包名称..."
          clearable
          class="toolbar__search"
          @keyup.enter="depsPage = 1"
          @clear="depsPage = 1"
        >
          <template #prefix
            ><el-icon><Search /></el-icon
          ></template>
        </el-input>
        <el-select
          v-model="statusFilter"
          placeholder="所有状态"
          clearable
          class="toolbar__filter"
          @change="depsPage = 1"
        >
          <el-option label="已安装" value="installed" />
          <el-option label="安装中" value="installing" />
          <el-option label="排队中" value="queued" />
          <el-option label="失败" value="failed" />
          <el-option label="已取消" value="cancelled" />
          <el-option label="卸载中" value="removing" />
        </el-select>
        <!-- 勾选后凭空冒出一个红按钮会把工具条右区顶一下。淡入淡出让它「浮现」出来，
             而不是瞬间插进去。只做 opacity：按钮的宽度仍是即时占位的，
             如果连宽度/高度一起过渡，工具条会在动画期间持续重排，整页跟着抖。 -->
        <Transition name="dd-batch-fade">
          <el-button
            v-if="selectedIds.length > 0"
            type="danger"
            plain
            @click="handleBatchDelete"
          >
            <el-icon><Delete /></el-icon> 批量卸载
          </el-button>
        </Transition>
      </div>
    </div>

    <el-alert
      v-if="activeTab === 'python'"
      class="python-runtime-hint"
      type="info"
      :closable="false"
      show-icon
    >
      <template #title>Python 多版本说明</template>
      <div class="python-runtime-hint__body">
        二进制部署不会内置三个
        Python，只需要在服务器安装实际要用的版本；面板会为可用版本创建独立依赖环境，未安装版本会明确提示不可用，不影响其他版本运行。
      </div>
      <div class="python-runtime-hint__body">
        当前正在展示 <b>Python {{ pythonVersion }}</b> 的依赖列表；
        系统默认版本是 <b>Python {{ pythonDefaultVersion }}</b>。
        如果默认版本当前不可用，页面会自动切到第一个可用版本，避免打开就是空白或报错。
      </div>
      <div class="python-runtime-hint__status">
        <el-tag
          v-for="runtime in pythonRuntimes"
          :key="runtime.version"
          size="small"
          :type="runtime.available ? 'success' : 'warning'"
          effect="plain"
        >
          {{ runtime.label }}：{{ runtime.available ? "可用" : "需先安装" }}
        </el-tag>
      </div>
    </el-alert>

    <!-- Linux 页签的说明。以前这里什么都没有，页签上只写着「Linux」两个字，
         用户根本看不出「这里装的就是 apk / apt 系统包」——issue #120 的用户
         在 Alpine 下装不上 opencv-python，问的是「能不能加个装系统依赖的功能」，
         而这个功能一直都在。包管理器/发行版/镜像源可配性全来自 GET /deps/mirrors，
         挂载时就拉好了（见 loadMirrorMeta），不用等用户打开镜像源弹窗。 -->
    <el-alert
      v-if="activeTab === 'linux'"
      class="linux-runtime-hint"
      type="info"
      :closable="false"
      show-icon
    >
      <template #title>Linux 系统包说明</template>
      <div class="linux-runtime-hint__body">
        这里安装的是<b>操作系统软件包</b>（走
        {{ linuxMirrorManagerText }} 包管理器），不是 Python / Node.js
        包。pip 安装时报缺 gcc、缺头文件、需要现场编译的依赖，要先在这里补上。
      </div>
      <div class="linux-runtime-hint__body">
        当前检测：包管理器 <b>{{ linuxMirrorManagerText }}</b>
        <span v-if="linuxMirrorDistributionText">
          · 发行版 <b>{{ linuxMirrorDistributionText }}</b></span
        >
        · 镜像源{{ linuxMirrorSupported ? "可配置" : "不可配置" }}。
        <span v-if="linuxMirrorMessage">{{ linuxMirrorMessage }}</span>
      </div>
      <div class="linux-runtime-hint__actions">
        <el-button
          type="primary"
          size="small"
          :disabled="linuxToolchainPackages.length === 0"
          @click="openLinuxToolchainInstall"
        >
          安装编译工具链
        </el-button>
        <span class="linux-runtime-hint__tip">{{ linuxToolchainTip }}</span>
      </div>
    </el-alert>

    <div v-if="isMobile" class="dd-mobile-list">
      <div
        v-for="(row, index) in paginatedDepsList"
        :key="row.id"
        class="dd-mobile-card"
      >
        <div class="dd-mobile-card__header">
          <div class="dd-mobile-card__title-wrap">
            <div class="deps-card__title-row">
              <div class="dd-mobile-card__selection">
                <el-checkbox
                  :model-value="isSelected(row.id)"
                  @change="toggleSelected(row.id, $event)"
                />
                <span class="dd-mobile-card__title">{{ row.name }}</span>
              </div>
              <span class="dd-mobile-card__subtitle">#{{ index + 1 }}</span>
            </div>
          </div>
        </div>
        <div class="dd-mobile-card__body">
          <div class="dd-mobile-card__grid">
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">状态</span>
              <div class="dd-mobile-card__value">
                <!-- 与桌面表格同一套过渡：key 绑状态值，只做 opacity -->
                <Transition name="dd-status-switch" mode="out-in">
                  <el-tag
                    :key="row.status"
                    :type="statusType(row.status)"
                    size="small"
                    effect="light"
                    >{{ statusLabel(row.status) }}</el-tag
                  >
                </Transition>
              </div>
            </div>
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">创建时间</span>
              <span class="dd-mobile-card__value">{{
                formatDateTime(row.created_at)
              }}</span>
            </div>
            <div v-if="activeTab === 'python'" class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">Python</span>
              <span class="dd-mobile-card__value">{{
                row.python_version || pythonDefaultVersion
              }}</span>
            </div>
          </div>
          <div class="dd-mobile-card__actions deps-card__actions">
            <el-button size="small" type="primary" plain @click="viewLog(row)"
              >日志</el-button
            >
            <el-button
              v-if="row.status === 'installing' || row.status === 'removing'"
              size="small"
              type="warning"
              plain
              @click="handleCancel(row)"
            >
              取消
            </el-button>
            <el-button
              size="small"
              type="warning"
              plain
              @click="handleReinstall(row)"
              :disabled="isProcessing(row.status)"
            >
              重装
            </el-button>
            <el-button
              size="small"
              type="danger"
              plain
              @click="handleDelete(row)"
              :disabled="isProcessing(row.status)"
            >
              卸载
            </el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleForceDelete(row)"
              :disabled="isProcessing(row.status)"
            >
              强制卸载
            </el-button>
          </div>
        </div>
      </div>

      <el-empty
        v-if="!loading && paginatedDepsList.length === 0"
        description="暂无依赖"
      />
    </div>

    <div v-else class="table-card">
      <el-table
        :data="paginatedDepsList"
        v-loading="loading"
        style="width: 100%"
        @selection-change="handleSelectionChange"
        :header-cell-style="{
          background: '#f8fafc',
          color: '#64748b',
          fontWeight: 600,
          fontSize: '13px',
        }"
      >
        <el-table-column type="selection" width="40" />
        <el-table-column prop="name" label="名称" min-width="160">
          <template #default="{ row }">
            <div class="dep-name-cell">
              <span
                class="dep-name-avatar"
                :style="{ background: getLetterColor(row.name) }"
                >{{ (row.name || "?").charAt(0).toUpperCase() }}</span
              >
              <span class="dep-name-text" :title="row.name">{{
                row.name
              }}</span>
            </div>
          </template>
        </el-table-column>
        <!-- 这里原本有一列「版本」，读 row.version，已整列删除。
             原因：Dependency 模型（server/model/dependency.go）根本没有 version 字段，
             ToDict() 也不输出它，所以 Node.js / Python / Linux 三种类型下这一列
             恒定渲染「-」，是一列纯噪音。
             真要展示版本，得先让后端在安装成功后把实际装到的版本回写进模型
             （pip show / npm ls / apk info 的输出解析），那是另一件事，不在本次范围。 -->
        <el-table-column
          v-if="activeTab === 'python'"
          prop="python_version"
          label="Python"
          width="110"
        >
          <template #default="{ row }">
            <el-tag size="small" type="info">{{
              row.python_version || pythonDefaultVersion
            }}</el-tag>
          </template>
        </el-table-column>
        <!-- 依赖状态是全站流转最密集的一处：排队中 → 安装中 → 已安装/失败，
             卸载时还会走 卸载中 → 行消失，全靠 3s 轮询推进。硬切换时用户只会看到
             文字突然变了，分不清是自己看漏了还是真的变了。out-in 让旧标签先淡出、
             新标签再淡入，交接过程本身就是「状态更新了」的信号。
             key 必须绑 row.status（状态值）：绑 row.id 的话同一行的 key 永远不变，
             过渡一次也不会触发。
             只做 opacity，不做位移——表格行里的位移会带着整行一起晃。 -->
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <Transition name="dd-status-switch" mode="out-in">
              <el-tag
                :key="row.status"
                :type="statusType(row.status)"
                size="small"
                effect="light"
                round
                >{{ statusLabel(row.status) }}</el-tag
              >
            </Transition>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            <span class="time-text">{{ formatDateTime(row.created_at) }}</span>
          </template>
        </el-table-column>
        <!-- 原来是「详情 + 取消/重装 + 更多▾」的手工版 split button：三个 text 按钮
             靠 .action-btns 的 gap 拼出来，还得把按钮压到 26px 才塞得进 176px。
             换成真正的 DdSplitButton 后只剩一个按钮组。
             宽度是【浏览器实测】的，不是估的（估算口诀在 caret 那一项上会算小）：
               主体「详情」42px（2×12 文字 + 本页 .action-btns 覆盖的 8px×2 内边距 + 2px 边框）
               caret     32px  ← 不是 24px。EP 的 `.el-dropdown--small .el-dropdown__caret-button
                                { width: 24px }` 命中不了：el-dropdown 根节点只挂
                                `el-dropdown` + `is-disabled`，不带 size 修饰类，
                                所以 size="small" 的 caret 实际吃的是基础档 32px。
               合计 42 + 32 − 1（button-group 的 -1px 负边距）= 73px
             .el-table .cell 是 padding:0 12px，可用内容宽 = 列宽 − 24。
             列宽 176 → 110（可用 86px，余量 13px，与环境变量页同口径）。
             最初改成 100 时余量只剩 3px，够是够，但把「详情」改成任何 3 字标签就会溢出，
             而 .cell 一溢出就会变成可滚动容器、点按钮时整行左移且不复位。 -->
        <el-table-column label="操作" width="110" fixed="right" align="center">
          <template #default="{ row }">
            <div class="action-btns">
              <DdSplitButton
                label="详情"
                type="primary"
                size="small"
                :items="depActionItems(row)"
                @click="viewLog(row)"
                @command="(key: string) => onDepAction(key, row)"
              />
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-bar">
      <span class="pagination-total">共 {{ depsTotal }} 条数据</span>
      <el-pagination
        v-model:current-page="depsPage"
        v-model:page-size="depsPageSize"
        :total="depsTotal"
        :page-sizes="[10, 20, 50, 100]"
        layout="sizes, prev, pager, next, jumper"
      />
    </div>
    <el-dialog
      v-model="showCreateDialog"
      title="新建依赖"
      width="500px"
      :fullscreen="dialogFullscreen"
    >
      <el-form label-width="80px">
        <el-form-item label="类型">
          <el-radio-group v-model="createType">
            <el-radio value="nodejs">Node.js</el-radio>
            <el-radio value="python">Python3</el-radio>
            <el-radio value="linux">Linux</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="createType === 'python'" label="版本">
          <el-alert
            :title="`会同步安装到当前镜像支持的 ${pythonRuntimeInstallSummary}；单版本镜像只会安装到当前小版本`"
            type="info"
            :closable="false"
            show-icon
          />
        </el-form-item>
        <!-- 系统包必须 root 才装得上。非 root 部署下不提前说，用户只能靠
             「先提交、再失败、再点详情、再读日志」才知道本来就装不了。 -->
        <el-form-item v-if="createType === 'linux'" label="说明">
          <el-alert
            :title="linuxCreateHint"
            type="warning"
            :closable="false"
            show-icon
          />
        </el-form-item>
        <el-form-item label="名称">
          <el-input
            v-model="createNames"
            type="textarea"
            :rows="5"
            :placeholder="createNamePlaceholder"
          />
        </el-form-item>
        <el-form-item label="自动拆分">
          <el-switch v-model="autoSplit" />
          <span
            style="
              margin-left: 8px;
              font-size: 12px;
              color: var(--el-text-color-secondary);
            "
            >开启后自动按换行、空格、逗号拆分为多个依赖</span
          >
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating"
          >安装</el-button
        >
      </template>
    </el-dialog>
    <el-dialog
      v-model="showLogDialog"
      title="安装日志"
      width="70%"
      :fullscreen="dialogFullscreen"
    >
      <div class="log-dialog-toolbar">
        <div class="log-dialog-status">
          <!-- 这个标签必须读依赖行的真实 status，不能读「日志流是否结束」。
               以前用 logDone 判定，导致日志流断开（例如长时间无输出）时明明还在装，
               却显示成绿色的「已完成」。 -->
          <!-- 弹窗开着的时候状态是由 SSE / 轮询实时推进的，这里是用户盯得最紧的一处，
               同样包 out-in。key 绑的是状态值而不是分支：
                 - 前两个分支之间（安装中 → 失败）跨分支切，会触发；
                 - 分支内部（排队中 → 安装中，两者都算 isProcessing）状态值变了也会触发，
                   靠分支切换的隐式 key 反而漏掉这一档。
               「已卸载」分支没有对应的 status（行已被后端删掉），单独给个静态 key。 -->
          <Transition name="dd-status-switch" mode="out-in">
            <el-tag v-if="logRowRemoved" key="removed" type="success" size="small"
              >已卸载</el-tag
            >
            <el-tag
              v-else-if="currentLogRow && isProcessing(currentLogRow.status)"
              :key="currentLogRow.status"
              type="warning"
              size="small"
              class="running-tag"
            >
              <LoadingMotion
                variant="dots"
                size="sm"
                tone="warning"
                :stacked="false"
              />
              <span>{{ statusLabel(currentLogRow.status) }}</span>
            </el-tag>
            <el-tag
              v-else-if="currentLogRow"
              :key="currentLogRow.status"
              :type="statusType(currentLogRow.status)"
              size="small"
              >{{ statusLabel(currentLogRow.status) }}</el-tag
            >
          </Transition>
          <el-tag v-if="logStreamNotice" type="info" size="small">{{
            logStreamNotice
          }}</el-tag>
        </div>
        <!-- 只有 installing / removing 能取消：服务端的 Cancel 接口对 queued 直接返回 400
             「当前依赖任务未在处理中」，把按钮显示出来只会让用户点了报错。 -->
        <el-button
          v-if="
            currentLogRow &&
            !logRowRemoved &&
            (currentLogRow.status === 'installing' ||
              currentLogRow.status === 'removing')
          "
          type="warning"
          plain
          size="small"
          @click="handleCancel(currentLogRow)"
        >
          取消当前任务
        </el-button>
      </div>
      <pre
        ref="logContainerRef"
        class="log-content dd-log-surface"
        v-html="logContentHtml"
      ></pre>
    </el-dialog>
    <el-dialog
      v-model="showMirrorDialog"
      title="软件包镜像源设置"
      width="560px"
      :fullscreen="dialogFullscreen"
    >
      <el-form label-width="110px" v-loading="mirrorLoading">
        <el-form-item label="Python (pip)">
          <el-input
            v-model="mirrorForm.pip_mirror"
            placeholder="留空恢复默认加速源"
            clearable
          >
            <template #append>
              <el-dropdown
                @command="(v: string) => (mirrorForm.pip_mirror = v)"
                trigger="click"
              >
                <el-button>快捷选择</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item
                      command="https://mirrors.aliyun.com/pypi/simple"
                      >阿里云 (默认)</el-dropdown-item
                    >
                    <el-dropdown-item
                      command="https://pypi.tuna.tsinghua.edu.cn/simple"
                      >清华大学</el-dropdown-item
                    >
                    <el-dropdown-item command="https://pypi.doubanio.com/simple"
                      >豆瓣</el-dropdown-item
                    >
                    <el-dropdown-item
                      command="https://mirrors.cloud.tencent.com/pypi/simple"
                      >腾讯云</el-dropdown-item
                    >
                    <el-dropdown-item
                      command="https://repo.huaweicloud.com/repository/pypi/simple"
                      >华为云</el-dropdown-item
                    >
                    <el-dropdown-item command=""
                      >恢复默认加速源</el-dropdown-item
                    >
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item label="Node.js (npm)">
          <el-input
            v-model="mirrorForm.npm_mirror"
            placeholder="留空恢复默认加速源"
            clearable
          >
            <template #append>
              <el-dropdown
                @command="(v: string) => (mirrorForm.npm_mirror = v)"
                trigger="click"
              >
                <el-button>快捷选择</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="https://registry.npmmirror.com"
                      >淘宝 (npmmirror)</el-dropdown-item
                    >
                    <el-dropdown-item
                      command="https://mirrors.cloud.tencent.com/npm/"
                      >腾讯云</el-dropdown-item
                    >
                    <el-dropdown-item
                      command="https://repo.huaweicloud.com/repository/npm/"
                      >华为云</el-dropdown-item
                    >
                    <el-dropdown-item command=""
                      >恢复默认加速源</el-dropdown-item
                    >
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item :label="linuxMirrorLabel">
          <el-input
            v-model="mirrorForm.linux_mirror"
            :placeholder="
              linuxMirrorSupported
                ? '留空恢复默认加速源'
                : '当前包管理器暂不支持镜像设置'
            "
            :disabled="!linuxMirrorSupported"
            clearable
          >
            <template #append>
              <el-dropdown
                @command="(v: string) => (mirrorForm.linux_mirror = v)"
                trigger="click"
                :disabled="
                  !linuxMirrorSupported || linuxMirrorOptions.length === 0
                "
              >
                <el-button
                  :disabled="
                    !linuxMirrorSupported || linuxMirrorOptions.length === 0
                  "
                  >快捷选择</el-button
                >
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item
                      v-for="option in linuxMirrorOptions"
                      :key="option.value"
                      :command="option.value"
                    >
                      {{ option.label }}
                    </el-dropdown-item>
                    <el-dropdown-item command=""
                      >恢复默认加速源</el-dropdown-item
                    >
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
          </el-input>
          <div class="mirror-hint">
            当前检测：{{ linuxMirrorManagerText }}
            <span v-if="linuxMirrorDistributionText">
              / {{ linuxMirrorDistributionText }}</span
            >
            <span v-if="linuxMirrorMessage">。{{ linuxMirrorMessage }}</span>
          </div>
        </el-form-item>
        <el-alert type="info" :closable="false" show-icon>
          依赖管理默认优先使用加速源；清空输入框并保存，会恢复到内置的默认加速源配置。
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="showMirrorDialog = false">取消</el-button>
        <el-button
          type="primary"
          @click="handleSaveMirrors"
          :loading="mirrorSaving"
          >保存</el-button
        >
      </template>
    </el-dialog>

    <!-- 系统命令行。入口放在依赖页是因为 issue #120 的用户正是在「装不上依赖」
         这个场景下提的需求；接口是 admin-only，所以整块按角色 gate 掉。 -->
    <SystemConsoleDialog
      v-if="isAdmin"
      v-model:visible="showConsoleDialog"
      :is-mobile="isMobile"
    />
  </div>
</template>

<script setup lang="ts">
import {
  ref,
  onMounted,
  onBeforeUnmount,
  onActivated,
  watch,
  computed,
} from "vue";
import {
  depsApi,
  type DepsFailedByType,
  type MirrorsResponse,
  type PythonRuntimeInfo,
} from "@/api/deps";
import {
  androidRuntimeApi,
  type AndroidRuntimeStatus,
  type AndroidRuntimePreset,
} from "@/api/androidRuntime";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  Box,
  Cpu,
  Delete,
  Download,
  Monitor,
  Plus,
  Refresh,
  RefreshRight,
  Search,
  Setting,
} from "@element-plus/icons-vue";
import DdSplitButton from "@/components/ui/DdSplitButton.vue";
import type { SplitButtonItem } from "@/components/ui/DdSplitButton.vue";
import DdBadge from "@/components/ui/DdBadge.vue";
import SystemConsoleDialog from "./components/SystemConsoleDialog.vue";
import {
  openAuthorizedEventStream,
  type EventStreamConnection,
} from "@/utils/sse";
import { usePageActivity } from "@/composables/usePageActivity";
import { useResponsive } from "@/composables/useResponsive";
import { useAuthStore } from "@/stores/auth";
import { useBadgesStore } from "@/stores/badges";
import { canAdminister } from "@/utils/roles";
import { ansiToHtml, normalizeAnsi } from "@/utils/ansi";
import { formatDateTime } from "@/utils/datetime";

const badgesStore = useBadgesStore();
const authStore = useAuthStore();
// 系统命令行那组接口是 JWTAuth + RequireAdmin 的管理员接口，非管理员点进去必然 403，
// 所以入口按角色隐藏（与订阅页里同一套判断）。
const isAdmin = computed(() => canAdminister(authStore.user?.role));
const showConsoleDialog = ref(false);

// ---------- Android 面具版脚本运行时 ----------
const androidStatus = ref<AndroidRuntimeStatus | null>(null);
const androidInstallingName = ref<string>("");
const androidInstallLog = ref<string[]>([]);
const androidInstallLogHtml = computed(() =>
  ansiToHtml(normalizeAnsi(androidInstallLog.value.join("\n"))),
);
let androidInstallAbort: AbortController | null = null;

async function loadAndroidStatus() {
  try {
    const res = await androidRuntimeApi.status();
    androidStatus.value = res.data;
  } catch (e) {
    androidStatus.value = null;
  }
}

function presetFor(name: string): AndroidRuntimePreset | undefined {
  return androidStatus.value?.presets?.find((p) => p.name === name);
}

async function installAndroidRuntime(name: string) {
  if (androidInstallingName.value) return;
  const preset = presetFor(name);
  if (!preset) {
    ElMessage.warning("当前架构没有预置下载源");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `将从 ${preset.url} 下载约 ${preset.size_mb}MB 并解压到 /data/adb/daidai-panel/bin/${name}，是否继续？`,
      "安装确认",
      { confirmButtonText: "开始安装", cancelButtonText: "取消" },
    );
  } catch {
    return;
  }

  androidInstallingName.value = name;
  androidInstallLog.value = [
    `[${new Date().toLocaleTimeString()}] 准备安装 ${name}...`,
  ];
  androidInstallAbort = new AbortController();

  try {
    const resp = await androidRuntimeApi.installStream(
      name,
      androidInstallAbort.signal,
    );
    if (!resp.ok) {
      const text = await resp.text();
      androidInstallLog.value.push(`HTTP ${resp.status}: ${text}`);
      ElMessage.error("安装失败: HTTP " + resp.status);
      return;
    }
    const reader = resp.body?.getReader();
    if (!reader) {
      ElMessage.error("无法建立流式连接");
      return;
    }
    const decoder = new TextDecoder();
    let buf = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let idx;
      while ((idx = buf.indexOf("\n\n")) >= 0) {
        const line = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        const m = line.match(/^data:\s?(.*)$/);
        if (m && m[1] !== undefined)
          androidInstallLog.value.push(m[1].replace(/\\n/g, "\n"));
      }
    }
    ElMessage.success(`${name} 安装完成`);
    await loadAndroidStatus();
  } catch (e: any) {
    if (e?.name !== "AbortError") {
      androidInstallLog.value.push("异常: " + (e?.message || String(e)));
      ElMessage.error(e?.message || "安装过程异常");
    }
  } finally {
    androidInstallingName.value = "";
    androidInstallAbort = null;
  }
}

async function uninstallAndroidRuntime(name: string) {
  try {
    await ElMessageBox.confirm(
      `确定移除 /data/adb/daidai-panel/bin/${name}？`,
      "确认",
      { type: "warning" },
    );
  } catch {
    return;
  }
  try {
    await androidRuntimeApi.uninstall(name);
    ElMessage.success("已移除");
    await loadAndroidStatus();
  } catch (e: any) {
    ElMessage.error("移除失败: " + (e?.message || String(e)));
  }
}
// ---------- /Android 面具版 ----------

const activeTab = ref("nodejs");
const pythonRuntimes = ref<PythonRuntimeInfo[]>([]);
const pythonDefaultVersion = ref("3.12");
const pythonVersion = ref("3.12");
const createPythonVersion = ref("3.12");
const pythonRuntimeInstallSummary = computed(() => {
  const labels = pythonRuntimes.value.map((item) => item.label || `Python ${item.version}`);
  return labels.length > 0 ? labels.join(" / ") : "Python 3.12";
});
const depsList = ref<any[]>([]);
/**
 * Node.js / Python3 / Linux 三个类型页签上各自的失败数，由 GET /deps 一并带回。
 *
 * 【为什么不在前端自己数】
 * depsList 只有【当前选中类型】（python 还只有当前版本）的依赖，数出来的失败数
 * 永远只是三分之一，与侧栏那个跨类型汇总的角标对不上——这正是用户看到「侧栏 9、
 * 页面失败 2」的原因。服务端那份是全量统计，且本页本来就在轮询这个接口，
 * 计数天然新鲜、零额外请求。
 */
const failedByType = ref<DepsFailedByType>({ nodejs: 0, python: 0, linux: 0 });
const loading = ref(false);
const showCreateDialog = ref(false);
const showLogDialog = ref(false);
const logContent = ref("");
const logContentHtml = computed(() =>
  ansiToHtml(normalizeAnsi(logContent.value || "暂无日志")),
);
// logDone 的语义是「日志流已结束」，不等于「任务已完成」。
// 任务是否完成一律看 currentLogRow.status。
const logDone = ref(true);
// 日志流意外断开（服务端硬超时、网络中断）时给用户的提示，避免用户以为任务停了。
const logStreamNotice = ref("");
// 依赖已被后端删除（卸载成功）。此时 currentLogRow 只是个查不到对应行的旧快照。
const logRowRemoved = ref(false);
const currentLogRow = ref<any | null>(null);
let eventSource: EventStreamConnection | null = null;
const logContainerRef = ref<HTMLElement>();
let depsLogBuffer: string[] = [];
let depsLogFlushRaf = 0;
const createType = ref("nodejs");
const createNames = ref("");
const autoSplit = ref(true);
const creating = ref(false);
const exporting = ref(false);
const selectedIds = ref<number[]>([]);
const selectedIdSet = computed(() => new Set(selectedIds.value));
const selectedRows = computed(() =>
  depsList.value.filter((dep) => selectedIdSet.value.has(dep.id)),
);
const batchReinstallRows = computed(() =>
  selectedRows.value.filter((dep) => !isProcessing(dep.status)),
);
const batchReinstallIds = computed(() =>
  batchReinstallRows.value.map((dep) => dep.id),
);

/**
 * 工具栏 Split Button 的菜单项。
 *
 * 主体是「新增依赖」（写在模板上），它只打开弹窗，点错了不产生任何副作用。
 * 「批量重装」是这一组里唯一会立刻对多行下手的写操作，用 divided 与三个
 * 只读/设置类操作隔开；它不是不可撤销操作，所以不标 danger。
 * 必须是 computed：刷新/导出的进行中状态和「有没有可重装的选中项」都会变。
 */
const toolbarActionItems = computed<SplitButtonItem[]>(() => [
  { key: "refresh", label: "刷新", icon: Refresh, disabled: loading.value },
  {
    key: "export",
    label: "导出清单",
    icon: Download,
    disabled: exporting.value,
  },
  { key: "mirror", label: "镜像源设置", icon: Setting },
  // 系统命令行只对管理员显示：接口本身是 admin-only，给 operator 显示出来只会点出 403
  {
    key: "console",
    label: "系统命令行",
    icon: Monitor,
    visible: isAdmin.value,
  },
  {
    key: "batch-reinstall",
    label: "批量重装",
    icon: RefreshRight,
    divided: true,
    disabled: batchReinstallIds.value.length === 0,
  },
]);

function onToolbarAction(key: string) {
  if (key === "refresh") loadData();
  else if (key === "export") handleExport();
  else if (key === "mirror") openMirrorDialog();
  else if (key === "console") showConsoleDialog.value = true;
  else if (key === "batch-reinstall") handleBatchReinstall();
}

/**
 * 操作列 Split Button 的菜单项，按行状态生成。
 *
 * 主体固定是「详情」——只打开日志弹窗，六种状态下都可用、点错零代价。
 * 「取消 / 重装」互斥：只有 installing / removing 能取消（服务端对 queued 的
 * Cancel 直接返回 400），所以用 visible 联动，绝不会出现「安装中的行菜单里
 * 同时挂着取消和重装」。queued 行仍然显示重装但禁用，与改造前一致。
 * 「卸载 / 强制卸载」不可撤销，只能待在菜单里并标红；divided 只加在危险组的
 * 第一项，用一条分隔线把这两项整体隔开——两项都加会在它们中间再画一条线，
 * 反而把同一组危险操作拆散。
 */
function depActionItems(row: any): SplitButtonItem[] {
  const cancellable = row.status === "installing" || row.status === "removing";
  const processing = isProcessing(row.status);
  return [
    { key: "cancel", label: "取消", visible: cancellable },
    {
      key: "reinstall",
      label: "重装",
      visible: !cancellable,
      disabled: processing,
    },
    {
      key: "delete",
      label: "卸载",
      danger: true,
      divided: true,
      disabled: processing,
    },
    {
      key: "force-delete",
      label: "强制卸载",
      danger: true,
      disabled: processing,
    },
  ];
}

function onDepAction(key: string, row: any) {
  if (key === "cancel") handleCancel(row);
  else if (key === "reinstall") handleReinstall(row);
  else if (key === "delete") handleDelete(row);
  else if (key === "force-delete") handleForceDelete(row);
}

let refreshTimer: ReturnType<typeof setInterval> | null = null;
const { isMobile, dialogFullscreen } = useResponsive();
const { isPageActive } = usePageActivity();

const showMirrorDialog = ref(false);
const mirrorLoading = ref(false);
const mirrorSaving = ref(false);
const mirrorForm = ref({ pip_mirror: "", npm_mirror: "", linux_mirror: "" });
const mirrorMeta = ref<MirrorsResponse>({
  pip_mirror: "",
  npm_mirror: "",
  linux_mirror: "",
  linux_package_manager: "",
  linux_distribution: "",
  linux_mirror_supported: false,
  linux_mirror_label: "Linux",
  linux_mirror_message: "",
});
let mounted = false;

const searchKeyword = ref("");
const statusFilter = ref("");

/**
 * 只套了搜索词、还没套状态筛选的列表。
 *
 * 状态标签页上的角标要数的就是它：点某个状态标签 = 在这份列表上再加一层状态过滤，
 * 所以角标数字和点进去看到的条数天然一致。
 * 如果角标改成直接数 depsList（无视搜索词），搜索状态下就会出现
 * 「失败 3」点进去却是空列表的自相矛盾。
 */
const searchScopedDeps = computed(() => {
  if (!searchKeyword.value) return depsList.value;
  const kw = searchKeyword.value.toLowerCase();
  return depsList.value.filter((dep) => dep.name?.toLowerCase().includes(kw));
});

const allCount = computed(() => searchScopedDeps.value.length);
const failedCount = computed(
  () => searchScopedDeps.value.filter((dep) => dep.status === "failed").length,
);
const installedCount = computed(
  () =>
    searchScopedDeps.value.filter((dep) => dep.status === "installed").length,
);

const filteredDepsList = computed(() => {
  const list = searchScopedDeps.value;
  if (!statusFilter.value) return list;
  return list.filter((dep) => dep.status === statusFilter.value);
});

const paginatedDepsList = computed(() => {
  const start = (depsPage.value - 1) * depsPageSize.value;
  return filteredDepsList.value.slice(start, start + depsPageSize.value);
});

const depsTotal = computed(() => filteredDepsList.value.length);
const depsPage = ref(1);
const depsPageSize = ref(20);

function resolveDisplayPythonVersion(
  runtimes: PythonRuntimeInfo[],
  defaultVersion: string,
) {
  if (runtimes.length === 0) {
    return defaultVersion || "3.12";
  }

  const defaultRuntime = runtimes.find((item) => item.version === defaultVersion);
  if (defaultRuntime?.available) {
    return defaultRuntime.version;
  }

  const firstAvailableRuntime = runtimes.find((item) => item.available);
  if (firstAvailableRuntime) {
    return firstAvailableRuntime.version;
  }

  const firstRuntime = runtimes[0];
  return defaultRuntime?.version || firstRuntime?.version || defaultVersion || "3.12";
}

function statusType(status: string) {
  switch (status) {
    case "queued":
      return "warning";
    case "installed":
      return "success";
    case "installing":
      return "warning";
    case "removing":
      return "warning";
    case "cancelled":
      return "info";
    case "failed":
      return "danger";
    default:
      return "info";
  }
}

function statusLabel(status: string) {
  switch (status) {
    case "queued":
      return "排队中";
    case "installed":
      return "已安装";
    case "installing":
      return "安装中";
    case "removing":
      return "卸载中";
    case "cancelled":
      return "已取消";
    case "failed":
      return "失败";
    default:
      return status;
  }
}

function isProcessing(status: string) {
  return (
    status === "queued" || status === "installing" || status === "removing"
  );
}

const hasPendingDeps = computed(() =>
  depsList.value.some((dep) => isProcessing(dep.status)),
);

watch([hasPendingDeps, isPageActive], () => {
  syncPendingRefresh();
});

const linuxMirrorLabel = computed(
  () => mirrorMeta.value.linux_mirror_label || "Linux",
);
const linuxMirrorSupported = computed(
  () => mirrorMeta.value.linux_mirror_supported,
);
const linuxMirrorMessage = computed(
  () => mirrorMeta.value.linux_mirror_message || "",
);
const linuxMirrorManagerText = computed(
  () => mirrorMeta.value.linux_package_manager || "未识别",
);
const linuxMirrorDistributionText = computed(
  () => mirrorMeta.value.linux_distribution || "",
);
const linuxMirrorOptions = computed(() => {
  const manager = mirrorMeta.value.linux_package_manager;
  const distro = mirrorMeta.value.linux_distribution;

  if (manager === "apk") {
    return [
      { label: "阿里云 (默认)", value: "https://mirrors.aliyun.com/alpine" },
      {
        label: "清华大学",
        value: "https://mirrors.tuna.tsinghua.edu.cn/alpine",
      },
      { label: "腾讯云", value: "https://mirrors.cloud.tencent.com/alpine" },
      { label: "华为云", value: "https://repo.huaweicloud.com/alpine" },
      { label: "中科大", value: "https://mirrors.ustc.edu.cn/alpine" },
    ];
  }

  if (manager === "apt") {
    if (distro === "debian") {
      return [
        {
          label: "阿里云 Debian (默认)",
          value: "https://mirrors.aliyun.com/debian",
        },
        {
          label: "清华大学 Debian",
          value: "https://mirrors.tuna.tsinghua.edu.cn/debian",
        },
        {
          label: "腾讯云 Debian",
          value: "https://mirrors.cloud.tencent.com/debian",
        },
      ];
    }
    return [
      {
        label: "阿里云 Ubuntu (默认)",
        value: "https://mirrors.aliyun.com/ubuntu",
      },
      {
        label: "清华大学 Ubuntu",
        value: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu",
      },
      {
        label: "腾讯云 Ubuntu",
        value: "https://mirrors.cloud.tencent.com/ubuntu",
      },
      { label: "华为云 Ubuntu", value: "https://repo.huaweicloud.com/ubuntu" },
    ];
  }

  return [];
});

/**
 * Linux 页签「安装编译工具链」要预填的包名。
 *
 * 名字按包管理器分：Alpine 的 build-base 在 Debian 系叫 build-essential，
 * RPM 系两边都不是。探测不到包管理器时返回空数组 —— 此时给任何包名都是错的，
 * 按钮会被禁用（见 linuxToolchainTip 说明原因）。
 * 走的是现成的 POST /deps，不新增接口，装出来的记录和手动新增的完全一样。
 */
const linuxToolchainPackages = computed<string[]>(() => {
  switch (mirrorMeta.value.linux_package_manager) {
    case "apk":
      return ["build-base", "linux-headers", "cmake"];
    case "apt":
      return ["build-essential", "cmake"];
    case "dnf":
    case "yum":
    case "microdnf":
    case "zypper":
      return ["gcc", "gcc-c++", "make", "cmake"];
    default:
      return [];
  }
});

const linuxToolchainTip = computed(() => {
  if (linuxToolchainPackages.value.length === 0) {
    return "未识别到系统包管理器，无法确定对应的工具链包名";
  }
  return `将预填：${linuxToolchainPackages.value.join(" ")}`;
});

/** 新建弹窗里 Linux 分支的前置提示：权限 + 当前包管理器 */
const linuxCreateHint = computed(() => {
  const manager = mirrorMeta.value.linux_package_manager;
  const managerText = manager
    ? `当前检测到的包管理器：${manager}`
    : "当前未识别到系统包管理器，提交后大概率直接失败";
  return `安装的是系统软件包，需要面板进程有 root 权限；非 root 部署会安装失败。${managerText}。`;
});

/** 名称输入框的占位文案。Linux 下给真实包名示例，通用文案对系统包没有任何提示作用 */
const createNamePlaceholder = computed(() => {
  if (createType.value !== "linux") {
    return "每行一个依赖名称，支持换行/空格/逗号分隔";
  }
  const manager = mirrorMeta.value.linux_package_manager;
  if (manager === "apk") {
    return "每行一个系统包名，例如：build-base linux-headers cmake";
  }
  if (manager === "apt") {
    return "每行一个系统包名，例如：build-essential cmake";
  }
  return "每行一个系统包名，例如：gcc make cmake";
});

function openLinuxToolchainInstall() {
  if (linuxToolchainPackages.value.length === 0) return;
  createType.value = "linux";
  createNames.value = linuxToolchainPackages.value.join("\n");
  // 预填的是多行包名，自动拆分必须开着，否则会被当成一个超长的包名提交
  autoSplit.value = true;
  showCreateDialog.value = true;
}

async function loadData() {
  loading.value = true;
  try {
    const res = await depsApi.list(
      activeTab.value,
      activeTab.value === "python" ? pythonVersion.value : undefined,
    );
    depsList.value = res.data || [];
    // 三个类型页签上的失败数由服务端一并带回（跨类型全量，python 跨所有版本）。
    // 老版本服务端没有这个字段，读不到就退回全 0，别把 undefined 渲染出去。
    failedByType.value = res.failed_by_type || { nodejs: 0, python: 0, linux: 0 };
    selectedIds.value = selectedIds.value.filter((id) =>
      depsList.value.some((dep) => dep.id === id),
    );
    syncCurrentLogRow();
    syncPendingRefresh();
  } catch {
    if (!refreshTimer) {
      depsList.value = [];
    }
    // failedByType 刻意不清零：网络抖一下就把「有 3 个失败」的提示抹掉，
    // 比不显示更糟——失败数保留上一次的值，只在请求成功时整体替换。
    syncPendingRefresh();
  } finally {
    loading.value = false;
  }
}

function stopRefreshTimer() {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
}

function syncPendingRefresh() {
  if (hasPendingDeps.value && isPageActive.value) {
    if (!refreshTimer) {
      refreshTimer = setInterval(() => {
        void loadData();
      }, 3000);
    }
    return;
  }
  stopRefreshTimer();
}

async function loadPythonRuntimes() {
  try {
    const res = await depsApi.pythonRuntimes();
    pythonRuntimes.value = res.data || [];
    pythonDefaultVersion.value = res.default_version || "3.12";
    pythonVersion.value = resolveDisplayPythonVersion(
      pythonRuntimes.value,
      pythonDefaultVersion.value,
    );
    createPythonVersion.value = pythonVersion.value;
  } catch {
    pythonRuntimes.value = [
      {
        version: "3.10",
        label: "Python 3.10",
        default: false,
        venv_path: "",
        venv_healthy: false,
        python_path: "",
        pip_path: "",
        available: false,
        message: "",
      },
      {
        version: "3.11",
        label: "Python 3.11",
        default: false,
        venv_path: "",
        venv_healthy: false,
        python_path: "",
        pip_path: "",
        available: false,
        message: "",
      },
      {
        version: "3.12",
        label: "Python 3.12",
        default: true,
        venv_path: "",
        venv_healthy: false,
        python_path: "",
        pip_path: "",
        available: false,
        message: "",
      },
    ];
    pythonDefaultVersion.value = "3.12";
    pythonVersion.value = resolveDisplayPythonVersion(
      pythonRuntimes.value,
      pythonDefaultVersion.value,
    );
    createPythonVersion.value = pythonVersion.value;
  }
}

function handlePythonVersionChange() {
  depsPage.value = 1;
  createPythonVersion.value = pythonVersion.value;
  void loadData();
}

async function setCurrentPythonDefault() {
  try {
    const res = await depsApi.setDefaultPythonRuntime(pythonVersion.value);
    pythonDefaultVersion.value = res.default_version || pythonVersion.value;
    pythonRuntimes.value = pythonRuntimes.value.map((item) => ({
      ...item,
      default: item.version === pythonDefaultVersion.value,
    }));
    pythonVersion.value = resolveDisplayPythonVersion(
      pythonRuntimes.value,
      pythonDefaultVersion.value,
    );
    createPythonVersion.value = pythonVersion.value;
    ElMessage.success("默认 Python 版本已更新");
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || "设置默认 Python 版本失败");
  }
}

function parseNames(text: string): string[] {
  if (!autoSplit.value) return [text.trim()].filter(Boolean);
  return text
    .split(/[\n,\s]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

async function handleCreate() {
  const names = parseNames(createNames.value);
  if (names.length === 0) {
    ElMessage.warning("请输入依赖名称");
    return;
  }
  creating.value = true;
  try {
    await depsApi.create(
      createType.value,
      names,
      createType.value === "python" ? createPythonVersion.value : undefined,
    );
    ElMessage.success(
      createType.value === "python"
        ? `已提交 ${names.length} 个依赖到 ${pythonRuntimes.value.length || 1} 个 Python 版本安装`
        : `已提交 ${names.length} 个依赖安装`,
    );
    showCreateDialog.value = false;
    createNames.value = "";
    activeTab.value = createType.value;
    if (activeTab.value === "python")
      pythonVersion.value = createPythonVersion.value;
    loadData();
  } catch {
    ElMessage.error("提交安装失败");
  } finally {
    creating.value = false;
  }
}

function handleSelectionChange(rows: any[]) {
  selectedIds.value = rows.map((r) => r.id);
}

function isSelected(id: number) {
  return selectedIdSet.value.has(id);
}

function toggleSelected(id: number, checked: boolean | string | number) {
  const next = new Set(selectedIds.value);
  if (checked) {
    next.add(id);
  } else {
    next.delete(id);
  }
  selectedIds.value = [...next];
}

async function handleBatchDelete() {
  if (selectedIds.value.length === 0) return;
  try {
    await ElMessageBox.confirm(
      `确定批量卸载选中的 ${selectedIds.value.length} 个依赖？`,
      "批量卸载",
      { type: "warning" },
    );
    await depsApi.batchDelete(selectedIds.value);
    ElMessage.success("批量卸载已提交");
    selectedIds.value = [];
    loadData();
  } catch (err: any) {
    if (err !== "cancel" && err?.toString() !== "cancel") {
      ElMessage.error(err?.response?.data?.error || "批量卸载失败");
    }
  }
}

async function handleBatchReinstall() {
  if (selectedIds.value.length === 0) return;
  if (batchReinstallIds.value.length === 0) {
    ElMessage.warning("选中的依赖当前都在处理中，暂时无法重装");
    return;
  }

  const skippedCount =
    selectedIds.value.length - batchReinstallIds.value.length;
  const skipHint =
    skippedCount > 0
      ? `\n其中 ${skippedCount} 个依赖正在处理中，已自动跳过。`
      : "";

  try {
    await ElMessageBox.confirm(
      `确定顺序重装选中的 ${batchReinstallIds.value.length} 个依赖吗？${skipHint}`,
      "批量重装",
      { type: "warning" },
    );
    await depsApi.batchReinstall(batchReinstallIds.value);
    ElMessage.success(
      `已提交 ${batchReinstallIds.value.length} 个依赖顺序重装`,
    );
    loadData();
  } catch (err: any) {
    if (err !== "cancel" && err?.toString() !== "cancel") {
      ElMessage.error(err?.response?.data?.error || "批量重装失败");
    }
  }
}

async function handleDelete(row: any) {
  try {
    await ElMessageBox.confirm(`确认卸载 ${row.name}？`, "提示", {
      type: "warning",
    });
  } catch {
    return;
  }
  try {
    await depsApi.delete(row.id);
    ElMessage.success("卸载中");
    loadData();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "卸载失败");
  }
}

async function handleForceDelete(row: any) {
  try {
    await ElMessageBox.confirm(
      `确认强制卸载 ${row.name}？\n强制卸载会跳过依赖检查直接删除`,
      "强制卸载",
      { type: "warning" },
    );
  } catch {
    return;
  }
  try {
    await depsApi.delete(row.id, true);
    ElMessage.success("强制卸载中");
    loadData();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "强制卸载失败");
  }
}

async function handleReinstall(row: any) {
  try {
    await depsApi.reinstall(row.id);
    ElMessage.success("重新安装中");
    loadData();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "操作失败");
  }
}

async function handleExport() {
  exporting.value = true;
  try {
    const blob = await depsApi.exportList(
      activeTab.value,
      activeTab.value === "python" ? pythonVersion.value : undefined,
    );
    const url = window.URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    const timestamp = new Date()
      .toISOString()
      .slice(0, 19)
      .replace(/[-:T]/g, "");
    anchor.href = url;
    const typeName =
      activeTab.value === "python"
        ? `${activeTab.value}-${pythonVersion.value.replace(".", "")}`
        : activeTab.value;
    anchor.download = `dependencies-${typeName}-${timestamp}.txt`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    window.URL.revokeObjectURL(url);
    ElMessage.success("依赖清单已导出");
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "导出失败");
  } finally {
    exporting.value = false;
  }
}

async function handleCancel(row: any) {
  try {
    await depsApi.cancel(row.id);
    ElMessage.success("取消请求已提交");
    loadData();
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || "取消失败");
  }
}

// currentLogRow 原本只是打开弹窗那一刻的快照，列表刷新后不会跟着变，
// 于是后端把状态改成 failed 时弹窗里还停在旧状态。这里在每次拉列表后回填。
function syncCurrentLogRow() {
  const current = currentLogRow.value;
  if (!current) {
    return;
  }
  const fresh = depsList.value.find((dep) => dep.id === current.id);
  if (fresh) {
    currentLogRow.value = fresh;
    return;
  }
  // 卸载成功后后端会直接删掉这一行（deleteOnSuccess），列表里再也找不到它。
  // 不识别这种情况的话，弹窗会一直停在快照里的「卸载中」，
  // 「取消当前任务」按钮也会常亮，点下去必然 404。
  // 只在日志流已经结束后才认定为已删除，避免切换 tab 造成的误判。
  if (logDone.value) {
    logRowRemoved.value = true;
  }
}

function viewLog(row: any) {
  currentLogRow.value = row;
  logContent.value = "";
  logStreamNotice.value = "";
  logRowRemoved.value = false;
  logDone.value = !(row.status === "installing" || row.status === "removing");
  showLogDialog.value = true;

  closeSSE();

  if (logDone.value) {
    depsApi
      .getStatus(row.id)
      .then((res) => {
        logContent.value = res.data?.log || "暂无日志";
      })
      .catch(() => {
        logContent.value = "获取日志失败";
      });
    return;
  }

  const url = `/api/v1/deps/${row.id}/log-stream`;
  eventSource = openAuthorizedEventStream(url, {
    onMessage(data) {
      depsLogBuffer.push(data);
      if (!depsLogFlushRaf) {
        depsLogFlushRaf = requestAnimationFrame(() => {
          logContent.value += depsLogBuffer.join("\n") + "\n";
          depsLogBuffer = [];
          depsLogFlushRaf = 0;
          if (logContainerRef.value) {
            logContainerRef.value.scrollTop =
              logContainerRef.value.scrollHeight;
          }
        });
      }
    },
    onEvent(event) {
      if (event.event !== "done") {
        return;
      }
      logDone.value = true;
      closeSSE();
      // data 携带的是结束原因：真实终态（installed/failed/...）表示任务确实结束了；
      // timeout 只代表服务端把这条日志流收了，任务本身可能还在跑，不能当成结束。
      if ((event.data || "").trim() === "timeout") {
        logStreamNotice.value = "日志流已断开，任务可能仍在进行";
      }
      loadData();
    },
    onError() {
      logDone.value = true;
      closeSSE();
      logStreamNotice.value = "日志流已断开，任务可能仍在进行";
      loadData();
    },
  });
}

function closeSSE() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
}

watch(showLogDialog, (val) => {
  if (!val) {
    closeSSE();
    currentLogRow.value = null;
    logStreamNotice.value = "";
    logRowRemoved.value = false;
  }
});

/**
 * 拉一次 GET /deps/mirrors 并写进 mirrorMeta，返回是否成功。
 *
 * 这份响应里除了三个镜像地址，还带着 linux_package_manager / linux_distribution /
 * linux_mirror_supported / linux_mirror_message —— 也就是「面板到底认出了 apk 还是 apt」。
 * 以前它只在打开镜像源弹窗时才拉，于是 Linux 页签和新建弹窗一个字都显示不出来。
 * 抽成函数后挂载时也调一次；镜像源弹窗仍然自己再拉一次拿最新值并回填表单，行为不变。
 */
async function loadMirrorMeta(): Promise<boolean> {
  try {
    mirrorMeta.value = await depsApi.getMirrors();
    return true;
  } catch {
    // 失败时保留上一次的值（初始值是全空 + 未识别），页面说明会退化成「未识别」，不弹错
    return false;
  }
}

async function openMirrorDialog() {
  showMirrorDialog.value = true;
  mirrorLoading.value = true;
  if (await loadMirrorMeta()) {
    mirrorForm.value.pip_mirror = mirrorMeta.value.pip_mirror || "";
    mirrorForm.value.npm_mirror = mirrorMeta.value.npm_mirror || "";
    mirrorForm.value.linux_mirror = mirrorMeta.value.linux_mirror || "";
  } else {
    ElMessage.error("获取镜像源配置失败");
  }
  mirrorLoading.value = false;
}

async function handleSaveMirrors() {
  if (!linuxMirrorSupported.value && mirrorForm.value.linux_mirror.trim()) {
    ElMessage.warning(
      linuxMirrorMessage.value || "当前系统暂不支持 Linux 镜像设置",
    );
    return;
  }
  mirrorSaving.value = true;
  try {
    await depsApi.setMirrors(mirrorForm.value);
    ElMessage.success("镜像源设置成功");
    showMirrorDialog.value = false;
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || "设置失败");
  } finally {
    mirrorSaving.value = false;
  }
}

const letterColors: Record<string, string> = {
  a: "#409eff",
  b: "#67c23a",
  c: "#e6a23c",
  d: "#67c23a",
  e: "#f56c6c",
  f: "#909399",
  g: "#2f7df6",
  h: "#36cfc9",
  i: "#409eff",
  j: "#0ea5e9",
  k: "#ffc53d",
  l: "#10b981",
  m: "#e6a23c",
  n: "#409eff",
  o: "#36cfc9",
  p: "#67c23a",
  q: "#f56c6c",
  r: "#06b6d4",
  s: "#ffc53d",
  t: "#409eff",
  u: "#22c55e",
  v: "#36cfc9",
  w: "#e6a23c",
  x: "#909399",
  y: "#67c23a",
  z: "#f56c6c",
};
function getLetterColor(name: string): string {
  const ch = (name || "?").charAt(0).toLowerCase();
  return letterColors[ch] || "#409eff";
}

onMounted(async () => {
  mounted = true;
  createType.value = activeTab.value;
  // 进页即把侧栏的「依赖管理」失败角标标记为已读——用户已经站在这一页上了，再红着没有意义
  badgesStore.ackDepsFailed();
  await loadPythonRuntimes();
  createPythonVersion.value = pythonVersion.value || pythonDefaultVersion.value;
  loadData();
  loadAndroidStatus();
  // Linux 页签的说明、新建弹窗的提示与 placeholder 都要靠这份元数据，
  // 不能再等到用户打开镜像源弹窗才拉。失败也不弹错，页面自己退化成「未识别」。
  void loadMirrorMeta();
});

onActivated(() => {
  // 角标清零刻意放在下面那道 mounted 闸【外面】、且无条件执行：
  // 那道闸是给 loadData 防重复请求用的（onMounted 刚拉过一次），
  // 而 MainLayout 的 keep-alive 是 :max="14"，第二次以后进本页只触发 onActivated、
  // 不再触发 onMounted，写进 if 里就只有首次访问才会清零。
  badgesStore.ackDepsFailed();
  if (!mounted) {
    void loadData();
  }
  mounted = false;
});

onBeforeUnmount(() => {
  closeSSE();
  stopRefreshTimer();
  if (depsLogFlushRaf) {
    cancelAnimationFrame(depsLogFlushRaf);
    depsLogFlushRaf = 0;
  }
});
</script>

<style scoped lang="scss">
.deps-page {
  padding: 0;
  overflow-x: hidden;
}

.page-header {
  margin-bottom: 18px;

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
    margin: 6px 0 0;
    line-height: 1.6;
    max-width: 720px;
  }
}

.page-title-with-icon {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.page-title-with-icon :deep(.el-icon) {
  color: var(--el-color-primary);
}

// ---------- Toolbar ----------
// 工具条：与定时任务页/执行日志页/订阅页/环境变量页对齐——上下统一间距、左右两区一行排布、gap 一致；
// 本页工具条元素较多（版本选择/搜索/状态过滤），保持各自业务所需宽度，行内间距统一到 12px。
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 14px 0;
  gap: 12px;
  flex-wrap: wrap;
  &__left {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  &__right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  &__search {
    width: 240px;
  }
  &__filter {
    width: 140px;
  }
  &__python-version {
    width: 150px;
  }
}

.python-runtime-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.python-runtime-hint {
  margin-bottom: 14px;
}

.python-runtime-hint__body {
  font-size: 13px;
  line-height: 1.6;
}

.python-runtime-hint__status {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

// Linux 页签说明块。位置与观感完全对齐上面的 Python 说明块，
// 只多一行「安装编译工具链」的操作区。
.linux-runtime-hint {
  margin-bottom: 14px;
}

.linux-runtime-hint__body {
  font-size: 13px;
  line-height: 1.6;
}

.linux-runtime-hint__actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 8px;
}

.linux-runtime-hint__tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

// ---------- Table Card ----------
// 表格卡：无阴影，仅用 1px 边框与页面底色区分；本页是滚动页（dd-scroll-page），不做 fixed 高度链处理。
.table-card {
  background: var(--el-bg-color);
  // 表格容器属容器类表面 → surface 档；overflow:hidden 让内部贴边的表头/行自动被圆角裁角
  border-radius: var(--dd-radius-surface);
  border: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
}

.dep-name-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

// 依赖名首字头像：形状承载语义（圆形=头像/身份标识），两种圆角模式下都固定正圆，不吃 --dd-radius-* 令牌
.dep-name-avatar {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  font-size: 11px;
  font-weight: 700;
  color: #fff;
}

.deps-tabs {
  margin-bottom: 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

// 状态分段控件：与定时任务页/执行日志页/订阅页/环境变量页一致的分段容器 + 选中态白底品牌色 + 1px 边框。
// 本页有两组：①运行时切换（Node/Python3/Linux）②状态筛选（.status-tabs--filter，含已安装/失败计数），
// 共用同一套观感，使两组视觉统一。
.status-tabs {
  display: inline-flex;
  background: var(--el-fill-color-light);
  // 分段控件的灰底槽属控件类表面 → control 档（与槽内的项同档，两者一致才不会露出内外错位的角）
  border-radius: var(--dd-radius-control);
  padding: 3px;
  gap: 2px;
}
.status-tab {
  padding: 6px 14px;
  // 分段项属控件类表面 → control 档
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
  display: inline-flex;
  align-items: center;
  gap: 5px;
  &:hover {
    color: var(--el-text-color-primary);
  }
  &.active {
    background: var(--el-bg-color);
    color: var(--el-color-primary);
    border-color: var(--el-border-color-lighter);
    font-weight: 600;
  }
  // 成功/失败筛选选中态用语义色，标出「这一档筛的是哪类结果」。
  // 语义色只上在标签文字上，角标本身保持中性——见下方说明。
  &--success.active {
    color: var(--el-color-success);
  }
  &--danger.active {
    color: var(--el-color-danger);
  }
}
// 计数徽标改用 DdBadge（level="info" + show-zero），原来手写的 .status-tab__count 删除。
// 两点变化，都是有意的：
//  1. 统一到全站唯一的角标实现，顺带白拿翻牌动效——本页角标会随 3s 轮询跳字
//     （安装完成时「已安装」+1、「失败」可能同时 +1），跳字比静默换数字更能被注意到。
//  2. 放弃原来「选中态反白成 success/danger 实心」的处理。设计系统里 danger 实心是
//     留给「需要用户处理」的，这三个角标只是中性计数；一个实心红的「失败 0」会和
//     真正要处理的告警抢注意力。现在筛选态由标签文字的语义色表达，角标始终中性描边。
.dep-name-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
  color: var(--el-text-color-primary);
}
// .version-text 随「版本」列一起删除（模型里没有 version 字段，见模板处注释）
.time-text {
  font-family: var(--dd-font-mono);
  font-size: 12px;
  color: var(--el-text-color-regular);
}
// 操作列按钮组。
// 尺寸口径变更：原来这里把按钮压到 height:26px / padding:0 5px / font-size:12px，
// 比全站其它页（定时任务页/执行日志页/订阅页的 padding:4px 8px）更紧一档——那是为了在
// 176px 列宽里硬塞下「详情 + 取消/重装 + 更多▾」三个按钮。现在整列只剩一个 Split Button，
// 不需要再压，统一回其它页的 padding: 4px 8px；列宽估算（详情 40px + caret 24px）也按这个口径算。
// .action-more-btn（手写 caret 的 gap）和 .danger-dropdown-text（手写红色强制卸载）
// 随手工版一起删除：前者由 DdSplitButton 自带的 caret 半边取代，后者由 item.danger 取代。
.action-btns {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  min-width: 0;

  // caret 半边保持 EP 自己的窄内边距（--small 档约 24px 宽），
  // 一起套 8px 会把它撑宽，列宽余量随之失准。
  :deep(.el-button:not(.el-dropdown__caret-button)) {
    padding: 4px 8px;
  }

  // EP 自带 `.el-button + .el-button { margin-left: 12px }`，split button 的两个半边
  // 正是相邻的 el-button，不清零会在按钮组中间裂开一道 12px。
  :deep(.el-button + .el-button) {
    margin-left: 0;
  }
}

// ---------- Pagination ----------
// 分页条：与定时任务页/执行日志页/订阅页/环境变量页一致的间距收敛（margin-top 14px）
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
    padding: 8px 0;
  }
  .el-table__fixed-right .el-table__cell {
    padding-left: 4px;
    padding-right: 4px;
  }
}

// ---------- Mobile card layout ----------
.deps-card__title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.deps-card__actions > * {
  flex: 1 1 calc(50% - 4px);
}

// ---------- Log dialog ----------
.log-content {
  // 安装日志面板属容器类表面 → surface 档（弹窗 body 有内边距，不贴边，不会露角）
  border-radius: var(--dd-radius-surface);
  padding: 16px;
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.6;
  min-height: 200px;
  max-height: 60vh;
  overflow-y: auto;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}

.log-dialog-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.log-dialog-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.mirror-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
  margin-top: 6px;
}

.running-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

// ===== 状态标签切换过渡 =====
// 表格状态列 / 移动卡片 / 日志弹窗共用。
// 只过渡 opacity：状态标签在表格行里，做位移会连带整行一起晃；
// 做缩放又违反「hover/active 禁 transform 位移或缩放」的同一条扁平约束。
// 时长/缓动全走令牌，prefers-reduced-motion 下令牌自动降到 1ms，等效关闭。
.dd-status-switch-enter-active,
.dd-status-switch-leave-active {
  transition: opacity var(--dd-motion-fast) var(--dd-ease-standard);
}

.dd-status-switch-enter-from,
.dd-status-switch-leave-to {
  opacity: 0;
}

// ===== 批量操作按钮进出场 =====
// 同样只做 opacity，不碰宽高——工具条是 flex 行，尺寸类过渡会让整条工具条持续重排。
.dd-batch-fade-enter-active,
.dd-batch-fade-leave-active {
  transition: opacity var(--dd-motion-fast) var(--dd-ease-standard);
}

.dd-batch-fade-enter-from,
.dd-batch-fade-leave-to {
  opacity: 0;
}

// ---------- Responsive ----------

@media screen and (max-height: 720px) and (min-width: 769px) {
  .android-runtime-card {
    margin-bottom: 12px;

    :deep(.el-card__header) {
      padding: 12px 16px;
    }

    :deep(.el-card__body) {
      padding: 12px 16px;
    }
  }

  .android-runtime-tip {
    margin-bottom: 8px;
  }

  .android-runtime-grid {
    margin-top: 6px;
  }

  .runtime-item {
    padding: 10px 12px;
    margin-bottom: 8px;
  }

  .android-runtime-log pre {
    max-height: 140px;
  }

  .deps-tabs,
  .toolbar {
    margin-bottom: 10px;
  }

  .pagination-bar {
    margin-top: 12px;
  }
}

@media (max-width: 768px) {
  .page-header {
    margin-bottom: 14px;
    h2 {
      font-size: 18px;
    }
  }
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    // 原来是「5 个按钮排两列网格 + 每个按钮 width:100%」。现在左区只剩一个 Split Button，
    // 两列网格会把它压成半行宽；而 `:deep(.el-button){width:100%}` 会让它的主体与 caret
    // 两个半边各自撑到按钮组的 100%，直接溢出。改回单行 flex，按钮取自身自然宽度。
    &__left {
      width: 100%;
      display: flex;
      gap: 8px;
    }
    &__right {
      flex-direction: column;
      gap: 10px;
    }
    &__search {
      width: 100% !important;
    }
    &__filter {
      width: 100% !important;
    }
  }

  .deps-card__title-row {
    flex-direction: column;
  }

  .pagination-bar {
    flex-direction: column;
    gap: 10px;
    align-items: center;
  }
}

// ---------- Android Runtime ----------
.android-runtime-card {
  margin-bottom: 16px;
  border: 1px solid var(--el-border-color-lighter);
}
.android-runtime-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.android-runtime-header .el-icon {
  vertical-align: middle;
  margin-right: 6px;
}
.android-runtime-meta {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  display: flex;
  gap: 8px;
  align-items: center;
}
.android-runtime-tip {
  margin-bottom: 12px;
}
.android-runtime-grid {
  margin-top: 8px;
}
.runtime-item {
  border: 1px solid var(--el-border-color-lighter);
  // 运行时条目是弹窗内的列表卡片，属容器类表面 → surface 档
  border-radius: var(--dd-radius-surface);
  padding: 12px 14px;
  margin-bottom: 12px;
  background: var(--el-fill-color-lighter);
}
.runtime-item__head {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 6px;
}
.runtime-item__meta {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}
.runtime-item__note {
  color: var(--el-color-warning);
  margin-top: 4px;
}
.runtime-item__actions {
  margin-top: 10px;
  display: flex;
  gap: 8px;
}
.android-runtime-log {
  margin-top: 12px;
  border-top: 1px dashed var(--el-border-color-lighter);
  padding-top: 10px;
}
.android-runtime-log__title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}
.android-runtime-log pre {
  background: var(--el-fill-color);
  // 运行时安装日志代码块属容器类表面 → surface 档（上方有虚线分隔+padding-top，不贴边）
  border-radius: var(--dd-radius-surface);
  padding: 10px 12px;
  font-size: 12px;
  max-height: 240px;
  overflow: auto;
  margin: 0;
}

// ===== 入场动画 =====
// 与定时任务页/执行日志页/订阅页/环境变量页统一：只对卡片级容器
// （Android 运行时卡 / 状态标签区 / 工具条 / 表格卡 / 移动列表）
// 做克制的淡入上移 + 轻微错落；不给表格每一行或每张移动卡做 stagger。
// 时长走令牌，prefers-reduced-motion 时令牌自动降为 1ms 即等效关闭。
@keyframes dd-deps-rise-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

// android-runtime-card 也是卡片级容器，而且是页面最上面那一块：原来它不参与入场，
// 下面几块却都在淡入上移，视觉上像是「上半页没加载完」。
// 它是异步的（loadAndroidStatus 返回后才 v-if 成立），补上入场后从「突然弹出」变成淡入。
.android-runtime-card,
.deps-tabs,
.toolbar,
.table-card,
.dd-mobile-list {
  animation: dd-deps-rise-in var(--dd-motion-page) var(--dd-ease-decelerate) both;
}

// 轻微错落：状态标签区/工具条先入，表格卡/移动列表略晚
.table-card,
.dd-mobile-list {
  animation-delay: 60ms;
}
</style>
