---
name: risk-config
description: ClaudeScope 本地压力估算配置 ~/.claude-scope/risk.json 阈值与 disclaimer 规范
type: reference
---

# ClaudeScope · risk.json 配置指南

> 文件路径：`~/.claude-scope/risk.json`
> 核心立场：**本地压力估算 ≠ 官方剩余额度**。所有 UI 文案、颜色、tooltip 都必须明示这一点
> 单位：windowMinutes 为整数分钟，tokenThreshold 为 token 整数

---

## 1. 为什么不叫"配额"

CodexScope 原版从 OpenAI 官方接口拿剩余额度，所以叫 `quotaThreshold`。**Anthropic 不公开 5h / 周窗口剩余额度**，ClaudeScope 只能：

> 把"窗口内累计 token / 用户配置阈值"作为**本地压力百分比**。

这**不是**官方额度，所以：

- 字段名一律 `pressure*` / `risk*`，禁用 `quota*`
- UI 文案用「本地压力估算」、「压力百分比」、「估算」等修饰
- 颜色避开 CodexScope 蓝色（=官方），改琥珀橙
- 每个 risk 卡顶部副标题强制写 disclaimer

---

## 2. 文件结构

```json
{
  "primary": {
    "label": "5 小时窗口",
    "windowMinutes": 300,
    "tokenThreshold": 19000000
  },
  "secondary": {
    "label": "7 天窗口",
    "windowMinutes": 10080,
    "tokenThreshold": 200000000
  },
  "disclaimer": "本地压力估算，不代表 Anthropic 官方剩余额度"
}
```

### 2.1 字段说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `primary.label` | string | 可选 | "5 小时窗口" | UI 显示名 |
| `primary.windowMinutes` | int | ✓ | 300 | 滑动窗口长度（分钟） |
| `primary.tokenThreshold` | int | ✓ | 19_000_000 | 用户阈值（窗口内累计达到即 100% 压力） |
| `secondary.*` | object | 可选 | 周窗口默认 | 同 primary，缺失则不显示第二张卡 |
| `disclaimer` | string | 可选 | 见下文默认 | 卡片副标题文案 |

### 2.2 默认 disclaimer

```
本地压力估算 · 不代表 Anthropic 官方剩余额度
```

如果 Master 想换成更温和的措辞，可以在 risk.json 里覆盖。**不允许覆盖为空字符串**——空字符串视为字段缺失，generator 强制回填默认。

---

## 3. 压力百分比算法

### 3.1 滑动窗口

对每个时间点 t，统计 `[t - windowMinutes, t]` 区间内所有 token（input + cacheRead + cacheCreate + output）总和：

```
pressureAt(t) = clampPercent( windowSum(t) / tokenThreshold * 100 )
```

`clampPercent(x) = max(0, min(100, x))`（不允许负数或 >100）。

### 3.2 实现要点

- **双指针 O(n)**：左右指针扫一遍 records，左指针追上窗口上界
- **同时返回 current 和 peak**：current = 截止 now 的压力，peak = 历史最高压力 + 出现时间
- **空数据**：tokenThreshold > 0 但 windowSum = 0 → 0%（不要 NaN）
- **阈值 = 0 边界**：返回 0% + 一行警告（用户配错），不要除零 panic

### 3.3 输出 schema（views.{period}.pressure 块）

```js
pressure: {
  primary: {
    label: "5 小时窗口",
    windowMinutes: 300,
    tokenThreshold: 19000000,
    current: { tokens: 12300000, percent: 64.7, asOf: ms },
    peak:    { tokens: 18900000, percent: 99.5, atTime: ms }
  },
  secondary: { ... },
  disclaimer: "本地压力估算 · 不代表..."
}
```

---

## 4. 阈值定多少合适

### 4.1 5h 窗口（primary）

社区流传的 Pro 档大致经验值：

| 套餐 | 5h token 阈值（约） |
|---|---|
| Pro | 19_000_000 |
| Max 5x | 88_000_000 |
| Max 20x | 220_000_000 |
| API 直购 | 按 Anthropic 公开 rate limit |

**ClaudeScope 内置默认 = 19_000_000**（Pro 档），其它档自己改 risk.json。

### 4.2 7d 窗口（secondary）

参考值：周阈值 ≈ 5h 阈值 × 30（按一天工作 6 个 5h 窗口估算）。

| 套餐 | 7d token 阈值（约） |
|---|---|
| Pro | 200_000_000 |
| Max 5x | 1_000_000_000 |

**内置默认 = 200_000_000**（Pro 档）。

### 4.3 阈值是估算的估算

- 这些阈值并非 Anthropic 公开数字，是社区从 rate-limit 报错回推
- ClaudeScope 不为阈值准确性负责，只为算法正确性负责
- 用户体感被限速时调高 / 调低阈值，让压力百分比和实际体感对得上即可

---

## 5. UI 三重防误解

任何 risk 卡片必须满足：

### 5.1 副标题强制 disclaimer

```html
<div class="pressure-card">
  <div class="pressure-card__header">
    <h3>5 小时窗口压力</h3>
    <p class="pressure-card__disclaimer">
      本地压力估算 · 不代表 Anthropic 官方剩余额度
    </p>
  </div>
  ...
</div>
```

### 5.2 颜色限定琥珀橙

CSS variable：

```css
:root {
  --pressure-primary: #d97706;   /* 琥珀 600 */
  --pressure-bg:      #fef3c7;   /* 琥珀 100 */
  --pressure-border:  #fbbf24;   /* 琥珀 400 */
}
```

**禁止**用 CodexScope 原版的蓝色系（`--brand-primary: #2563eb`），避免被误认为官方数据。

### 5.3 数字 hover tooltip

悬浮压力数字时显示完整 disclaimer：

```
压力 64.7%
———
基于 5 小时滑动窗口本地累计 token 估算。
阈值来源：用户配置 (~/.claude-scope/risk.json)
此数值不代表 Anthropic 官方 5 小时剩余额度。
```

---

## 6. 错误处理（M3 实现）

generator 启动时按以下顺序处理 `~/.claude-scope/risk.json`：

| # | 情况 | `riskConfig.source` | 退出码 | 行为 |
|---|---|---|---|---|
| 1 | 文件不存在 | `builtin` | 0 | 用内置默认（5h 19M / 7d 200M） |
| 2 | 文件存在但 JSON 解析失败 | `user-config-broken:<path>（fallback to builtin）` | 0 + stderr `[警告]` | 回退内置，避免阻塞 dashboard |
| 3 | 缺少 `primary` 字段 | `user-config-broken:<path>（缺少 primary，fallback to builtin）` | 0 + stderr `[警告]` | 回退内置 |
| 4 | `windowMinutes <= 0` 或 `tokenThreshold < 0` | `user-config-broken:<path>（参数非法，fallback to builtin）` | 0 + stderr `[警告]` | 回退内置 |
| 5 | `tokenThreshold == 0` | `user-config:<path>` | 0 | 合法保留，视图压力百分比固定为 0%（不除零） |
| 6 | `secondary` 缺省 | `user-config:<path>` | 0 | 仅 `primary` 用户阈值，`secondary` 回退到内置默认 |
| 7 | `disclaimer == ""` | `user-config:<path>` | 0 | 视为字段缺失，回填默认 disclaimer |
| 8 | 文件存在且合法 | `user-config:<path>` | 0 | 用户规则覆盖（缺省字段用内置兜底） |

**为什么破损不直接 exit**：与 `pricing-config.md §6` 保持一致策略 — v1.0 §13 风险表 P-3 缓解为「友好错误信息 + 回退」而非硬失败。M3 选择回退 + stderr `[警告]`，并把 `riskConfig.source` 透出到前端（chips 显示 `本地压力估算 · 用户配置异常（已回退内置）`），让用户看到「我配的没生效」，但不阻塞 dashboard 打开。

---

## 7. 与 CodexScope quota 配置的差异

| 维度 | CodexScope V2 | ClaudeScope V3 |
|---|---|---|
| 字段名前缀 | `quota*` | `pressure*` / `risk*` |
| 数据语义 | 官方剩余额度模拟 | 本地压力估算（无官方接口） |
| 文案立场 | 「剩余 X%」 | 「压力 X%」+ 强制 disclaimer |
| 配色 | 蓝色系（官方质感） | 琥珀橙（提示估算） |
| reset 时间 | 显示 | 隐藏（无法获知） |
| disclaimer | 无 | 三重强制 |

---

## 8. 检查清单（开发自查）

- [ ] generator 输出 `riskConfig.disclaimer` 字段非空
- [ ] generator 输出 `pressure.current` 和 `pressure.peak` 都包含 `tokens` 和 `percent`
- [ ] 前端任何显示压力数字的位置都有 disclaimer 副标题
- [ ] 前端任何颜色不是 `--brand-primary` 蓝色
- [ ] tooltip 文案包含「不代表官方剩余额度」字样
- [ ] 用户改 `risk.json` 阈值，重跑 generator，UI 数字同步变化
- [ ] 删除 `risk.json`，重跑 generator，回退到内置默认 + chips 显示 `risk: builtin`

---

## 9. 变更历史

- **v1.0**（2026-05-10）：首版定稿，含 Pro 档双窗口默认阈值
