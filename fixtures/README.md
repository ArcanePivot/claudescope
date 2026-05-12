# ClaudeScope Fixtures

> 8 份合成 jsonl 样本，用于 generator 单元测试。**不含真实用户数据**，所有 prompt/text 都是 `<placeholder>`。

---

## 文件清单

| 文件 | 期望行为 | 关键字段 |
|---|---|---|
| `main-session.jsonl` | 解析为 3 条 assistant usage 行（含 1 条 Opus） | `isSidechain:false`，正常 usage |
| `subagent.jsonl` | 解析为 2 条 assistant usage 行，flags 含 `IS_SUBAGENT (0x01)` | `isSidechain:true`，含 `agentId` |
| `synthetic.jsonl` | 2 条 `<synthetic>` 行被 generator drop，1 条真实 Opus 行保留；filteredCounts.synthetic=2 | `message.model == "<synthetic>"` |
| `api-error.jsonl` | 1 条 usage=0 错误行打 `IS_FAILURE \| HAS_ERROR (0x0c)`；1 条无 usage 错误行进 `failureRecordsV3` 但**不**进 token total；1 条成功行正常 | `isApiErrorMessage:true`（一个有 usage、一个无） |
| `rate-limit.jsonl` | 4 条 `isApiErrorMessage:true` 无 usage 行，分别归类为 `rate_limit`（429×2）/ `overloaded`（529×1）/ `timeout`（504×1）；预期 `pressure.rateLimit.countAll == 3`（rate_limit+overloaded，timeout 不计入） | v1.0.1 rate-limit 真实信号 |
| `duplicate-message.jsonl` | 3 条相同 `message.id+requestId` 行 dedup 为 1；1 条 unique strong-key 行；1 条缺 requestId 但有 uuid 的行（uuid fallback）→ `dedupStats.rawUsageRows=5, keptUsageRows=3, duplicatesSkipped=2, uuidFallbackRows=1, weakKeyRows=0` | 故意构造重复 + uuid 兜底（fixture 不含极端 weak-key 行；真实日志 uuid 100% 覆盖，weakKeyRows 几乎不会被触发） |
| `third-party-history.jsonl` | 2 条非 claude 模型行打 `IS_THIRD_PARTY_MODEL (0x10)`，不进主统计；1 条 native claude 行正常；filteredCounts.thirdParty=2 | `model: gpt-5.4 / kimi-k2.6` |
| `empty.jsonl` | 解析无 panic，返回 0 条事件 | 文件为空 |

---

## 使用方式

generator 单元测试调用 `ParseClaudeJsonl(fixturePath, cutoff)`，断言：

- 每个 fixture 对应的 raw 事件数 / kept 事件数
- `flags` 位是否正确（IS_SUBAGENT / IS_FAILURE / HAS_ERROR / IS_THIRD_PARTY_MODEL）
- `filteredCounts` 各字段累加值
- `dedupStats` 各字段累加值
- `IsApiError` 字段是否被识别（含 usage 和无 usage 两种）
- 第三方模型 token 是否进入 `views.thirdParty` 而非主视图

**禁止**把这些 fixtures 用于 prompt 内容相关的测试——它们只验证 usage / 结构解析。

---

## 去重优先级（generator 必须遵守）

```ts
function dedupKey(event): string {
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

**累加规则**：

| 输入 | rawUsageRows | keptUsageRows | duplicatesSkipped | uuidFallbackRows | weakKeyRows |
|---|---|---|---|---|---|
| 第一次见到某 strong key（含 message.id+requestId） | +1 | +1 | — | — | — |
| 重复 strong key | +1 | — | +1 | — | — |
| 第一次见到某 uuid（缺 strong key，但有 uuid） | +1 | +1 | — | +1 | — |
| 重复 uuid | +1 | — | +1 | — | — |
| 第一次见到某 weak key（缺 strong 且缺 uuid，极端兜底） | +1 | +1 | — | — | +1 |
| 重复 weak key | +1 | — | +1 | — | — |

**字段语义**（避免歧义）：

- `uuidFallbackRows`：缺 `message.id+requestId` 但**有** `uuid` 的行数（probe 实测 uuid 100% 覆盖，正常情况这个数 ≈ 28% × 总行数）
- `weakKeyRows`：缺 strong 且缺 uuid 的行数（真实日志极少出现，保留作为极端兜底）

UI 显示：`本次扫描去重 N 条 / uuid 兜底 K 条 / 弱键 M 条`。弱键比例高 = 数据质量差，应提示用户。

---

## 第三方/历史模型隔离策略

**默认行为**（`subagentMerge.enabled=true`、未传 `--include-third-party`）：

1. 任何 `model` 不以 `claude-` 前缀开头的行 → 打 `flags |= 0x10 IS_THIRD_PARTY_MODEL`
2. 仍写入 `recordsV3`（保留），但**不**计入：
   - `views.{period}.cost`
   - `views.{period}.models`（主排行）
   - 顶部 cost 总额
   - pressure 估算窗口
3. 计入 `filteredCounts.thirdParty`
4. 进入独立 `views.thirdParty` 视图（前端可显式打开）

**用户显式覆盖**：

- generator CLI flag `--include-third-party` → 第三方模型进入主统计（仍打 flag，UI 可视化区分）
- 用户在 `~/.claude-scope/pricing.json` 自己写 pattern 命中（如 `gpt-5.4`） → pricing 命中即可，但 flag 仍保留

**为什么这么做**：mini小娜 P0-4 审计指出，Mac 历史日志里存在 `gpt-5.4` / `kimi-k2.6` 等中转残留。如果默认进主费用统计，会把 ClaudeScope 报成 "你今天烧了 $X"，但 X 里其实有一部分是中转模型，不是 Anthropic 真实出账。隔离 + 显式开关比"全算"更诚实。

---

## API 错误两种分支

| 分支 | 字段特征 | generator 行为 |
|---|---|---|
| 有 usage（usage 全 0） | `isApiErrorMessage:true` + `message.usage` 存在但全 0 | 进 `recordsV3`，flags = `IS_FAILURE \| HAS_ERROR (0x0c)`，不计 token |
| 无 usage | `isApiErrorMessage:true` + 无 `message.usage` 字段 | 进 `failureRecordsV3` 独立列表 + `errorStats` 计数，**不**进 `recordsV3` |

理由：无 usage 的错误行如果硬塞进 recordsV3 会让 token total 看起来"少了"（实际是没数据）。分两条路径，让失败率统计准确，token 统计干净。

---

## 字段约定（Anthropic jsonl 公开 schema）

每行一个 JSON 对象：

```jsonc
{
  "type": "user" | "assistant" | "summary" | ...,
  "uuid": "<event uuid>",
  "sessionId": "<session uuid>",
  "agentId": "<只在 subagent 出现>",
  "requestId": "<API 请求 id，去重关键>",
  "timestamp": "ISO 8601",
  "cwd": "<绝对路径>",
  "gitBranch": "<分支名>",
  "parentUuid": "<父事件 uuid 或 null>",
  "isSidechain": false,                   // true 表示子代理
  "isApiErrorMessage": false,             // true 表示 API 错误占位
  "message": {
    "id": "msg_xxx",                      // 去重关键
    "role": "user|assistant",
    "model": "claude-sonnet-4-6 | <synthetic> | gpt-5.4 | ...",
    "content": [{"type":"text","text":"..."}],
    "usage": {                            // 可能不存在（无 usage 错误分支）
      "input_tokens": 0,
      "cache_creation_input_tokens": 0,
      "cache_read_input_tokens": 0,
      "output_tokens": 0
    }
  }
}
```

字段名以 Phase 2.6 schema probe 实跑 `~/.claude/projects/` 输出的统计为准——若 probe 发现真实字段名与上表不一致，**以 probe 为准**修正本文档与 fixtures。

---

## 隐私边界

- 这些是合成数据，可以放进 git 公开
- **不要**把 `~/.claude/projects/` 下的真实 jsonl 复制进来
- 若需新增 fixture，参考已有结构改写，不读真实数据
- Phase 2.6 schema probe 只输出**字段存在性统计**，禁止输出 prompt / message.content / API key / 完整 cwd / 完整 jsonl 原文
