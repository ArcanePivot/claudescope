# ClaudeScope · recordsV3 Schema

> 数据契约文档 · 主版本：V3 · 兼容降级：V2（CodexScope 老格式）
> 文件位置：generator 输出 `data.js` 中 `window.CLAUDESCOPE_DATA.recordsV3`

---

## 1. 顶层结构

```js
window.CLAUDESCOPE_DATA = {
  schemaVersion: 3,
  tool: "claudescope",
  generatedAt: "2026-05-10 14:00:00",
  windowDays: 30,

  pricingRules: PricingRule[],          // 见 pricing-config.md
  pricingSource: string,                // "builtin" | "user-config:<path>"

  // 价格未命中模型的 token 累计（不能静默 0 处理，必须 surface 给用户）
  unpricedTokens: number,               // 未匹配 pricing 规则的 token 总数
  unpricedModels: string[],             // 这些 token 对应的模型名去重列表

  riskConfig: RiskConfig,               // 见 risk-config.md

  subagentMerge: {
    enabled: boolean,                   // v1.0 默认 true
    strategy: "sessionId+agentId"       // 真实合并字段：相同 sessionId + isSidechain=true + agentId 唯一
  },

  // 解析阶段去重统计（按 message.id + requestId 优先；fallback uuid；再 fallback 弱键）
  dedupStats: {
    rawUsageRows:      number,          // 原始 assistant usage 行数
    keptUsageRows:     number,          // 去重后保留的行数
    duplicatesSkipped: number,          // 被去重 drop 的行数
    uuidFallbackRows:  number,          // 缺 strong key（无 message.id+requestId）但有 uuid 的行数
    weakKeyRows:       number           // 缺 strong 且缺 uuid 的行数（极端兜底；真实日志 uuid 100% 覆盖时 ≈ 0）
  },

  filteredCounts: {
    synthetic:    number,               // generator 过滤的 <synthetic> 行数
    malformed:    number,               // 解析失败的行数
    subagentRows: number,               // 已合并的子代理事件总数
    thirdParty:   number                // 标记为 IS_THIRD_PARTY_MODEL 的行数（默认不进主统计）
  },

  availableRange: { start: ms, end: ms },

  catalog: {
    sessions: SessionRow[],             // 见下文 §3
    models:   string[]                  // 模型名列表（已过滤 <synthetic>，第三方模型保留）
  },

  recordBase: number,                   // 时间戳基准（ms），records 第 0 列是相对偏移

  recordsV3:        UsageRow[],         // 见下文 §2
  failureRecordsV3: FailureRow[],       // 失败事件（也可由 recordsV3 flags 推导）

  views: {
    "24h":   ViewBlock,
    "today": ViewBlock,
    "7":     ViewBlock,
    "30":    ViewBlock,
    "history": ViewBlock,
    "thirdParty": ViewBlock              // 第三方/历史模型独立视图（IS_THIRD_PARTY_MODEL 行）
  }
};
```

---

## 2. recordsV3 行格式（10 列）

```
[ tsDelta, sidIdx, modelIdx, input, cacheRead, cacheCreate, output, total, sourceSidIdx, flags ]
```

| 索引 | 字段 | 类型 | 含义 |
|---|---|---|---|
| 0 | `tsDelta` | int (ms) | 距 `recordBase` 的毫秒偏移；绝对时间戳 = `recordBase + tsDelta` |
| 1 | `sidIdx` | int | **合并后**主 session 在 `catalog.sessions` 的索引。子代理事件已重写为父 sid 索引 |
| 2 | `modelIdx` | int | 模型在 `catalog.models` 的索引 |
| 3 | `input` | int | `message.usage.input_tokens` |
| 4 | `cacheRead` | int | `message.usage.cache_read_input_tokens` |
| 5 | `cacheCreate` | int | `message.usage.cache_creation_input_tokens`（**V3 新增维度**） |
| 6 | `output` | int | `message.usage.output_tokens`（含 thinking 段） |
| 7 | `total` | int | `input + cacheRead + cacheCreate + output` |
| 8 | `sourceSidIdx` | int | 事件的**真实来源** sid 索引：主 session 自引用 = `sidIdx`；子代理事件指向 catalog 里子代理自己那一行。**注意命名**：这一列与 `catalog.sessions[*][3]` 的 `parentSidIdx` 语义不同——这里指向 origin（事件出处），catalog 里的 `parentSidIdx` 才是真父 session 指针 |
| 9 | `flags` | int | 按位或的位标记，见 §2.1 |

### 2.1 flags 位定义

| 位 | 名称 | 含义 |
|---|---|---|
| `0x01` | `IS_SUBAGENT` | 这条事件原本来自子代理 jsonl（已合并到父 sidIdx） |
| `0x02` | `WAS_SYNTHETIC` | 备用位，generator 默认 drop 而非保留 |
| `0x04` | `IS_FAILURE` | 这条事件被识别为失败（API 错误/异常返回） |
| `0x08` | `HAS_ERROR` | usage 全 0 的异常行（区分 synthetic 与正常 0-token 行） |
| `0x10` | `IS_THIRD_PARTY_MODEL` | 非 `claude-*` 模型（历史中转残留 gpt-5.x / kimi-k2.x 等），默认不进主费用统计，进 thirdParty 分组 |
| `0x20`+ | （保留） | 未来扩展（reasoning 拆分、限速等） |

**为什么不用单独的 failureRecordsV3？** flags 第 0x04 位即可推导失败事件，避免数据冗余。`failureRecordsV3` 字段保留只是为兼容老前端 decoder（仍输出，但内容由 flags 推导）。

**关于第三方模型（IS_THIRD_PARTY_MODEL）**：

- 真实日志里历史可能存在 `gpt-5.4` / `kimi-k2.6` 等中转模型残留
- 默认行为：保留进 records（带 `0x10` flag）但**不进主视图费用统计**，进 `views.thirdParty` 分组
- 用户可通过 generator CLI 参数 `--include-third-party` 强制纳入主统计
- 同步 `filteredCounts.thirdParty` 累加这类行计数

### 2.2 行示例

```js
// 一个普通主 session 的 assistant 行
[125000, 0, 1, 1234, 5678, 910, 200, 8022, 0, 0]
//  ↑     ↑  ↑  ↑     ↑     ↑    ↑    ↑     ↑  ↑
//  ts    sid model in    cR    cC   out  tot  src  flags(=0)
//                                              ↑
//                                              sourceSidIdx，主会话自引用 = 0

// 一个子代理事件，已合并到主 session（sidIdx=0）
// 但 sourceSidIdx=2 指向 catalog 中子代理自己那一行
[126500, 0, 1, 800, 2000, 0, 150, 2950, 2, 0x01]
//                                          ↑
//                                          IS_SUBAGENT

// 一个 API 失败行（usage 全 0）
[127000, 0, 1, 0, 0, 0, 0, 0, 0, 0x0c]
//                                ↑
//                                IS_FAILURE | HAS_ERROR

// 一个第三方/历史中转模型行（gpt-5.4 残留），默认隔离
[128000, 0, 3, 500, 0, 0, 200, 700, 0, 0x10]
//                                       ↑
//                                       IS_THIRD_PARTY_MODEL，不进主费用统计
```

---

## 3. catalog.sessions 行格式（V3）

```
[ sid, displayName, primaryModel, parentSidIdx, eventCount ]
```

| 索引 | 字段 | 类型 | 含义 |
|---|---|---|---|
| 0 | `sid` | string | 真实 session UUID（主 session）或 `agent-<id>`（子代理） |
| 1 | `displayName` | string | 用户可读名：主 session 用 `projectName(cwd)`；子代理用 `<父名> · subagent <id 后8位>` |
| 2 | `primaryModel` | string | 该 session 主要使用的模型（最多 token 的那个） |
| 3 | `parentSidIdx` | int | 主 session 自引用（=自己 idx）；子代理指父 idx |
| 4 | `eventCount` | int | 该 session 在 records 里的事件条数 |

### 3.1 catalog 示例

```js
{
  "catalog": {
    "sessions": [
      // idx 0: 主 session
      ["3e907347-c1f1-4075-912c-b5f5f810de65", "swift-hawk-debc", "claude-sonnet-4-6", 0, 12],

      // idx 1: 另一个主 session
      ["ad4c7abe-4d94-4a3b-8316-e0886e655dca", "miniclaw", "claude-opus-4-7", 1, 87],

      // idx 2: 子代理（父在 idx 0）
      ["agent-a4dcb76d4a37f6ddd", "swift-hawk-debc · subagent a4dcb76d", "claude-sonnet-4-6", 0, 3]
    ],
    "models": ["claude-sonnet-4-6", "claude-opus-4-7"]
    //                                                ↑ <synthetic> 已被 generator 过滤，不出现
  }
}
```

---

## 4. 默认聚合 vs 反向拆分（前端视图切换）

这是 V3 schema 的关键设计：**记录子代理身份但默认按父聚合**。

### 4.1 默认聚合视图（按 sidIdx）

前端按 `row[1]` group by → 自动得到合并后的 session 排行。

```js
const aggregated = {};
for (const row of recordsV3) {
  const sid = row[1];
  aggregated[sid] = (aggregated[sid] || 0) + row[7]; // 累加 total
}
// 主 session 已经包含子代理 token，零额外开销
```

### 4.2 反向拆分视图（按 sourceSidIdx）

前端 toggle 切换后，按 `row[8]` group by → 子代理独立成行。

```js
const expanded = {};
for (const row of recordsV3) {
  const realSid = row[8]; // sourceSidIdx：真实来源 sid（含子代理自身那一行）
  expanded[realSid] = (expanded[realSid] || 0) + row[7];
}
```

**三类 sid 关系总结**（避免实现写错）：

```
默认聚合视图：group by row[1] sidIdx                   → 子代理 token 已合到父
反向拆分视图：group by row[8] sourceSidIdx             → 子代理独立成行
session 树（父子关系）：catalog.sessions[*][3] parentSidIdx → 真 parent 指针
```

`row[8] sourceSidIdx` 与 `catalog.sessions[*][3] parentSidIdx` 名字不同**语义也不同**——前者是事件 origin，后者是 session parent。


### 4.3 视图切换 UI 位置

会话排行面板右上角：`[聚合主会话] / [展开子代理]`，默认聚合。

---

## 5. views 视图块（v1.0 沿用 CodexScope 设计）

```
views.{period} = {
  range: { start: ms, end: ms, label: "..." },
  trend: TrendBlock,
  distribution: DistributionBlock,
  sessions: TopSessionsBlock,
  models:   TopModelsBlock,
  cost:     CostBlock,
  pressure: PressureBlock   // ← 替代 CodexScope 的 quotaRiskRow
}
```

`pressure` 块的详细字段见 `risk-config.md` §3。

---

## 5.1 去重规则（generator 必须遵守）

> 真实日志核查表明：不去重会让 token 计入约 2.8 倍（同一 message.id 在多个 jsonl 重复出现）。

**去重键优先级**：

```ts
function dedupKey(event: ClaudeUsageEvent): string {
  if (event.messageId && event.requestId) {
    return `strong:${event.messageId}:${event.requestId}`;
  }
  if (event.uuid) {
    return `uuid:${event.uuid}`;
  }
  // 弱键：tsMs + sid + model + 4 个 token 数
  return `weak:${event.tsMs}:${event.sid}:${event.model}:${event.input}:${event.cacheRead}:${event.cacheCreate}:${event.output}`;
}
```

**dedupStats 累加规则**（每条原始 assistant usage 行 `rawUsageRows += 1`，然后按下表）：

| 输入分支 | keptUsageRows | duplicatesSkipped | uuidFallbackRows | weakKeyRows |
|---|---|---|---|---|
| 第一次见到某 strong key（含 message.id+requestId） | +1 | — | — | — |
| 重复 strong key | — | +1 | — | — |
| 第一次见到某 uuid（缺 strong key，但有 uuid） | +1 | — | +1 | — |
| 重复 uuid | — | +1 | — | — |
| 第一次见到某 weak key（缺 strong 且缺 uuid，极端兜底） | +1 | — | — | +1 |
| 重复 weak key | — | +1 | — | — |

**字段语义**（避免歧义）：

- `uuidFallbackRows`：缺 `message.id+requestId` 但**有** `uuid` 的行数（probe 实测 uuid 100% 覆盖；正常情况 ≈ 28% × 总行数，与 requestId 71.8% 缺失率互补）
- `weakKeyRows`：缺 strong 且**也缺** uuid 的行数（真实日志极少出现，保留作为极端兜底）

**前端 UI**：覆盖率小卡显示 `本次扫描去重 N 条 / uuid 兜底 K 条 / 弱键 M 条`。弱键比例高 = 数据质量差，应提示用户。

---

## 6. failureRecordsV3 行格式

为兼容 CodexScope 老前端，保留独立失败列表：

```
[ tsDelta, sidIdx, modelIdx ]
```

generator 实现可由 `recordsV3` 中 `flags & IS_FAILURE` 的行筛选得出，不必单独存储原始 jsonl 解析结果。

---

## 7. 与 V2 的差异速查

| 维度 | V2 (CodexScope) | V3 (ClaudeScope) |
|---|---|---|
| 列数 | 8 | 10 |
| V2 行真实顺序 | `[ts, sid, model, input, cached, output, reasoning, total]`（generator 1730 行 verify） | `[ts, sid, model, input, cacheRead, cacheCreate, output, total, sourceSidIdx, flags]` |
| catalog 行 | 3 列 `[sid, displayName, primaryModel]` | 5 列 `[sid, displayName, primaryModel, parentSidIdx, eventCount]` |
| cache 维度 | `cached`（仅 read） | 拆 `cacheRead` + `cacheCreate` |
| reasoning 列 | 有（V2[6]） | **删除**（thinking 并入 output） |
| total 列 | V2[7] | V3[7]，公式调整 |
| 子代理标记 | 无 | `sourceSidIdx` 第 8 列 + `flags` 第 9 列 |
| 第三方模型隔离 | 无 | `flags 0x10 IS_THIRD_PARTY_MODEL` + `views.thirdParty` |
| 去重统计 | 无 | 顶层 `dedupStats` |
| 未匹配 pricing | 无（静默 0） | 顶层 `unpricedTokens` + `unpricedModels` |
| failure 列表 | `failureRecordsV2` 独立数组 | `failureRecordsV3` + `flags` 双通道 |
| schemaVersion | 2 | 3 |
| 全局变量 generator 写 | `CODEXSCOPE_DATA` / `QUOTASCOPE_DATA` | 仅 `CLAUDESCOPE_DATA`（不写旧别名） |
| 全局变量 reader 读 | — | 兼容读取 `CLAUDESCOPE_DATA → CODEXSCOPE_DATA → QUOTASCOPE_DATA`（打开历史 V2 数据） |

详细兼容降级映射见 `schema-v2-compat.md`。

---

## 8. 字段命名约定

- 字段名一律 **camelCase**（与 CodexScope 现有 catalog 字段保持一致）
- token 数量一律 **int**（不是 string，前端不需要 BigInt）
- 时间戳一律 **ms 整数**（不是 ISO 字符串，节省体积）
- pattern 数组一律 **小写 + 全形态**（比如 `["claude-opus-4-7", "claude-opus-4.7"]`）

---

## 9. 体积估算

每行 V3 record：约 60-90 字节 JSON（10 个 int，含分隔符）

100k 行 ≈ 7-9 MB → gzip 后 1-2 MB → 浏览器加载 < 1s。

如果出现极端长会话（百万级行），考虑 Phase 6 加分页/抽样。

---

## 10. 真实数据观察（Phase 2.6 schema probe 输出）

> 数据来源：本机 `~/.claude/projects/`，2026-05-10 扫描，203 个 jsonl，13240 条 assistant 行
> 工具：`generator/probe/probe.py`（脱敏统计，不输出原文）
> 报告（敏感）：`probe-real.json`（已加 .gitignore，仅本地保留）

### 10.1 字段覆盖率（影响 generator 实现）

> 样本规模：13240 条 assistant 行 / 203 jsonl / 50 个子代理路径文件

| 字段 | 覆盖率 | generator 含义 |
|---|---|---|
| `uuid` | 100.0% | 永远存在，安全用作去重 fallback 键 |
| `sessionId` | 100.0% | 永远存在 |
| `parentUuid` | 100.0% | 父子链路完整 |
| `gitBranch` | 100.0% | 可放心展示 |
| `cwd` | 100.0% | 用于 `projectName(cwd)`，永远存在 |
| `message.id` | 100.0% | 永远存在，作为强键主要部分 |
| `message.usage` | 100.0% | 永远存在（注：`isApiErrorMessage` 行也可能携带 usage 全 0；无 usage 错误是另一分支） |
| **`requestId`** | **71.8%** | **关键发现**：28% 缺失！强键命中率上限 ≈ 71.8%，**uuid/弱键 fallback 不是可选** |
| `agentId` | 21.4% | 即"子代理事件"占比 ≈ 21.4%，与 `isSidechain=true` 数量一致（2837 行）|
| `isSidechain=true` | 21.4% | 与 `agentId` 同源，是子代理事件的天然标识 |
| `isApiErrorMessage=true` | 0.9% | 错误占位行 120 条；其中含 usage 全 0 与无 usage 两种分支 |

### 10.2 真实模型分布（按 13240 assistant 行）

| 模型名 | 行数 | 占比 | 备注 |
|---|---|---|---|
| `claude-sonnet-4-6` | 4673 | 35.3% | 主力 |
| `claude-opus-4-7` | 3492 | 26.4% | Opus 主版本（v1.0 plan 里的核心） |
| **`gpt-5.4`** | **2695** | **20.4%** | **第三方残留**（中转），进 thirdParty 视图 |
| **`claude-opus-4-6`** | **1911** | **14.4%** | **历史 Opus 残留**，pricing.md 必须有此条目 |
| `claude-haiku-4-5-20251001` | 310 | 2.3% | **真名带日期后缀**，pattern `claude-haiku-4-5` substring 仍能命中 |
| `<synthetic>` | 154 | 1.2% | 必须 drop |
| `kimi-k2.6` | 5 | <1% | 第三方残留 |

### 10.3 文件内重复

- 文件内 `message.id+requestId` 重复行：4309 / 13240 = **32.5%**
- **注意**：probe 仅在文件内去重；跨文件（同一 message 出现在多个 jsonl）的真实膨胀比例需 generator 跑全局去重后才能精确，估算实际更高
- **强制结论**：dedup 不可缺；按 §5.1 三级 key 实现

### 10.4 usage 块的额外元数据键

probe 统计 `message.usage` 内字段分布（基于 13240 条 assistant 行）：

| 键名 | 命中行数 | 占比 | generator 处理 |
|---|---|---|---|
| `input_tokens` | 13240 | 100% | **取** |
| `output_tokens` | 13240 | 100% | **取** |
| `cache_creation_input_tokens` | 13105 | 99.0% | **取**（缺失行视为 0） |
| `cache_read_input_tokens` | 13105 | 99.0% | **取**（缺失行视为 0） |
| `service_tier` | 13026 | 98.4% | **忽略**（如 `standard` / `priority`，v1.0 不参与定价） |
| `cache_creation` | 13026 | 98.4% | **忽略**（细分对象，v1.0 不读） |
| `inference_geo` | 13026 | 98.4% | **忽略**（地理推理位置） |
| `iterations` | 10806 | 81.6% | **忽略** |
| `server_tool_use` | 10803 | 81.6% | **忽略** |
| `speed` | 10803 | 81.6% | **忽略** |

**generator 实现要求**：解析 usage 块时**只取 4 个 token 字段**，遇到未知键不报错、不警告，静默忽略——避免 Anthropic 后续加新字段时触发误警。

### 10.5 顶层未知键（Claude Code 客户端遥测）

probe 扫到大量 `KNOWN_TOP_KEYS` 之外的字段，多为 Claude Code 客户端写入的本地遥测，与 usage / 计费无关：

`entrypoint` / `slug` / `promptId` / `hookCount` / `subtype` / `tool_use_id` / `userId` / `exitCode` / 等等。

**generator 实现要求**：遇到未知顶层键**只忽略、不报错**。schema 漂移检测留给 probe 工具单独跑。

---

## 11. 变更历史

- **v1.0**（2026-05-10）：首版定稿
- **v1.0-patch**（2026-05-10）：基于 mini小娜 P0/P1 审计 + Phase 2.6 probe 真实数据，新增 §5.1 去重规则、§10 真实数据观察、`flags 0x10 IS_THIRD_PARTY_MODEL`、顶层 `dedupStats / unpricedTokens / unpricedModels / filteredCounts.thirdParty / views.thirdParty`，重命名 recordsV3[8] `parentSidIdx → sourceSidIdx` 以与 catalog 真父指针消除歧义
