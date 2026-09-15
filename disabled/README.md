# 暂时下线的条目

这里的 JSON 片段**不在** `fndepot.json` / `fnpack.json` 索引里，源客户端不会加载，仅作为原文暂存以便整改后原样恢复。

## kms-activator（v1.0.0，2026-09-15 下线）

下线原因（详见提交说明）：

- 打包内的 `vlmcsd` 为 stripped 的 `private build`，无源码、无构建配方、无可核验的上游 tag/checksum
- 署名指向的账号名下并无该应用仓库，真实上游为 vlmcsd（SystemInflux，上游仓库已下架）
- 非 root 运行下 1688→11688 端口转发无法生效，而界面给出的 `slmgr /skms <IP>` 命令默认打 1688，主流程预计不可用
- 卸载路径不清理 nat 规则、`vlmcsd.kmd` 密钥库文件实际缺失等若干实现缺陷
- KMS 激活工具在公开应用索引中分发存在条款与政策风险

## 恢复方法

1. 把 `kms-activator.json` 的内容原样插回 `fndepot.json` 与 `fnpack.json` 顶层（两个文件必须保持字节一致）
2. 校验：`python3 -c "import json;print(len(json.load(open('fndepot.json'))))"` 应为 10
3. 或直接回退本次下线提交

> 恢复前需完成：可核验的二进制构建脚本与 SHA256、真实署名、端口与卸载路径修复、`platform` 与图标槽位规范。
