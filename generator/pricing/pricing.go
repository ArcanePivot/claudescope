// Package pricing 实现 ClaudeScope M2：Anthropic 模型价格表加载与单行 priceUsage。
//
// 契约对应文档：
//   - docs/pricing-config.md  用户配置文件 ~/.claude-scope/pricing.json
//   - v1.1 §4.5             pricing.json 设计
//
// 数据流：
//   LoadPricingRules() → []PricingRule → PriceUsage(model, event, rules) → CostSummary
//
// 加载策略（v1.1 §4.5）：
//   - 用户文件存在 → 完全覆盖内置（不 patch merge，避免漏字段意外）
//   - 不存在或损坏 → 用内置 Anthropic 公开价
//   - 单位：USD per 1M tokens
//   - pattern：substring + case-insensitive，按数组顺序首条命中胜出
package pricing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PricingRule 单条模型价格规则。USD per 1M tokens。
type PricingRule struct {
	Label       string   `json:"label"`
	Patterns    []string `json:"patterns"`
	Input       float64  `json:"input"`
	CacheRead   float64  `json:"cacheRead"`
	CacheCreate float64  `json:"cacheCreate"`
	Output      float64  `json:"output"`
}

// PricingFile 是 ~/.claude-scope/pricing.json 的根结构。
type PricingFile struct {
	Rules    []PricingRule `json:"rules"`
	Fallback *PricingRule  `json:"fallback,omitempty"`
}

// CostSummary 单行计费结果（USD）。
type CostSummary struct {
	Input       float64
	CacheRead   float64
	CacheCreate float64
	Output      float64
	Total       float64
	// PricedTokens 是被该规则计入计费的 token 数；
	// UnpricedTokens 是未匹配任何规则时累计的 token 数（用于 surface fallback 比例）
	PricedTokens   int64
	UnpricedTokens int64
	Matched        bool   // 是否命中某条规则
	MatchedLabel   string // 命中的 rule.label
}

// builtinAnthropicRules 是内置 Anthropic 公开价（截至 2026-05）。
// 数据来源：Anthropic 官方 API pricing 页面。
//
// 单位：USD / 1M tokens
//
// 价格档位说明：
//   - cacheRead：cache_read_input_tokens（命中缓存的输入）
//   - cacheCreate：cache_creation_input_tokens（写缓存，约 1.25× input）
//   - input：未命中缓存的输入
//   - output：含 thinking 的输出 token
func builtinAnthropicRules() []PricingRule {
	return []PricingRule{
		{
			Label:       "Claude Opus 4.7",
			Patterns:    []string{"claude-opus-4-7", "claude-opus-4.7", "opus-4-7"},
			Input:       15.00,
			CacheRead:   1.50,
			CacheCreate: 18.75,
			Output:      75.00,
		},
		{
			Label:       "Claude Opus 4 系列",
			Patterns:    []string{"claude-opus-4", "opus-4"},
			Input:       15.00,
			CacheRead:   1.50,
			CacheCreate: 18.75,
			Output:      75.00,
		},
		{
			Label:       "Claude Sonnet 4.6",
			Patterns:    []string{"claude-sonnet-4-6", "claude-sonnet-4.6", "sonnet-4-6"},
			Input:       3.00,
			CacheRead:   0.30,
			CacheCreate: 3.75,
			Output:      15.00,
		},
		{
			Label:       "Claude Sonnet 4 系列",
			Patterns:    []string{"claude-sonnet-4", "sonnet-4"},
			Input:       3.00,
			CacheRead:   0.30,
			CacheCreate: 3.75,
			Output:      15.00,
		},
		{
			Label:       "Claude Haiku 4.5",
			Patterns:    []string{"claude-haiku-4-5", "claude-haiku-4.5", "haiku-4-5"},
			Input:       0.80,
			CacheRead:   0.08,
			CacheCreate: 1.00,
			Output:      4.00,
		},
		{
			Label:       "Claude Haiku 4 系列",
			Patterns:    []string{"claude-haiku-4", "haiku-4"},
			Input:       0.80,
			CacheRead:   0.08,
			CacheCreate: 1.00,
			Output:      4.00,
		},
		{
			Label:       "Claude Sonnet 3.7",
			Patterns:    []string{"claude-3-7-sonnet", "claude-sonnet-3-7", "sonnet-3-7"},
			Input:       3.00,
			CacheRead:   0.30,
			CacheCreate: 3.75,
			Output:      15.00,
		},
		{
			Label:       "Claude Haiku 3.5",
			Patterns:    []string{"claude-3-5-haiku", "claude-haiku-3-5", "haiku-3-5"},
			Input:       0.80,
			CacheRead:   0.08,
			CacheCreate: 1.00,
			Output:      4.00,
		},
	}
}

// builtinFallback 内置 fallback：未匹配模型按 0 计价并标 Unpriced。
// 用户可在 pricing.json 顶层 fallback 字段覆盖。
func builtinFallback() PricingRule {
	return PricingRule{
		Label:       "未知模型",
		Patterns:    []string{},
		Input:       0,
		CacheRead:   0,
		CacheCreate: 0,
		Output:      0,
	}
}

// UserConfigPath 返回用户 pricing.json 的预期路径。
func UserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude-scope", "pricing.json")
}

// validateRule 校验单条 PricingRule 的关键字段。
//
// 钦定约束（v1.0 §13 P-3 + Phase 3C-Gate2 P1-3）：
//   - label 非空
//   - patterns 至少 1 条非空字符串
//   - 4 项价格 ≥ 0（零价合法 = 用户主动表达「免费档」；负价禁止）
//
// idx 仅用于错误信息定位（数组下标，0 起）；isFallback 影响错误前缀。
func validateRule(r PricingRule, idx int, isFallback bool) error {
	prefix := fmt.Sprintf("rules[%d]", idx)
	if isFallback {
		prefix = "fallback"
	}
	if strings.TrimSpace(r.Label) == "" {
		return fmt.Errorf("%s: label 不能为空", prefix)
	}
	if !isFallback {
		// fallback 不必有 patterns（按 v1.0 §4.5：fallback 是兜底，无需匹配模式）
		if len(r.Patterns) == 0 {
			return fmt.Errorf("%s: patterns 至少要 1 条", prefix)
		}
		hasNonEmpty := false
		for _, p := range r.Patterns {
			if strings.TrimSpace(p) != "" {
				hasNonEmpty = true
				break
			}
		}
		if !hasNonEmpty {
			return fmt.Errorf("%s: patterns 至少要 1 条非空字符串", prefix)
		}
	}
	if r.Input < 0 {
		return fmt.Errorf("%s: input 价格不能为负 (%v)", prefix, r.Input)
	}
	if r.CacheRead < 0 {
		return fmt.Errorf("%s: cacheRead 价格不能为负 (%v)", prefix, r.CacheRead)
	}
	if r.CacheCreate < 0 {
		return fmt.Errorf("%s: cacheCreate 价格不能为负 (%v)", prefix, r.CacheCreate)
	}
	if r.Output < 0 {
		return fmt.Errorf("%s: output 价格不能为负 (%v)", prefix, r.Output)
	}
	return nil
}

// validatePricingFile 在 unmarshal 后校验整体配置；首条非法规则即返回错误。
func validatePricingFile(pf PricingFile) error {
	for i, r := range pf.Rules {
		if err := validateRule(r, i, false); err != nil {
			return err
		}
	}
	if pf.Fallback != nil {
		if err := validateRule(*pf.Fallback, 0, true); err != nil {
			return err
		}
	}
	return nil
}

// LoadPricingRules 加载价格规则。
//
// 返回值：
//   - rules：规则数组（用户优先 / 内置 fallback）
//   - fallback：未命中规则时使用的兜底规则
//   - source：来源说明字符串，写到 data.js 的 pricingSource 字段
//   - err：仅在用户文件存在但解析失败 / schema 校验失败时非 nil（fallback 到内置）
//
// 校验失败的处理（与「合法 JSON 但内容非法」对齐 v1.0 §13 P-3 缓解条款）：
// 用户文件 unmarshal 通过但 validatePricingFile 拒绝 → 视同 broken，fallback 到内置 + warn。
func LoadPricingRules() (rules []PricingRule, fallback PricingRule, source string, err error) {
	path := UserConfigPath()
	if path != "" {
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			var pf PricingFile
			if jsonErr := json.Unmarshal(data, &pf); jsonErr != nil {
				return builtinAnthropicRules(), builtinFallback(),
					fmt.Sprintf("user-config-broken:%s（fallback to builtin）", path),
					fmt.Errorf("解析 %s 失败：%w", path, jsonErr)
			}
			if validateErr := validatePricingFile(pf); validateErr != nil {
				return builtinAnthropicRules(), builtinFallback(),
					fmt.Sprintf("user-config-broken:%s（fallback to builtin）", path),
					fmt.Errorf("校验 %s 失败：%w", path, validateErr)
			}
			if len(pf.Rules) == 0 {
				return builtinAnthropicRules(), builtinFallback(),
					fmt.Sprintf("user-config-empty:%s（fallback to builtin）", path),
					nil
			}
			rules = pf.Rules
			fallback = builtinFallback()
			if pf.Fallback != nil {
				fallback = *pf.Fallback
			}
			return rules, fallback, "user-config:" + path, nil
		}
	}
	return builtinAnthropicRules(), builtinFallback(), "builtin", nil
}

// MatchRule 在 rules 中找首条匹配 model 的规则。未匹配返回 nil。
// 匹配规则：substring + case-insensitive；按数组顺序，首条命中胜出。
func MatchRule(model string, rules []PricingRule) *PricingRule {
	if model == "" {
		return nil
	}
	key := strings.ToLower(model)
	for i := range rules {
		for _, p := range rules[i].Patterns {
			if p == "" {
				continue
			}
			if strings.Contains(key, strings.ToLower(p)) {
				return &rules[i]
			}
		}
	}
	return nil
}

// PriceUsage 对单行 usage（input / cacheRead / cacheCreate / output）计费，返回 USD。
//
// rule 为 nil 时使用 fallback；fallback 价为 0 时所有金额为 0 但 UnpricedTokens 累计。
func PriceUsage(model string, input, cacheRead, cacheCreate, output int64, rules []PricingRule, fallback PricingRule) CostSummary {
	totalTokens := input + cacheRead + cacheCreate + output
	rule := MatchRule(model, rules)
	matched := rule != nil
	if rule == nil {
		f := fallback
		rule = &f
	}
	const million = 1_000_000.0
	cost := CostSummary{
		Input:        float64(input) * rule.Input / million,
		CacheRead:    float64(cacheRead) * rule.CacheRead / million,
		CacheCreate:  float64(cacheCreate) * rule.CacheCreate / million,
		Output:       float64(output) * rule.Output / million,
		Matched:      matched,
		MatchedLabel: rule.Label,
	}
	cost.Total = cost.Input + cost.CacheRead + cost.CacheCreate + cost.Output
	if matched {
		cost.PricedTokens = totalTokens
	} else {
		cost.UnpricedTokens = totalTokens
	}
	return cost
}
