package risk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claudescope/generator/parser"
)

func mkEvent(tsMin int, tokens int64) parser.ClaudeUsageEvent {
	return parser.ClaudeUsageEvent{
		Ts:    time.Date(2026, 5, 10, 0, tsMin, 0, 0, time.UTC),
		Input: tokens,
	}
}

func TestDefaultRiskConfigShape(t *testing.T) {
	cfg := DefaultRiskConfig()
	if cfg.Primary.WindowMinutes != 300 {
		t.Fatalf("primary 默认 windowMinutes 应为 300，实际 %d", cfg.Primary.WindowMinutes)
	}
	if cfg.Primary.TokenThreshold != 19_000_000 {
		t.Fatalf("primary 默认阈值应为 19M，实际 %d", cfg.Primary.TokenThreshold)
	}
	if cfg.Secondary == nil {
		t.Fatalf("secondary 默认应存在")
	}
	if cfg.Secondary.WindowMinutes != 10080 {
		t.Fatalf("secondary 默认窗口 7d=10080min，实际 %d", cfg.Secondary.WindowMinutes)
	}
	if cfg.Disclaimer == "" {
		t.Fatalf("默认 disclaimer 不能为空")
	}
	if !strings.Contains(cfg.Disclaimer, "本地") || !strings.Contains(cfg.Disclaimer, "估算") {
		t.Fatalf("默认 disclaimer 应含「本地」「估算」字样，实际 %q", cfg.Disclaimer)
	}
	if cfg.Source != "builtin" {
		t.Fatalf("默认 source 应为 builtin，实际 %q", cfg.Source)
	}
}

func TestComputePressureEmpty(t *testing.T) {
	cfg := DefaultRiskConfig()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	p := ComputePressure(nil, cfg, now)
	if p.Primary.Current.Tokens != 0 || p.Primary.Current.Percent != 0 {
		t.Fatalf("空输入 current 应全 0")
	}
	if p.Primary.Peak.Tokens != 0 || p.Primary.Peak.Percent != 0 {
		t.Fatalf("空输入 peak 应全 0")
	}
	if p.Disclaimer == "" {
		t.Fatalf("disclaimer 永远非空")
	}
}

func TestComputePressureSimple(t *testing.T) {
	// 5 个事件分布在 0/60/120/180/240 分钟，每个 1M token
	// 5h 窗口（300 分钟）：to=240min 时累计 = 5 * 1M = 5M
	// 阈值 = 10M → percent = 50%
	events := []parser.ClaudeUsageEvent{
		mkEvent(0, 1_000_000),
		mkEvent(60, 1_000_000),
		mkEvent(120, 1_000_000),
		mkEvent(180, 1_000_000),
		mkEvent(240, 1_000_000),
	}
	cfg := RiskConfig{
		Primary: WindowConfig{
			Label:          "test",
			WindowMinutes:  300,
			TokenThreshold: 10_000_000,
		},
		Disclaimer: "test",
	}
	now := time.Date(2026, 5, 10, 4, 0, 0, 0, time.UTC) // 240 分钟
	p := ComputePressure(events, cfg, now)
	if p.Primary.Current.Tokens != 5_000_000 {
		t.Fatalf("current 应为 5M，实际 %d", p.Primary.Current.Tokens)
	}
	if p.Primary.Current.Percent < 49.9 || p.Primary.Current.Percent > 50.1 {
		t.Fatalf("current percent 应≈50，实际 %v", p.Primary.Current.Percent)
	}
	if p.Primary.Peak.Tokens != 5_000_000 {
		t.Fatalf("peak 应为 5M（最后一个事件时刻），实际 %d", p.Primary.Peak.Tokens)
	}
}

func TestComputePressureWindowSlide(t *testing.T) {
	// 事件在 0 / 30 / 400 分钟，5h 窗口
	// peak 在 30min：[0, 30] 都在 5h 内，sum = 2M
	// 在 400min：[100, 400] 内只有 400min 那条，sum = 1M
	events := []parser.ClaudeUsageEvent{
		mkEvent(0, 1_000_000),
		mkEvent(30, 1_000_000),
		mkEvent(400, 1_000_000),
	}
	cfg := RiskConfig{
		Primary: WindowConfig{
			Label:          "test",
			WindowMinutes:  300,
			TokenThreshold: 10_000_000,
		},
	}
	now := time.Date(2026, 5, 10, 7, 0, 0, 0, time.UTC) // 420 分钟
	p := ComputePressure(events, cfg, now)
	if p.Primary.Peak.Tokens != 2_000_000 {
		t.Fatalf("peak 应为 2M（前两条都在 5h 内），实际 %d", p.Primary.Peak.Tokens)
	}
	// now = 420min，窗口 = [120, 420]，命中 400min 那条
	if p.Primary.Current.Tokens != 1_000_000 {
		t.Fatalf("current 应为 1M（只有 400min 一条命中），实际 %d", p.Primary.Current.Tokens)
	}
}

func TestComputePressurePeakNoDriftLargeN(t *testing.T) {
	// 1000 条均匀分布事件，验证双指针不漂移
	events := make([]parser.ClaudeUsageEvent, 1000)
	for i := range events {
		events[i] = mkEvent(i, 1000)
	}
	cfg := RiskConfig{
		Primary: WindowConfig{
			Label:          "test",
			WindowMinutes:  300,
			TokenThreshold: 1_000_000,
		},
	}
	now := time.Date(2026, 5, 10, 0, 999, 0, 0, time.UTC).Add(time.Minute)
	p := ComputePressure(events, cfg, now)
	// 任意 5h（300 个连续事件）窗口最多 = 301 * 1000 = 301000
	// （因为 [t-300, t] 是闭区间，可包含 301 个事件）
	if p.Primary.Peak.Tokens > 301_000 {
		t.Fatalf("peak 不应超过 301K（单窗口最大），实际 %d", p.Primary.Peak.Tokens)
	}
	if p.Primary.Peak.Tokens < 100_000 {
		t.Fatalf("peak 至少应有数万 token，实际 %d", p.Primary.Peak.Tokens)
	}
}

func TestComputePressureZeroThreshold(t *testing.T) {
	events := []parser.ClaudeUsageEvent{mkEvent(0, 1_000_000)}
	cfg := RiskConfig{
		Primary: WindowConfig{
			Label:          "test",
			WindowMinutes:  300,
			TokenThreshold: 0,
		},
	}
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	p := ComputePressure(events, cfg, now)
	if p.Primary.Current.Percent != 0 {
		t.Fatalf("阈值 0 时 percent 应为 0（不除零），实际 %v", p.Primary.Current.Percent)
	}
	if p.Primary.Peak.Tokens != 1_000_000 {
		t.Fatalf("peak token 数仍应正确累加，实际 %d", p.Primary.Peak.Tokens)
	}
}

func TestComputePressureClampOver100(t *testing.T) {
	// 1 个事件 = 5M token，阈值 1M → 应 clamp 到 100%
	events := []parser.ClaudeUsageEvent{mkEvent(0, 5_000_000)}
	cfg := RiskConfig{
		Primary: WindowConfig{
			Label:          "test",
			WindowMinutes:  300,
			TokenThreshold: 1_000_000,
		},
	}
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	p := ComputePressure(events, cfg, now)
	if p.Primary.Current.Percent != 100 {
		t.Fatalf("超阈值应 clamp 到 100%%，实际 %v", p.Primary.Current.Percent)
	}
	if p.Primary.Peak.Percent != 100 {
		t.Fatalf("peak 也应 clamp 到 100%%，实际 %v", p.Primary.Peak.Percent)
	}
}

func TestComputePressureSecondaryOptional(t *testing.T) {
	cfg := DefaultRiskConfig()
	cfg.Secondary = nil
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	p := ComputePressure(nil, cfg, now)
	if p.Secondary != nil {
		t.Fatalf("Secondary 缺省时不应输出")
	}
}

func TestLoadRiskConfigBuiltinWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := LoadRiskConfig()
	if err != nil {
		t.Fatalf("文件不存在不应返回 err，实际 %v", err)
	}
	if cfg.Source != "builtin" {
		t.Fatalf("source 应为 builtin，实际 %q", cfg.Source)
	}
	if cfg.Primary.WindowMinutes != 300 {
		t.Fatalf("应回退到内置默认，实际 windowMinutes=%d", cfg.Primary.WindowMinutes)
	}
}

func TestLoadRiskConfigUserOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir 失败：%v", err)
	}
	custom := map[string]any{
		"primary": map[string]any{
			"label":          "Max 5x",
			"windowMinutes":  300,
			"tokenThreshold": 88_000_000,
		},
		"disclaimer": "我自己的提示",
	}
	buf, _ := json.Marshal(custom)
	if err := os.WriteFile(filepath.Join(dir, "risk.json"), buf, 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	cfg, err := LoadRiskConfig()
	if err != nil {
		t.Fatalf("用户配置合法不应返回 err，实际 %v", err)
	}
	if !strings.HasPrefix(cfg.Source, "user-config:") {
		t.Fatalf("source 应为 user-config:...，实际 %q", cfg.Source)
	}
	if cfg.Primary.TokenThreshold != 88_000_000 {
		t.Fatalf("primary 阈值应被覆盖为 88M，实际 %d", cfg.Primary.TokenThreshold)
	}
	if cfg.Primary.Label != "Max 5x" {
		t.Fatalf("primary label 应为 Max 5x，实际 %q", cfg.Primary.Label)
	}
	if cfg.Disclaimer != "我自己的提示" {
		t.Fatalf("disclaimer 应被覆盖，实际 %q", cfg.Disclaimer)
	}
	// secondary 未指定 → fallback 到默认
	if cfg.Secondary == nil || cfg.Secondary.WindowMinutes != 10080 {
		t.Fatalf("未指定 secondary 应回退到默认，实际 %+v", cfg.Secondary)
	}
}

func TestLoadRiskConfigBrokenFileFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	_ = os.MkdirAll(dir, 0755)
	if err := os.WriteFile(filepath.Join(dir, "risk.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	cfg, err := LoadRiskConfig()
	if err == nil {
		t.Fatalf("破损 JSON 应返回 err（让 main 打 stderr）")
	}
	if !strings.Contains(cfg.Source, "user-config-broken") {
		t.Fatalf("source 应含 user-config-broken，实际 %q", cfg.Source)
	}
	// 仍应有可用的 builtin 阈值
	if cfg.Primary.WindowMinutes != 300 {
		t.Fatalf("破损时应回退到内置默认 windowMinutes=300，实际 %d", cfg.Primary.WindowMinutes)
	}
}

func TestLoadRiskConfigInvalidWindowFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	_ = os.MkdirAll(dir, 0755)
	bad := `{"primary":{"windowMinutes":-1,"tokenThreshold":1000}}`
	if err := os.WriteFile(filepath.Join(dir, "risk.json"), []byte(bad), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	cfg, err := LoadRiskConfig()
	if err == nil {
		t.Fatalf("非法 windowMinutes 应返回 err")
	}
	if !strings.Contains(cfg.Source, "user-config-broken") {
		t.Fatalf("source 应含 user-config-broken，实际 %q", cfg.Source)
	}
	if cfg.Primary.WindowMinutes != 300 {
		t.Fatalf("应回退到内置默认 300，实际 %d", cfg.Primary.WindowMinutes)
	}
}

func TestLoadRiskConfigZeroThresholdAllowed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	_ = os.MkdirAll(dir, 0755)
	zero := `{"primary":{"windowMinutes":300,"tokenThreshold":0}}`
	if err := os.WriteFile(filepath.Join(dir, "risk.json"), []byte(zero), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	cfg, err := LoadRiskConfig()
	if err != nil {
		t.Fatalf("阈值=0 应允许（不报错），实际 %v", err)
	}
	if cfg.Primary.TokenThreshold != 0 {
		t.Fatalf("阈值应保留为 0，实际 %d", cfg.Primary.TokenThreshold)
	}
	if !strings.HasPrefix(cfg.Source, "user-config:") {
		t.Fatalf("source 应为 user-config:...，实际 %q", cfg.Source)
	}
}

func TestLoadRiskConfigEmptyDisclaimerFallsBack(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".claude-scope")
	_ = os.MkdirAll(dir, 0755)
	empty := `{"primary":{"windowMinutes":300,"tokenThreshold":1},"disclaimer":""}`
	if err := os.WriteFile(filepath.Join(dir, "risk.json"), []byte(empty), 0644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	cfg, err := LoadRiskConfig()
	if err != nil {
		t.Fatalf("不应有 err，实际 %v", err)
	}
	// 空字符串视为缺失，回退到默认
	if cfg.Disclaimer == "" {
		t.Fatalf("空 disclaimer 应回填默认，实际为空")
	}
}
