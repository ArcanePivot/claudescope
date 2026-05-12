# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 风格，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [1.0.1] - 2026-05-12

聚焦「本地压力可信度」：preset 系统 + 真实 rate-limit 信号 + overflow 透传。功能新增，不改 schema 主版本（仍是 v3）。

### 新增

- **preset 系统**：`pro` / `max-5x` / `max-20x` / `custom` 四档，自动按 Pro 基线放大阈值（5h / 7d 同步）
  - CLI 新增 `--preset` flag，优先级 CLI > `risk.json` 文件 preset > 内置 `pro`
  - 启动时 stderr 输出 hint：`[risk] preset='<p>'（阈值来自社区估算 <baseline>，非 Anthropic 官方额度）`
- **rate-limit 真实信号**：parser 从 `isApiErrorMessage:true` 行的 `message.content[0].text` 按枚举分类（`rate_limit` / `overloaded` / `timeout` / `other`），仅前两类计入 `pressure.rateLimit`
  - 输出 7 天 / 30 天 / 全部命中数 + 最近 10 条 hit（含 sessionId、model、kind）
  - 这是 v1.0.1 里**唯一**来自 Anthropic 真实信号的指标
- **overflow ratio**：`pressure.{primary,secondary}.{current,peak}` 新增 `ratio`（不 clamp 的真实倍数）和 `overflow`（bool）字段；UI 在超阈值时显示 `100% · 2.3×` 而不是看起来"才刚到 100%"
- **容量导向 weights**：`riskConfig.weights` 默认 `{input:1, output:1, cacheCreate:1, cacheRead:0.1}`，cache_read 因为是被复用过的 prompt，对 rate-limit 压力贡献低；用户可在 `risk.json` 覆盖
- **baseline 标签**：`riskConfig.baseline` 默认 `community-estimate-2026-05`，自校准后改为 `self-calibrated-YYYY-MM` 即可
- **fixture**：`fixtures/rate-limit.jsonl`（4 条无 usage 错误行覆盖 429/529/504 三种 kind）+ `TestRateLimitFixture` 单测

### 修改

- `docs/risk-config.md` 完整重写：新增「什么是本地压力」章节，明示 ClaudeScope 没有也无法拿到官方剩余额度
- `riskConfig.source` 格式：`builtin` → `builtin:preset=<p>` / `user-config:<path>（preset=<p>）`，前端 chips 同步展示 preset
- 前端 risk 卡 tone 在 `overflow=true` 时升级红色（社区估算阈值都不够用了 = 该警惕）
- README 中英补充 preset / rate-limit / 多账户 disclaimer 说明

### 修复

- `LoadRiskConfigWithPreset("")` 之前会把空 CLI preset 经 `normalizePreset` 规范成 `"pro"`，导致 risk.json 里的 `preset` 字段被覆盖；现在空字符串保持空，仅在非空时归一化
- `release.yml` GitHub Actions 升级到 Node 24 兼容版本（checkout v5 / setup-go v6 / setup-node v5 / upload-artifact v5 / action-gh-release v3）

### 兼容性

- 数据 schema 仍是 v3。v1.0 的 `data.js` 在 v1.0.1 前端打开会缺 `pressure.preset / baseline / rateLimit / ratio` 字段，UI 会回退到原 v1.0 显示（无 preset 标签、无 rate-limit 行、不显示 `×` 倍数）。重跑 generator 即可获得完整 v1.0.1 显示。

---

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
