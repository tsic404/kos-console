# kos-console

KOS 系统应用项目：浏览器维护终端。作为独立扩展包装载，经控制面建立、接入与结束节点或 Pod 容器会话；执行、凭据隔离与审计归控制面。

- 设计：`tsic/kos-design` 仓的 `kos-console/`（范围、契约、验收、决策记录）

## 现有实现

| 路径 | 职责 |
|---|---|
| `app/` | 扩展声明与原生「终端」页面注册、目标读取面与页面条目读取 |
| `terminal/` | 控制面会话客户端与令牌状态机：创建、接入、输入、续期、结束与审计读取 |
| `cmd/package/`、`internal/artifact/` | 经 kos-sdk 打包为独立扩展包并自验摘要 |
| `assets/` | 终端页面文档与 UI 资产：入口文档、脚本、样式与图标 |
| `assets/api.js` | 控制面路径：页面清单、页面投影与终端会话面调用，失败包络原样透出 |
| `assets/terminal.js` | 终端页面渲染：目标类型、目标选择、执行身份与影响确认、会话状态与占用、终端区域、会话审计 |
| `assets/session.js` | 浏览器端会话状态机：仅 `connected` 接受输入、会话内递增序号、重复序号返回原接收结果 |
| `tests/shell/`、`tests/acceptance/CONSOLE-TC-*/terminal.test.js` | 浏览器资产自测：渲染顺序、空数据、失败分类、限额展示与窄屏布局 |
