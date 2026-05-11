# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 风格，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [1.0.0] - 2026-05-11

ClaudeScope 首个公开版本。从上游 [CodexScope](https://github.com/JUk1-GH/CodexScope) fork 改造，专门服务 Anthropic Claude Code 工作流。

Fork 起点：上游 commit `21fcc718de232cca7f3453f9156bf8fec1e2aae0`（2026-05-10）。

### 新增

- **Claude Code 数据源**：扫描 `~/.claude/projects/` 全部 `*.jsonl`，按 schema v3 输出
- **`cache_creation_input_tokens` 维度**：在 token 趋势、模型排行、费用拆分中独立成列
- **subagent 父子合并**：通过路径解析将子代理 token 自动合并到父会话；前端会话排行右上角 toggle 可一键展开 / 折叠
- **本地压力估算**：5 小时 + 7 天双滑窗（O(n) 双指针），琥珀橙配色 + 显式 disclaimer 区别于"官方剩余额度"
- **自定义 pricing**：`~/.claude-scope/pricing.json` 完全 override 内置 Anthropic 价格表；带 schema 校验（缺 patterns / 负价格 / 空 label 等会触发警告并回退内置）
- **自定义 risk**：`~/.claude-scope/risk.json` 调整 5h / 7d 阈值；解析失败时回退内置并在 UI 提示"用户配置异常（已回退内置）"
- **未定价提醒**：未匹配任何 pricing 规则的 token 不再显示成 `$0.00`，改为琥珀橙"未定价"徽章 + 顶部 banner 列出涉及模型
- **`<synthetic>` 过滤**：客户端生成的占位 message 自动剔除，统计与排行均不显示，过滤计数写入 `filteredCounts.synthetic`
- **多子命令 CLI**：`claudescope generate / open / version`；版本号通过 `-X main.Version` 注入
- **V2 兼容降级**：保留 schemaVersion=2 的 decoder，老 CodexScope 数据也能在 ClaudeScope 前端打开（自动隐藏 subagent toggle）
- **数据 schema 文档**：`docs/schema-v3.md` / `docs/schema-v2-compat.md` / `docs/pricing-config.md` / `docs/risk-config.md`

### 修改

- 项目品牌：CodexScope → ClaudeScope，全局变量 `CODEXSCOPE_DATA` → `CLAUDESCOPE_DATA`
- generator 入口从 `generate_codex_data.go` 单文件迁到 `generator/cmd/claudescope`，核心流程集中在 `generator/cmd/internal/runner`
- 默认 UI 文案中文化；移除 `landed`/`unchanged`/`pending` 等英文残留
- 启动器（macOS / Windows）改用 `claudescope generate` 子命令，移除 `--cache` 参数（dedup 已内建）

### 删除

- 真实 `data.raw.js`：V3 已把全部数据合并到 `data.js`，generator 不再写 `data.raw.js`；release zip 仅保留一行 `window.CLAUDESCOPE_RAW_DATA = null` 空 stub 用于向后兼容
- OpenAI USD/CNY 汇率换算（Claude 价格统一以 USD 展示）
- CodexScope 的 `quotaRiskRow` 蓝色"官方额度"卡片（替换为琥珀色"本地压力估算"卡）

### 已知限制

- 仅在 macOS arm64 / Windows amd64 上预编译；其他平台需要自行 `npm run build:generator`
- 不支持远端同步、多账户、实时刷新（手动重跑 `generate` 即可）
- 浏览器 V2 兼容路径下 `cache_creation_input_tokens` 列显示为 0（V2 schema 没这个字段）
