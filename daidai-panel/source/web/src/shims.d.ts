declare module 'sortablejs'

/**
 * 补充 Vite 注入的环境变量类型。
 * vite/client 里 ImportMetaEnv 带索引签名（值是 any），这里显式声明一遍，
 * 让「有哪些自定义变量、各自什么含义」在类型层面可见。
 */
interface ImportMetaEnv {
  /** '1' 表示这是在线演示 Demo 构建（vite build --mode demo，见 web/.env.demo）；发布版恒为 '' */
  readonly VITE_DEMO?: string
  /** 演示站展示的面板版本号，由 Pages 部署流程注入（发布 tag 去掉前缀 v）；本地构建为空 */
  readonly VITE_DEMO_VERSION?: string
}
