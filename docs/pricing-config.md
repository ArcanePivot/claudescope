---
name: pricing-config
description: ClaudeScope 用户自定义价格表 ~/.claude-scope/pricing.json 编写指南
type: reference
---

# ClaudeScope · pricing.json 配置指南

> 文件路径：`~/.claude-scope/pricing.json`
> 优先级：用户文件存在 → **完全覆盖**内置默认；不存在 → 用内置 Anthropic 公开价
> 单位：USD per 1,000,000 tokens（百万 token 美元单价）

---

## 1. 为什么允许自定义价格

1. Anthropic 官方价格表会变动（Opus 4.7 上线后官方公开价 vs ClaudeScope 内置默认可能不同步）
2. 用户可能有企业折扣、token 包优惠、内部转账价
3. 模型新增（Haiku 4.5 / 未来新模型）时，无需等 ClaudeScope 升级，自己写 pattern 即可

---

## 2. 文件结构

```json
{
  "rules": [
    {
      "label": "Claude Opus 4.7",
      "patterns": ["claude-opus-4-7", "claude-opus-4.7"],
      "input": 15.00,
      "cacheRead": 1.50,
      "cacheCreate": 18.75,
      "output": 75.00
    },
    {
      "label": "Claude Opus 4.6",
      "patterns": ["claude-opus-4-6", "claude-opus-4.6"],
      "input": 15.00,
      "cacheRead": 1.50,
      "cacheCreate": 18.75,
      "output": 75.00
    },
    {
      "label": "Claude Sonnet 4.6",
      "patterns": ["claude-sonnet-4-6", "claude-sonnet-4.6"],
      "input": 3.00,
      "cacheRead": 0.30,
      "cacheCreate": 3.75,
      "output": 15.00
    },
    {
      "label": "Claude Haiku 4.5",
      "patterns": ["claude-haiku-4-5"],
      "input": 0.80,
      "cacheRead": 0.08,
      "cacheCreate": 1.00,
      "output": 4.00
    }
  ],
  "fallback": {
    "label": "未知模型",
    "input": 0,
    "cacheRead": 0,
    "cacheCreate": 0,
    "output": 0
  }
}
```

### 2.1 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `rules` | array | ✓ | 价格规则数组，按数组顺序首条命中胜出 |
| `rules[].label` | string | ✓ | UI 显示名（不参与匹配，可用中文） |
| `rules[].patterns` | string[] | ✓ | 匹配 pattern 数组，**小写 + 全形态** |
| `rules[].input` | number | ✓ | 输入 token 单价（USD / 1M tokens） |
| `rules[].cacheRead` | number | ✓ | 缓存命中读取单价 |
| `rules[].cacheCreate` | number | ✓ | 缓存创建（写入）单价 |
| `rules[].output` | number | ✓ | 输出 token 单价（含 thinking） |
| `fallback` | object | 可选 | 未匹配模型的兜底价格，缺失则用 `{label:"未知", 全 0}` |

### 2.2 pattern 匹配规则

- **substring 匹配**（不是正则）
- **case-insensitive**（用户配 `Claude-Opus` 也能命中 `claude-opus-4-7`）
- **数组顺序优先**：`patterns: ["claude-opus", "claude"]` 时，模型 `claude-opus-4-7` 命中第一条
- **rules 数组顺序优先**：第一条 rule 命中即返回，不继续往下找

---

## 3. 编写技巧

### 3.1 同一模型的多个变体

Claude 模型在 jsonl 里有时是 `claude-opus-4-7`，有时是 `claude-opus-4.7`（点号 vs 横杠），写两个 pattern 都覆盖：

```json
"patterns": ["claude-opus-4-7", "claude-opus-4.7"]
```

### 3.2 通配前缀

把所有 Opus 4.x 系列归一价：

```json
{
  "label": "Claude Opus 4 系列",
  "patterns": ["claude-opus-4"],
  "input": 15, "cacheRead": 1.5, "cacheCreate": 18.75, "output": 75
}
```

注意：substring 匹配下 `claude-opus-4` 会同时命中 `claude-opus-4-6`、`claude-opus-4-7`、`claude-opus-4.7`。

### 3.3 自家测试模型

```json
{
  "label": "内部测试",
  "patterns": ["my-internal-model"],
  "input": 0, "cacheRead": 0, "cacheCreate": 0, "output": 0
}
```

### 3.4 启用企业折扣（七折）

把所有公开价 ×0.7 直接写进 rules，不在代码里做 multiplier。这样自查 pricing.json 一目了然。

---

## 4. cost 计算公式

generator 内部对每条事件按以下公式算 cost：

```
cost = input * inputPrice / 1e6
     + cacheRead * cacheReadPrice / 1e6
     + cacheCreate * cacheCreatePrice / 1e6
     + output * outputPrice / 1e6
```

单位：USD（精度 4 位小数，UI 展示根据数值大小自适应）

### 4.1 未匹配模型必须 surface，不得静默 0 处理

如果某条事件的 `model` 没有命中任何 pricing rule（且 `fallback` 也是 0 单价）：

- 该事件 cost = 0（不强行猜价）
- **但**累加到顶层 schema：
  - `unpricedTokens` += 该事件 4 列 token 之和
  - `unpricedModels` 加入该 model 名（去重）
- UI 顶部覆盖率小卡显示：`未匹配价格的 token：N（涉及模型：X, Y）`，提示用户补 pricing.json

这条规则的目的是避免"silently zero"——如果一个模型的 cost 静默为 0，用户会以为今天免费，实际上是模型名拼错或价格表过时。

---

## 5. 内置默认价（**provisional · 草案，未联网校验**）

> ⚠ **provisional / 草案声明**：以下数字基于公开资料 + 类比推测，**未在 Anthropic 官网联网校验**。Phase 5 发布前必须二次校验，不得直接对外宣称为"官方公开价"。
> 实际价格以 Anthropic 官网（console.anthropic.com/pricing）为准。如有出入，写 pricing.json 覆盖。

| 模型 | input | cacheRead | cacheCreate | output | 来源 |
|---|---|---|---|---|---|
| Claude Opus 4.7 | 15.00 | 1.50 | 18.75 | 75.00 | 类比 Opus 4.x 历史档（草案） |
| Claude Opus 4.6 | 15.00 | 1.50 | 18.75 | 75.00 | 类比 Opus 4.x 历史档（草案）；本机 probe 实测 1911 行真实残留，必须有兜底条目 |
| Claude Sonnet 4.6 | 3.00 | 0.30 | 3.75 | 15.00 | 类比 Sonnet 4.x 历史档（草案） |
| Claude Haiku 4.5 | 0.80 | 0.08 | 1.00 | 4.00 | 类比 Haiku 4.x 历史档（草案）；真实模型名带日期后缀如 `claude-haiku-4-5-20251001`，pattern `claude-haiku-4-5` substring 仍能命中 |

**Phase 5 发布前必须**：

1. 联网校验上表 4 个模型 4 列共 16 个数字
2. 任何与官网不符的数字**必须更新**或加注脚说明依据
3. 把"provisional / 草案"标识从 README / UI tooltip 中移除（仅当全部校验通过）
4. UI 顶部 chips：当前显示 `pricing: builtin (provisional)`，校验后改为 `pricing: builtin (verified 2026-MM-DD)`

---

## 6. 错误处理（M2 实现）

generator 启动时按以下顺序处理 `~/.claude-scope/pricing.json`：

| # | 情况 | `pricingSource` | 退出码 | 行为 |
|---|---|---|---|---|
| 1 | 文件不存在 | `builtin` | 0 | 用内置默认价 |
| 2 | 文件存在但 JSON 解析失败 | `user-config-broken:<path>（fallback to builtin）` | 0 + stderr `[警告]` | 回退到内置，避免阻塞首次安装 |
| 3 | 文件存在但 `rules: []` | `user-config-empty:<path>（fallback to builtin）` | 0 | 回退到内置 |
| 4 | 文件存在且合法 | `user-config:<path>` | 0 | 用户规则**完全覆盖**内置 |

**为什么破损不直接 exit**：v1.0 §13 风险表 P-3 的缓解是"友好错误信息" 而非"硬失败"。M2 选择回退 + 在 stderr 打 `[警告]`，并在 cost panel popover 暴露 `pricingSource`，让用户清楚看到"我配的没生效"，但不阻塞 dashboard 打开。

未来 Phase 5 提供 `claude-scope pricing validate` 主动校验时，会在 schema 校验失败时硬退出。

---

## 7. 校验脚本（可选）

未来 Phase 5 会提供：

```bash
claude-scope pricing validate ~/.claude-scope/pricing.json
```

输出：
- 全部 pattern 命中预览（针对当前 catalog.models）
- 哪些模型会落到 fallback（提醒补 rule）
- 总 rules 数 / patterns 数

---

## 8. 与 V2 的差异

| 维度 | V2（CodexScope） | V3（ClaudeScope） |
|---|---|---|
| 字段名 | `cached`（一个） | 拆 `cacheRead` + `cacheCreate` |
| reasoning | 独立字段 | 取消（并入 output） |
| 文件路径 | `~/.codex-scope/pricing.json` | `~/.claude-scope/pricing.json` |
| 模型名空间 | OpenAI 系（gpt-4 / gpt-4o / o1） | Anthropic 系（claude-opus / claude-sonnet / claude-haiku） |

---

## 9. 变更历史

- **v1.0**（2026-05-10）：首版定稿，含 Opus 4.7 / Sonnet 4.6 / Haiku 4.5 三档默认价
