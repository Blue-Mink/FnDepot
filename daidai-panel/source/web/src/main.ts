import { createApp } from "vue";
import { createPinia } from "pinia";
import { ElMessageBox, provideGlobalConfig } from "element-plus";
import zhCn from "element-plus/es/locale/lang/zh-cn";
import "element-plus/theme-chalk/dark/css-vars.css";
import "element-plus/theme-chalk/el-loading.css";
import "element-plus/theme-chalk/el-message.css";
import "element-plus/theme-chalk/el-message-box.css";
import {
  ArrowLeft,
  ArrowRight,
  Bell,
  Box,
  Check,
  CircleCheck,
  CircleCheckFilled,
  CircleClose,
  Clock,
  Close,
  Connection,
  CopyDocument,
  Delete,
  Document,
  DocumentAdd,
  DocumentCopy,
  Download,
  Edit,
  Expand,
  Fold,
  Folder,
  FolderAdd,
  Hide,
  InfoFilled,
  Key,
  Lock,
  Menu,
  Monitor,
  Moon,
  More,
  MoreFilled,
  Odometer,
  Operation,
  Plus,
  Rank,
  Refresh,
  RefreshRight,
  Search,
  Setting,
  SetUp,
  Sort,
  Star,
  Sunny,
  Tickets,
  Timer,
  Top,
  Unlock,
  Upload,
  User,
  UserFilled,
  VideoPause,
  VideoPlay,
  View,
} from "@element-plus/icons-vue";
import App from "./App.vue";
import LoadingMotion from "./components/LoadingMotion.vue";
import router from "./router";
import {
  applyPanelShapeStyle,
  fetchAndApplyPanelAppearance,
} from "./utils/panelAppearance";
import "./styles/global.scss";
import "./styles/animations.css";
// visual-enhancements.css 已删除（v3.0.8）：209 行 / 22 个工具类【全部零引用】，
// 逐个 grep 确认过。唯一看似有引用的 `.status-dot` 是 api-docs 页自己在 scoped 里
// 定义的同名类（且属性更全、取色走令牌），删掉全局那份不影响它。
//
// 删掉的另一个理由是它在留着当隐患：里面还有 `backdrop-filter: blur(10px)` 的玻璃拟态
// （.glass-card / .btn-glass）、`transition: all 0.3s ease` 这种写死时长的过渡，
// 以及 #67c23a / #f56c6c 这类硬编码语义色（暗色下不跟随）——
// 全都违反「全屏纯扁平直角」的硬规则，谁顺手复用一个类名就等于引入一处违规。

// Edge / Chromium 在窗口最小化后，如果弹窗、编辑器或第三方组件延迟调用 focus()，
// 可能会把已经最小化的浏览器窗口重新拉回前台。面板后台不可见时不需要抢焦点，
// 所以统一拦截后台状态下的程序化聚焦，避免用户点击最小化后窗口又闪回。
const daidaiWindow = window as Window & {
  __DAIDAI_SAFE_FOCUS_PATCHED__?: boolean;
};

if (!daidaiWindow.__DAIDAI_SAFE_FOCUS_PATCHED__) {
  daidaiWindow.__DAIDAI_SAFE_FOCUS_PATCHED__ = true;
  const rawHTMLElementFocus = HTMLElement.prototype.focus;

  HTMLElement.prototype.focus = function safeFocus(
    this: HTMLElement,
    options?: FocusOptions,
  ) {
    if (document.visibilityState === "hidden" || !document.hasFocus()) {
      return;
    }

    rawHTMLElementFocus.call(this, options);
  };
}

const globalIcons = {
  ArrowLeft,
  ArrowRight,
  Bell,
  Box,
  Check,
  CircleCheck,
  CircleCheckFilled,
  CircleClose,
  Clock,
  Close,
  Connection,
  CopyDocument,
  Delete,
  Document,
  DocumentAdd,
  DocumentCopy,
  Download,
  Edit,
  Expand,
  Fold,
  Folder,
  FolderAdd,
  Hide,
  InfoFilled,
  Key,
  Lock,
  Menu,
  Monitor,
  Moon,
  More,
  MoreFilled,
  Odometer,
  Operation,
  Plus,
  Rank,
  Refresh,
  RefreshRight,
  Search,
  Setting,
  SetUp,
  Sort,
  Star,
  Sunny,
  Tickets,
  Timer,
  Top,
  Unlock,
  Upload,
  User,
  UserFilled,
  VideoPause,
  VideoPlay,
  View,
};

// 整个启动流程必须是异步的，唯一原因是：演示环境的 mock 层要抢在
// 【app.use(router)】之前装到 axios 上，而它只能靠动态 import() 加载（见下方 D2 说明）。
//
// ⚠️ 触发首次导航的是 app.use(router)，不是 app.mount()。
//    vue-router 的 install() 里直接就是 `push(routerHistory.location)`
//    （node_modules/vue-router/dist/vue-router.mjs:1502-1507，同步执行），
//    所以 app.use(router) 一执行，router.beforeEach 就跑起来了：
//    已登录状态下刷新页面时 authStore.user 是空的，守卫会 await fetchUser()
//    发出 GET /api/auth/user；这一发晚一步被接管，就会 404 → clearAuth() → 打回登录页。
//    （首次访问看不出来：守卫更早的 `!isLoggedIn` 分支会先短路掉，走不到 fetchUser。）
//    ⇒ 只把 mount 推迟到 demo 加载完是【不够】的，必须连 app.use(router) 一起推迟。
//    改动这里前请先确认这条时序，不要退回「装 mock 与建 app 并行」的写法。
async function bootstrap() {
  // 首屏防闪形：圆角风格来自异步的 panel-settings，首帧一定先按 :root 的直角画一遍，
  // 圆角用户会看到「先方后圆」的形状跳变 —— 比闪色刺眼得多。
  // 这里先用 localStorage 里上次记住的值【同步】写一遍三条 --dd-radius-*，
  // 等下面那发请求回来 applyPanelAppearance() 再以服务端值重写（值一致时什么都不会变）。
  //
  // ⚠️ 必须放在下面 demo 的 await import() 之前：演示版构建里那一 await 会让出主线程，
  //    浏览器很可能已经把首帧画出去了，放在它后面就等于没做。
  applyPanelShapeStyle();

  // 这段刻意写成「编译期常量守卫 + 动态 import()」：
  // VITE_DEMO 在发布版构建里被 define 成 ''（见 vite.config.ts），条件恒假，
  // rollup 会把整个 if 分支连同 web/src/demo/** 的 chunk 一起剔除——
  // 发布版产物必须 0 字节 demo 代码，真实面板的请求绝不能被 mock 顶替。
  // 不要改成静态 import，也不要把条件换成运行期判断（那样无法做死代码消除）。
  if (import.meta.env.VITE_DEMO === "1") {
    try {
      const { installDemo } = await import("./demo");
      installDemo();
    } catch (error) {
      // demo chunk 拉取失败（CDN 抖动、缓存过期后旧 hash 404）时也要继续把 app 挂上去。
      // 少了这个 catch，异常会中断 bootstrap，访客看到的是纯白页面，
      // 连「哪里出错了」都无从判断。兜住之后至少还能看到登录页。
      console.error("[demo] mock 层加载失败，演示环境将无法正常工作:", error);
    }
  }

  // ⚠️ 别被位置误导：把这一行挪到 demo 安装前后，【都改变不了】那一发网络请求的时机。
  //    真正发请求的是 utils/panelSettings.ts 的裸 fetch('/api/system/panel-settings')，
  //    而它早在 `import router from "./router"` 这个静态 import 求值时就被打出去了
  //    （router/index.ts 顶层有一句 `void loadPanelSettings()`），
  //    也就是说它先于 bootstrap() 的函数体、更先于 installDemo()。
  //    loadPanelSettings() 内部对 promise 做了记忆化，这里只是复用同一发的结果。
  //    ⇒ 想让演示站不发这一枪，唯一的办法是按 design.md C4 在 panelSettings.ts 内部短路，
  //      而不是在这里调顺序。（目前它在演示站上会 404，被那边的空 catch 吞掉，不影响挂载。）
  //    留在 createApp 之前的理由只有一个：外观 CSS 变量尽早写进 documentElement，减少首屏闪色。
  void fetchAndApplyPanelAppearance();

  const app = createApp(App);

  // Element Plus 的 locale 走 provide/inject，取不到就回落英文默认包
  // （es/hooks/use-locale/index.mjs:17-18，`locale.value || en_default`），
  // 表现为确认框「Cancel / OK」、表格空态「No Data」、分页器「20/page」。
  //
  // ① provideGlobalConfig({ locale: zhCn }, app, true) —— 【唯一的真开关】。
  //    删掉它全站立刻回英文，而且构建全绿、控制台无声，属于典型的静默回归。
  //    传了 app ⇒ 走 app.provide 做【app 级】provide，组件树里的 el-table /
  //    el-pagination 才 inject 得到；第三个参数 global=true 另外把这份配置写进
  //    element-plus 的模块级 globalConfig（es/…/config-provider/src/hooks/use-global-config.mjs:54）。
  //    它与 app.use(ElementPlus, { locale }) 内部做的是同一件事（es/make-installer.mjs:11），
  //    但不会把整包组件拉进来，按需引入的体积优化保持不变。
  //
  // ② app.use(ElMessageBox) —— 【加固项，不是必需项】。
  //    别把它读成「缺了确认框就是英文」：ElMessageBox 虽然是命令式 API，用 render()
  //    脱离组件树挂载、vnode.appContext 默认为 null（es/…/message-box/src/messageBox.mjs:114），
  //    但它取 locale 的路径是 useGlobalComponentSettings -> useGlobalConfig ->
  //    inject(configProviderContextKey, globalConfig)（use-global-config.mjs:14）。
  //    appContext 为 null 时 Vue 的 inject 取不到 provides，会走「有默认值就返回默认值」
  //    那条分支（runtime-core inject: instance.parent == null -> provides 取自
  //    vnode.appContext，为 null 则 return defaultValue），返回的正是 ① 写好的
  //    那个模块级 globalConfig。⇒ 只有 ① 时确认框【已经】是中文。
  //    保留 ② 的理由：把 _context 指到 app._context，让它走真正的 app 级 provide，
  //    而不是长期依赖「inject 默认参数兜底」这个 EP 内部实现细节
  //    （哪天那个默认值变了，靠兜底的写法会静默失效）。
  //
  // ⚠️ 想加 CI 门禁守这件事时注意：能被产物断言守住的只有 ①（删了它 zhCn 这个 import
  //    就没人用，element-plus 的 sideEffects 不含 es/locale/**，rollup 会把整份中文包
  //    剔掉，产物里就搜不到 `共 {total} 条` 了）。英文包 en.mjs 被 use-locale 静态 import，
  //    两种情况下都在产物里，所以任何拿英文串做判据的断言都是恒绿的空转门禁。
  provideGlobalConfig({ locale: zhCn }, app, true);
  app.use(ElMessageBox);

  app.use(createPinia());
  // ↓ 首次导航在这一行触发，此时 demo adapter 已经就位
  app.use(router);
  app.component("LoadingMotion", LoadingMotion);

  for (const [key, component] of Object.entries(globalIcons)) {
    app.component(key, component);
  }

  app.mount("#app");
}

void bootstrap();
