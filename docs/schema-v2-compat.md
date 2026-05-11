---
name: schema-v2-compat
description: ClaudeScope V3 前端如何兼容降级读取 CodexScope V2 老格式 data.js
type: reference
---

# ClaudeScope · V2 兼容降级方案

> 适用前端：ClaudeScope app.ts decoder
> 适用数据：CodexScope 老版生成的 `window.CODEXSCOPE_DATA` / `window.QUOTASCOPE_DATA`（schemaVersion: 2）
> 设计目标：把 V2 数据**升维**为 V3 内存结构，让所有视图代码只走一条路径

---

## 1. 为什么要兼容 V2

- Master 已有 CodexScope 历史数据（OpenAI Codex 用量），希望换 ClaudeScope 后还能打开看
- ClaudeScope 前端是单一二进制（不区分 V2/V3 入口），所以由 decoder 一次性把 V2 升维成 V3
- V3 多出来的字段（`cacheCreate` / `parentSidIdx` / `flags`）在 V2 数据里**全部填默认值 0**，视图自然显示空

---

## 2. 识别 schema 版本

前端入口逻辑：

```ts
function loadDashboardData(): NormalizedData {
  const raw =
    (window as any).CLAUDESCOPE_DATA ||
    (window as any).CODEXSCOPE_DATA ||
    (window as any).QUOTASCOPE_DATA;

  if (!raw) throw new Error("data.js 未加载或全局变量缺失");

  const version = raw.schemaVersion ?? 2;

  if (version === 3) return decodeV3(raw);
  if (version === 2) return decodeV2AsV3(raw);
  throw new Error(`不支持的 schemaVersion=${version}`);
}
```

**注意（Q5 钦定表述）**：

- **generator** 不再写 `CODEXSCOPE_DATA` / `QUOTASCOPE_DATA` 别名（V3 数据只输出 `CLAUDESCOPE_DATA`）
- **reader / 前端 decoder** 继续兼容读取 `CODEXSCOPE_DATA` / `QUOTASCOPE_DATA`，用于打开历史 V2 数据
- 这两件事不矛盾：写不向后写、读向后兼容

---

## 3. V2 → V3 行升维

### 3.1 V2 行格式（CodexScope 真实顺序）

> 来源：CodexScope `generate_codex_data.go` 第 1730-1739 行（已逐字 verify）
> 前端 decoder：`temp-codexscope/app.ts` 第 96-108 行 `decodeUsageRowsV2`

```
[ tsDelta, sidIdx, modelIdx, input, cached, output, reasoning, total ]
//  0       1       2         3      4       5       6          7
```

**关键纠正（v2 contract patch）**：早期 v1.0 文档将位置 5/6 写成了 `reasoning, output`，**实际相反**。`output` 在前、`reasoning` 在后。

### 3.2 升维到 V3 的伪代码

```ts
function upcastV2RowToV3(row: number[]): number[] {
  const [tsDelta, sidIdx, modelIdx, input, cached, output, reasoning, total] = row;
  //                                              ↑ V2[5]   ↑ V2[6]
  //                                              真实顺序：output 在前

  // V3 把 reasoning 并入 output；V2 已经分开存，这里合并
  const v3Output = output + reasoning;

  // V2 没有 cacheCreate，填 0
  const cacheCreate = 0;

  // total 重新计算（保险起见，避免 V2 的 total 计算口径不同）
  const v3Total = input + cached + cacheCreate + v3Output;

  // V2 没有 sourceSidIdx，主 session 自引用
  const sourceSidIdx = sidIdx;

  // V2 没有 flags
  const flags = 0;

  return [
    tsDelta,
    sidIdx,
    modelIdx,
    input,
    cached,        // V2 的 cached → V3 的 cacheRead
    cacheCreate,   // V3 新增，填 0
    v3Output,      // 含原 reasoning
    v3Total,
    sourceSidIdx,  // 自引用
    flags,         // 0
  ];
}
```

### 3.3 catalog.sessions 升维

> 来源：CodexScope `generate_codex_data.go` 第 1728 行（已逐字 verify）

V2 catalog 行：`[sid, displayName, primaryModel]`（**3 列**，无 eventCount）
V3 catalog 行：`[sid, displayName, primaryModel, parentSidIdx, eventCount]`（5 列）

**关键纠正（v2 contract patch）**：早期 v1.0 文档说 V2 catalog 是 4 列，**实际只有 3 列**。CodexScope V2 没有把 eventCount 写进 catalog 行，前端是按 records 自己 group by 累计的。

升维：追加两列，`parentSidIdx = idx`（自引用），`eventCount = 0`（升维时未知，前端可按需 group by 累计）。

```ts
function upcastV2SessionRow(row: any[], idx: number): any[] {
  const [sid, displayName, primaryModel] = row;
  return [sid, displayName, primaryModel, idx, 0];
}
```

---

## 4. failureRecords 兼容

V2 有独立 `failureRecordsV2: [tsDelta, sidIdx, modelIdx][]`。V3 同样保留 `failureRecordsV3`（同 3 列结构），所以不需要升维，直接 rename：

```ts
const failureRecords = raw.failureRecordsV3 || raw.failureRecordsV2 || [];
```

---

## 5. 字段差异 fallback

| V3 字段 | V2 是否存在 | fallback 行为 |
|---|---|---|
| `pricingRules` | 有（结构略不同） | V2 是 OpenAI 模型 pricing，原样保留即可，V3 decoder 不强求字段名一致 |
| `pricingSource` | 无 | 默认填 `"legacy:v2"` |
| `riskConfig` | 部分有（CodexScope 用 `quotaThreshold`） | 字段映射：`quotaThreshold` → `primaryThreshold`，`quotaWindowMinutes` → `primaryWindow` |
| `riskConfig.disclaimer` | 无 | 填 `"V2 数据：原始数据来源为官方额度模拟，不再适用 ClaudeScope 压力估算口径"` |
| `subagentMerge` | 无 | 填 `{ enabled: false, strategy: "none" }`（V2 没有子代理概念） |
| `filteredCounts` | 无 | 填 `{ synthetic: 0, malformed: 0, subagentRows: 0, thirdParty: 0 }` |
| `dedupStats` | 无（V2 没有去重逻辑） | 填 `{ rawUsageRows: N, keptUsageRows: N, duplicatesSkipped: 0, weakKeyRows: 0 }`（升维时 N=records 长度） |
| `unpricedTokens` / `unpricedModels` | 无 | 填 `0` / `[]`（V2 数据无法回推未匹配 pricing 的部分） |
| `views.*.pressure` | 无（V2 是 `quotaRiskRow`） | 视图层 fallback：检测到 `quotaRiskRow` 就把它当 pressure 渲染，但顶部加一行警示「V2 数据：以下为官方额度模拟，并非压力估算」 |

---

## 6. UI 层兼容提示

加载到 V2 数据时，顶部 chips 插入一个橙色提醒：

```
⚠ 兼容模式：当前数据为 CodexScope V2 格式，部分新字段（cache_creation / 子代理拆分）显示为 0
```

只在 `schemaVersion < 3` 时出现。

---

## 7. 不兼容的部分（明示放弃）

V2 数据无论如何无法补出来的：

1. **cache_creation_input_tokens**：OpenAI 没这个概念，V2 数据 `cacheCreate` 全为 0，cost 拆分柱状图缺一栏（视觉上正常显示 0 即可）
2. **subagent 父子关系**：V2 不存在 sidechain，所有 session 视为主 session
3. **API 失败精细位**：V2 没有 `flags`，失败行只能从 `failureRecordsV2` 推回

这些**不再补**，前端在覆盖率小卡显示「兼容 V2 模式」即可。

---

## 8. 测试矩阵

| 输入 | 期望前端表现 |
|---|---|
| 纯 V2 data.js（CodexScope 老数据） | 不报错，cacheCreate 列全 0，subagent toggle 隐藏，risk 卡顶部加兼容警示 |
| 纯 V3 data.js（ClaudeScope 当前数据） | 正常走 V3 decoder，所有功能开启 |
| 损坏 data.js（缺 schemaVersion） | 默认按 V2 处理，能渲染就渲染，不能就显示错误面板 |

---

## 9. decoder 落地代码骨架

```ts
function decodeV2AsV3(raw: any): NormalizedData {
  const records = (raw.recordsV2 || []).map(upcastV2RowToV3);
  const sessions = (raw.catalog?.sessions || []).map((row, idx) =>
    upcastV2SessionRow(row, idx)
  );
  const failureRecords = raw.failureRecordsV2 || [];

  return {
    schemaVersion: 3,
    legacyMode: true,                      // 视图层据此显示兼容警示
    tool: raw.tool || "codexscope",
    generatedAt: raw.generatedAt,
    pricingRules: raw.pricingRules || [],
    pricingSource: raw.pricingSource || "legacy:v2",
    riskConfig: mapV2RiskConfig(raw),
    subagentMerge: { enabled: false, strategy: "none" },
    filteredCounts: { synthetic: 0, malformed: 0, subagentRows: 0, thirdParty: 0 },
    dedupStats: { rawUsageRows: records.length, keptUsageRows: records.length, duplicatesSkipped: 0, weakKeyRows: 0 },
    unpricedTokens: 0,
    unpricedModels: [],
    catalog: {
      sessions,
      models: raw.catalog?.models || [],
    },
    recordBase: raw.recordBase || 0,
    recordsV3: records,
    failureRecordsV3: failureRecords,
    views: mapV2Views(raw.views),
  };
}
```

---

## 10. 变更历史

- **v1.0**（2026-05-10）：首版定稿，覆盖 CodexScope 全部已知 V2 字段
