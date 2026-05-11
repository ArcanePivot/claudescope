package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchRuleClaudeOpus47(t *testing.T) {
	rules := builtinAnthropicRules()
	r := MatchRule("claude-opus-4-7-20260101", rules)
	if r == nil {
		t.Fatalf("期望命中 Opus 4.7，实际 nil")
	}
	if !strings.Contains(r.Label, "Opus 4.7") {
		t.Fatalf("期望命中 Opus 4.7，实际 label=%q", r.Label)
	}
}

func TestMatchRuleClaudeSonnet46(t *testing.T) {
	rules := builtinAnthropicRules()
	r := MatchRule("claude-sonnet-4-6-thinking", rules)
	if r == nil || !strings.Contains(r.Label, "Sonnet 4.6") {
		t.Fatalf("期望命中 Sonnet 4.6，实际 %+v", r)
	}
}

func TestMatchRuleHaiku45(t *testing.T) {
	rules := builtinAnthropicRules()
	r := MatchRule("claude-haiku-4-5-20251001", rules)
	if r == nil || !strings.Contains(r.Label, "Haiku 4.5") {
		t.Fatalf("期望命中 Haiku 4.5，实际 %+v", r)
	}
}

func TestMatchRuleUnknownReturnsNil(t *testing.T) {
	rules := builtinAnthropicRules()
	if r := MatchRule("gpt-4o-mini", rules); r != nil {
		t.Fatalf("期望未匹配，实际命中 %s", r.Label)
	}
}

func TestMatchRuleCaseInsensitive(t *testing.T) {
	rules := builtinAnthropicRules()
	r := MatchRule("Claude-Opus-4-7", rules)
	if r == nil {
		t.Fatalf("期望大小写不敏感匹配 Opus 4.7，实际 nil")
	}
}

func TestPriceUsageOpus47(t *testing.T) {
	rules := builtinAnthropicRules()
	fb := builtinFallback()
	// 1M input + 1M cacheRead + 1M cacheCreate + 1M output → 15 + 1.5 + 18.75 + 75 = 110.25
	cost := PriceUsage("claude-opus-4-7", 1_000_000, 1_000_000, 1_000_000, 1_000_000, rules, fb)
	if !cost.Matched {
		t.Fatalf("期望 Matched=true")
	}
	const expected = 110.25
	if delta := cost.Total - expected; delta > 0.001 || delta < -0.001 {
		t.Fatalf("Opus 4.7 1M 各档总价应=%v，实际=%v", expected, cost.Total)
	}
	if cost.PricedTokens != 4_000_000 {
		t.Fatalf("PricedTokens 应为 4M，实际 %d", cost.PricedTokens)
	}
	if cost.UnpricedTokens != 0 {
		t.Fatalf("匹配时 UnpricedTokens 应为 0，实际 %d", cost.UnpricedTokens)
	}
}

func TestPriceUsageUnknownGoesUnpriced(t *testing.T) {
	rules := builtinAnthropicRules()
	fb := builtinFallback()
	cost := PriceUsage("gpt-4o-mini", 1_000_000, 0, 0, 0, rules, fb)
	if cost.Matched {
		t.Fatalf("期望 Matched=false")
	}
	if cost.Total != 0 {
		t.Fatalf("fallback 价格为 0，应总价=0，实际 %v", cost.Total)
	}
	if cost.UnpricedTokens != 1_000_000 {
		t.Fatalf("UnpricedTokens 应为 1M，实际 %d", cost.UnpricedTokens)
	}
}

func TestPriceUsageZeroSafe(t *testing.T) {
	rules := builtinAnthropicRules()
	fb := builtinFallback()
	cost := PriceUsage("", 0, 0, 0, 0, rules, fb)
	if cost.Total != 0 || cost.PricedTokens != 0 || cost.UnpricedTokens != 0 {
		t.Fatalf("空模型空 token 应全 0，实际 %+v", cost)
	}
}

func TestLoadPricingRulesBuiltinWhenMissing(t *testing.T) {
	// 强制使用一个不存在的目录作 HOME，触发 fallback to builtin
	t.Setenv("HOME", t.TempDir())
	rules, fb, source, err := LoadPricingRules()
	if err != nil {
		t.Fatalf("HOME 不存在 pricing.json 时不应返回 err，实际 %v", err)
	}
	if len(rules) == 0 {
		t.Fatalf("应返回内置规则，实际空")
	}
	if source != "builtin" {
		t.Fatalf("source 应为 builtin，实际 %q", source)
	}
	if fb.Label == "" {
		t.Fatalf("fallback.Label 应非空")
	}
}

func TestLoadPricingRulesUserOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir 失败：%v", err)
	}
	custom := PricingFile{
		Rules: []PricingRule{
			{Label: "Custom Opus", Patterns: []string{"opus"}, Input: 99, CacheRead: 9.9, CacheCreate: 12.5, Output: 199},
		},
	}
	buf, _ := json.Marshal(custom)
	if err := os.WriteFile(filepath.Join(dir, "pricing.json"), buf, 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	rules, _, source, err := LoadPricingRules()
	if err != nil {
		t.Fatalf("不应有 err，实际 %v", err)
	}
	if len(rules) != 1 || rules[0].Label != "Custom Opus" {
		t.Fatalf("应完全覆盖为用户规则，实际 %+v", rules)
	}
	if !strings.HasPrefix(source, "user-config:") {
		t.Fatalf("source 应为 user-config:...，实际 %q", source)
	}
}

func TestLoadPricingRulesBrokenFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir 失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pricing.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	rules, _, source, err := LoadPricingRules()
	if err == nil {
		t.Fatalf("损坏文件应返回 err")
	}
	if len(rules) == 0 {
		t.Fatalf("损坏时应 fallback 到内置非空规则，实际空")
	}
	if !strings.Contains(source, "user-config-broken") {
		t.Fatalf("source 应含 user-config-broken，实际 %q", source)
	}
}

func TestLoadPricingRulesEmptyArray(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir 失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pricing.json"), []byte(`{"rules":[]}`), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	rules, _, source, err := LoadPricingRules()
	if err != nil {
		t.Fatalf("空数组应静默 fallback，实际 err=%v", err)
	}
	if len(rules) == 0 {
		t.Fatalf("应 fallback 到内置非空规则")
	}
	if !strings.Contains(source, "user-config-empty") {
		t.Fatalf("source 应含 user-config-empty，实际 %q", source)
	}
}

func TestPatternOrderFirstWins(t *testing.T) {
	rules := []PricingRule{
		{Label: "通用 Claude", Patterns: []string{"claude"}, Input: 1, CacheRead: 0.1, CacheCreate: 1.25, Output: 5},
		{Label: "Opus 专用", Patterns: []string{"opus"}, Input: 99, CacheRead: 9.9, CacheCreate: 124, Output: 495},
	}
	r := MatchRule("claude-opus-4-7", rules)
	if r == nil || r.Label != "通用 Claude" {
		t.Fatalf("应按数组顺序首条命中胜出，实际 %+v", r)
	}
}

// Phase 3C-Gate2 P1-3：合法 JSON 但内容非法不能静默接受
// 钦定约束：label 非空 / patterns 至少 1 条非空 / 4 项价格 ≥ 0

func writeUserConfig(t *testing.T, content string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir 失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pricing.json"), []byte(content), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
}

func assertValidationFallback(t *testing.T, content string) {
	t.Helper()
	writeUserConfig(t, content)
	rules, _, source, err := LoadPricingRules()
	if err == nil {
		t.Fatalf("非法规则应返回 err，实际 nil")
	}
	if !strings.Contains(source, "user-config-broken") {
		t.Fatalf("非法规则应回退 source=user-config-broken:...，实际 %q", source)
	}
	if len(rules) == 0 {
		t.Fatalf("校验失败应 fallback 到内置非空规则")
	}
	// 内置 8 条 → 校验失败时 rules 应等于内置
	if len(rules) != len(builtinAnthropicRules()) {
		t.Fatalf("校验失败应 fallback 到内置规则集（%d 条），实际 %d 条",
			len(builtinAnthropicRules()), len(rules))
	}
}

func TestValidationRejectsMissingPatterns(t *testing.T) {
	// rules 数组中某条缺 patterns
	body := `{"rules":[{"label":"X","input":1,"cacheRead":0.1,"cacheCreate":1.25,"output":5}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsEmptyPatterns(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":[],"input":1,"cacheRead":0.1,"cacheCreate":1.25,"output":5}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsAllBlankPatterns(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":["", "  "],"input":1,"cacheRead":0.1,"cacheCreate":1.25,"output":5}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsEmptyLabel(t *testing.T) {
	body := `{"rules":[{"label":"  ","patterns":["claude"],"input":1,"cacheRead":0.1,"cacheCreate":1.25,"output":5}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsNegativeInput(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":["claude"],"input":-1,"cacheRead":0,"cacheCreate":0,"output":0}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsNegativeCacheRead(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":["claude"],"input":0,"cacheRead":-0.5,"cacheCreate":0,"output":0}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsNegativeCacheCreate(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":["claude"],"input":0,"cacheRead":0,"cacheCreate":-1,"output":0}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsNegativeOutput(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":["claude"],"input":0,"cacheRead":0,"cacheCreate":0,"output":-99}]}`
	assertValidationFallback(t, body)
}

func TestValidationRejectsNegativePriceInFallback(t *testing.T) {
	body := `{"rules":[{"label":"X","patterns":["claude"],"input":0,"cacheRead":0,"cacheCreate":0,"output":0}],"fallback":{"label":"未知","input":-1}}`
	assertValidationFallback(t, body)
}

func TestValidationAcceptsZeroPrice(t *testing.T) {
	// 零价合法（"免费档"）
	body := `{"rules":[{"label":"免费试用","patterns":["claude"],"input":0,"cacheRead":0,"cacheCreate":0,"output":0}]}`
	writeUserConfig(t, body)
	rules, _, source, err := LoadPricingRules()
	if err != nil {
		t.Fatalf("零价规则应合法，实际 err=%v", err)
	}
	if !strings.HasPrefix(source, "user-config:") {
		t.Fatalf("source 应为 user-config:...，实际 %q", source)
	}
	if len(rules) != 1 {
		t.Fatalf("零价规则应保留 1 条，实际 %d 条", len(rules))
	}
}

func TestValidationAcceptsFallbackWithoutPatterns(t *testing.T) {
	// fallback 无 patterns 合法
	body := `{"rules":[{"label":"X","patterns":["claude"],"input":1,"cacheRead":0.1,"cacheCreate":1.25,"output":5}],"fallback":{"label":"未知","input":0,"cacheRead":0,"cacheCreate":0,"output":0}}`
	writeUserConfig(t, body)
	_, fb, source, err := LoadPricingRules()
	if err != nil {
		t.Fatalf("fallback 无 patterns 应合法，实际 err=%v", err)
	}
	if !strings.HasPrefix(source, "user-config:") {
		t.Fatalf("source 应为 user-config:...，实际 %q", source)
	}
	if fb.Label != "未知" {
		t.Fatalf("fallback.Label 应为「未知」，实际 %q", fb.Label)
	}
}
