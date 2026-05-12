---
name: risk-config
description: ClaudeScope 本地压力估算配置 ~/.claude-scope/risk.json preset / weights / 阈值 / disclaimer 规范（v1.0.1）
type: reference
---

# ClaudeScope · risk.json 配置指南（v1.0.1）

> 文件路径：`~/.claude-scope/risk.json`
> 核心立场：**本地压力估算 ≠ 官方剩余额度**。所有 UI 文案、颜色、tooltip 都必须明示这一点
> v1.0.1 新增：preset 系统 + weights（容量导向） + overflow ratio + rate-limit 真实信号

---

## 0. 什么是「本地压力」？

ClaudeScope **没有**也**无法**拿到 Anthropic 官方的剩余额度。
Anthropic 不公开「Pro 档此刻还能用多少 token」这种数字，所有官方信号只在你**实际被限流**那一刻通过错误响应下发。

ClaudeScope 做的是反过来：把你**本地 jsonl 里已经发生的 token 用量**，按滑动窗口累计，再除以一个**社区估算的阈值**，给你一个「占比百分数」。这个百分数：

- **不是**官方剩余额度
- **是**一个让你能直观感觉「这小时是不是烧得猛」的相对值
- 阈值是社区从 rate-limit 错误回推的估算（baseline = `community-estimate-2026-05`）
- 你可以用 `--preset` / `risk.json` 校准成更贴合自己体感的数字

**唯一来自 Anthropic 真实信号的指标**是 v1.0.1 新增的 `rateLimit` 命中计数 —— 那是从你 jsonl 里 429/529 错误行抓出来的，是「Anthropic 真的拒绝了你」。

---

## 1. 三种用法（按复杂度递增）

### 1.1 什么都不配（推荐 Pro 用户）

不写 `risk.json`，CLI 也不传 `--preset` —— ClaudeScope 用 builtin Pro preset：

| 字段 | 默认 |
|---|---|
| primary | 5h 窗口，阈值 19_000_000 |
| secondary | 7d 窗口，阈值 200_000_000 |
| weights | input=1, output=1, cacheCreate=1, cacheRead=0.1 |

启动时 stderr 会打一行提示：

```
[risk] 使用默认 preset 'pro'（社区估算，非 Anthropic 官方额度）
       要校准请加 --preset max-5x 或写 ~/.claude-scope/risk.json，详见 docs/risk-config.md
```

### 1.2 通过 `--preset` 一键切换（Max 用户最快）

```bash
claudescope generate --preset max-5x   # Pro × 5
claudescope generate --preset max-20x  # Pro × 20
claudescope generate --preset pro      # 显式选 Pro
```

CLI preset 优先级 > 文件 preset > builtin pro。

| preset | 倍数 | 5h 阈值 | 7d 阈值 |
|---|---|---|---|
| `pro` | 1× | 19M | 200M |
| `max-5x` | 5× | 95M | 1B |
| `max-20x` | 20× | 380M | 4B |
| `custom` | — | 必须在 risk.json 给 explicit primary/secondary |

### 1.3 完整 risk.json（高级用户）

```json
{
  "preset": "max-5x",
  "baseline": "self-calibrated-2026-05",
  "primary": {
    "label": "5 小时窗口",
    "windowMinutes": 300,
    "tokenThreshold": 100000000
  },
  "secondary": {
    "label": "7 天窗口",
    "windowMinutes": 10080,
    "tokenThreshold": 1200000000
  },
  "weights": {
    "input": 1.0,
    "output": 1.0,
    "cacheCreate": 1.0,
    "cacheRead": 0.1
  },
  "disclaimer": "本地压力估算 · 自校准 · 不代表 Anthropic 官方剩余额度"
}
```

所有字段都可省略；只写 `preset` 也是合法的，其他走 preset 默认。

---

## 2. 字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `preset` | string | 否 | `pro` | `pro` / `max-5x` / `max-20x` / `custom`；CLI `--preset` 优先 |
| `baseline` | string | 否 | `community-estimate-2026-05` | 阈值出处标签，仅展示用 |
| `primary.label` | string | 否 | `"5 小时窗口"` | UI 显示名 |
| `primary.windowMinutes` | int | 给 explicit 时必填 | 300 | 滑动窗口长度（分钟），> 0 |
| `primary.tokenThreshold` | int | 给 explicit 时必填 | preset × Pro 基线 | ≥ 0；= 0 时压力固定 0% |
| `secondary` | object | 否 | preset × Pro 7d 基线 | 同 primary 结构；不写则不显示第二张卡 |
| `weights.input` | float | 否 | 1.0 | input token 权重，≥ 0 |
| `weights.output` | float | 否 | 1.0 | output token 权重，≥ 0 |
| `weights.cacheCreate` | float | 否 | 1.0 | cache_creation token 权重，≥ 0 |
| `weights.cacheRead` | float | 否 | 0.1 | cache_read token 权重，≥ 0；默认压低因为复用已计费上下文 |
| `disclaimer` | string | 否 | 内置 | 卡片副标题；空字符串视为字段缺失，回填默认 |

**字段非法的处理**：任一窗口 `windowMinutes <= 0`、`tokenThreshold < 0`、weights 全 0 或有负数，generator 不 panic，整份配置回退到 preset 默认，`riskConfig.source` 标记为 `user-config-broken:<path>（...）`，UI chips 显示「用户配置异常（已回退内置）」。

---

## 3. 算法（v1.0.1）

### 3.1 加权滑动窗口

对每个时间点 t，统计 `[t - windowMinutes, t]` 区间内：

```
weightedSum(t) = Σ(  weights.input       × input_tokens
                   + weights.output      × output_tokens
                   + weights.cacheCreate × cache_creation_input_tokens
                   + weights.cacheRead   × cache_read_input_tokens )
```

```
ratio(t)   = weightedSum(t) / tokenThreshold           # 不 clamp
percent(t) = clamp(ratio(t) × 100, 0, 100)             # 进度条用
overflow(t) = ratio(t) > 1.0
```

**ratio vs percent 的关键区别**：
- `percent` 用于进度条 fill 宽度，必须 clamp 到 [0, 100]
- `ratio` 是真实倍数，**不** clamp —— 用户用满阈值 2.3 倍时 UI 显示 `100% · 2.3×`，而不是看起来"才刚到 100%"

### 3.2 双指针 O(n)

左右指针扫一遍 records，左指针追上窗口上界。同时返回 `current`（截止 now）和 `peak`（历史最高）。

### 3.3 输出 schema（`pressure` 字段）

```js
pressure: {
  preset: "max-5x",
  baseline: "community-estimate-2026-05",
  official: false,                           // 永远 false
  disclaimer: "本地压力估算 · ...",
  primary: {
    label: "5 小时窗口",
    windowMinutes: 300,
    tokenThreshold: 95000000,
    current: { tokens: 73210000, percent: 77.1, ratio: 0.771, overflow: false, asOf: ms },
    peak:    { tokens: 218300000, percent: 100, ratio: 2.30,  overflow: true,  atTime: ms }
  },
  secondary: { ... 同 primary },
  rateLimit: {                               // v1.0.1 新增；nil 表示无命中
    count7d: 3,
    count30d: 12,
    countAll: 27,
    lastHitTs: ms,
    recent: [ { ts, sessionId, model, kind: "rate_limit" | "overloaded" | "timeout" | "other" } ]
  }
}
```

---

## 4. rate-limit 真实信号（v1.0.1 新增）

ClaudeScope 现在从 jsonl 里 `isApiErrorMessage: true` 的行，按 `message.content[0].text` 的标签分类：

| Kind | 触发文本（不区分大小写） |
|---|---|
| `rate_limit` | `429`、`rate limit`、`rate_limit` |
| `overloaded` | `529`、`overloaded` |
| `timeout` | `504`、`timeout`、`upstream` |
| `other` | 其它 |

只有 `rate_limit` + `overloaded` 计入 `pressure.rateLimit` 命中计数。

**隐私**：parser 只读这一个分类标签文本（Claude Code 客户端归类后的字段，不是用户 prompt），且**只存归一化后的枚举值**，不存原文。

UI 在 risk 列表里多出一行「rate-limit 命中」，展示 7d / 30d / 全部 计数 + 最近命中时间。这是 v1.0.1 里**唯一**来自 Anthropic 真实信号的指标。

---

## 5. preset / baseline 怎么定的

### 5.1 Pro 档基线（community-estimate-2026-05）

| 窗口 | 阈值 | 出处 |
|---|---|---|
| 5h | 19_000_000 | 社区 reddit / discord 多人 rate-limit 报错回推 |
| 7d | 200_000_000 | 5h 阈值 × 30（一天约 6 个 5h 窗口估算） |

### 5.2 Max 档倍数

| Plan | 倍数 | 依据 |
|---|---|---|
| `max-5x` | 5× | Anthropic 公开「Max 5x = Pro 5 倍」 |
| `max-20x` | 20× | Anthropic 公开「Max 20x = Pro 20 倍」 |

倍数是 Anthropic 官方公开的，**乘的 Pro 基线**是社区估算 —— 所以 Max 阈值是「官方倍数 × 社区估算 Pro 基数」，仍是估算。

### 5.3 自校准

如果你用 ClaudeScope 一段时间，发现：
- 显示 70% 你已经被限速 → 阈值估高了，调小
- 显示 200% 你还没被限速 → 阈值估低了，调大

直接在 `risk.json` 给 explicit `primary.tokenThreshold`，把 `baseline` 改成 `"self-calibrated-YYYY-MM"`。

---

## 6. UI 三重防误解

任何 risk 卡片必须满足：

1. **副标题**：每个 risk 卡顶部「本地压力估算 · 不代表官方剩余额度」
2. **颜色**：琥珀橙（estimate），ratio > 1.0 时升级红色（=社区估算阈值都不够用了）
3. **chips**：顶部数据来源 chip 显示 `本地压力估算 · {preset}（社区估算 / 用户配置 / ...）`

**禁止**：
- 用 CodexScope 原版蓝色（=官方质感）
- 隐藏 disclaimer
- 用「剩余」「额度」「quota」字样

---

## 7. 错误处理矩阵

| # | 情况 | `riskConfig.source` | 行为 |
|---|---|---|---|
| 1 | 文件不存在 + CLI 无 preset | `builtin:preset=pro` | Pro 默认 + stderr hint |
| 2 | 文件不存在 + CLI `--preset max-5x` | `builtin:preset=max-5x` | Max 5× 默认 + stderr hint |
| 3 | 文件存在但 JSON 解析失败 | `user-config-broken:<path>（fallback to builtin）` | 回退 + stderr `[警告]` |
| 4 | `primary.windowMinutes <= 0` | `user-config-broken:<path>（primary：...，fallback to builtin）` | 回退 + stderr `[警告]` |
| 5 | `weights` 全 0 或含负数 | `user-config-broken:<path>（weights：...，fallback to builtin）` | 回退 + stderr `[警告]` |
| 6 | `tokenThreshold == 0` | `user-config:<path>` | 合法保留，压力百分比固定 0% |
| 7 | `secondary` 缺省 | `user-config:<path>` | primary 用户阈值 + secondary 走 preset 默认 |
| 8 | 文件合法 | `user-config:<path>（preset=<p>）` | 用户规则覆盖 preset 基线 |

---

## 8. 与 CodexScope quota / v1.0 risk 的差异

| 维度 | CodexScope V2 | ClaudeScope v1.0 | ClaudeScope v1.0.1 |
|---|---|---|---|
| 字段前缀 | `quota*` | `pressure*` | `pressure*` |
| 数据语义 | 官方剩余额度模拟 | 本地压力估算 | 本地压力估算 + rate-limit 真实信号 |
| preset 系统 | — | — | ✓（pro/max-5x/max-20x/custom） |
| weights | — | 全 1 | 容量导向（cacheRead=0.1） |
| overflow ratio | — | 截断 100% | 透传真实倍数（如 2.3×） |
| baseline 标签 | — | — | `community-estimate-2026-05` 等 |
| 多账户 disclaimer | — | — | ✓（多账户混跑时阈值无意义） |

---

## 9. 多账户场景

如果你在同一台机器上跑多个 Claude 账户（个人 Pro + 团队 Max），ClaudeScope 默认会把它们的 token 全部累加到一个 jsonl 池子里，**压力百分比因此失去意义**（你看到 200% 可能两个账户各 100%）。

v1.0.1 暂时**不区分账户**。你可以：
- 用 `--root` 指向单账户的 jsonl 目录
- 跑两次 generator，分别输出到不同 `--out` 路径
- 或忽略压力卡，只看 rate-limit 真实信号（那个按命中行计数，多账户也准）

未来版本（v1.1）会加 `project` / `account` 维度切片。

---

## 10. 检查清单（开发自查）

- [ ] generator 输出 `pressure.preset` 字段非空（`pro` / `max-5x` / `max-20x` / `custom`）
- [ ] generator 输出 `pressure.baseline` 字段非空（默认 `community-estimate-2026-05`）
- [ ] generator 输出 `pressure.official === false`
- [ ] generator 输出 `pressure.primary.current.{tokens, percent, ratio, overflow}` 全字段
- [ ] generator 输出 `pressure.rateLimit` 在有 429/529 时非 nil
- [ ] 前端任何显示压力数字的位置都有 disclaimer
- [ ] 前端任何颜色不是 `--brand-primary` 蓝色
- [ ] overflow 行展示 `100% · 2.3×` 而不是 `100%`
- [ ] CLI `--preset max-20x` 实测把阈值放大 20 倍
- [ ] 用户改 `risk.json`，重跑 generator，UI 数字同步变化
- [ ] 删除 `risk.json`，重跑 generator，回退 preset pro + stderr hint

---

## 11. 变更历史

- **v1.0**（2026-05-10）：首版定稿，单 preset，Pro 档双窗口默认阈值
- **v1.0.1**（2026-05-12）：preset 系统 + weights 容量导向 + overflow ratio + rate-limit 真实信号 + baseline 标签 + 多账户 disclaimer
