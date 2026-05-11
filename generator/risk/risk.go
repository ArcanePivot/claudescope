// Package risk 实现 ClaudeScope M3：本地压力估算（不是官方剩余额度）。
//
// 契约对应文档：
//   - docs/risk-config.md §2 文件结构 / §3 算法 / §5 UI / §6 错误处理
//   - mini phase-3a-v2-review.md §10 顶层字段（pressure / riskConfig）
//
// 字段命名一律 pressure* / risk*；禁用 quota*。
package risk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"claudescope/generator/parser"
)

// 默认值（与 docs/risk-config.md §4 同步）
const (
	defaultPrimaryWindowMinutes    = 300
	defaultPrimaryTokenThreshold   = int64(19_000_000)
	defaultSecondaryWindowMinutes  = 10080
	defaultSecondaryTokenThreshold = int64(200_000_000)
	defaultDisclaimer              = "本地压力估算 · 不代表 Anthropic 官方剩余额度"
	defaultPrimaryLabel            = "5 小时窗口"
	defaultSecondaryLabel          = "7 天窗口"
)

// WindowConfig 单个窗口的阈值配置。
type WindowConfig struct {
	Label          string `json:"label"`
	WindowMinutes  int    `json:"windowMinutes"`
	TokenThreshold int64  `json:"tokenThreshold"`
}

// RiskConfig 顶层 risk 配置（写入 data.js 时按 docs/risk-config.md §3.3 输出）。
type RiskConfig struct {
	Primary    WindowConfig  `json:"primary"`
	Secondary  *WindowConfig `json:"secondary,omitempty"`
	Disclaimer string        `json:"disclaimer"`
	Source     string        `json:"source"`
}

// WindowSnapshot 一个窗口在某一刻的累计量 + 百分比。
type WindowSnapshot struct {
	Tokens  int64   `json:"tokens"`
	Percent float64 `json:"percent"`
	AsOf    int64   `json:"asOf,omitempty"`   // current 用：观测时刻 ms
	AtTime  int64   `json:"atTime,omitempty"` // peak 用：峰值出现时刻 ms
}

// WindowPressure 单窗口压力（current + peak）。
type WindowPressure struct {
	Label          string         `json:"label"`
	WindowMinutes  int            `json:"windowMinutes"`
	TokenThreshold int64          `json:"tokenThreshold"`
	Current        WindowSnapshot `json:"current"`
	Peak           WindowSnapshot `json:"peak"`
}

// PressureSummary 顶层 pressure 字段（写入 data.js）。
type PressureSummary struct {
	Primary    WindowPressure  `json:"primary"`
	Secondary  *WindowPressure `json:"secondary,omitempty"`
	Disclaimer string          `json:"disclaimer"`
}

// DefaultRiskConfig 内置默认（Pro 档双窗口 + 标准 disclaimer）。
func DefaultRiskConfig() RiskConfig {
	sec := WindowConfig{
		Label:          defaultSecondaryLabel,
		WindowMinutes:  defaultSecondaryWindowMinutes,
		TokenThreshold: defaultSecondaryTokenThreshold,
	}
	return RiskConfig{
		Primary: WindowConfig{
			Label:          defaultPrimaryLabel,
			WindowMinutes:  defaultPrimaryWindowMinutes,
			TokenThreshold: defaultPrimaryTokenThreshold,
		},
		Secondary:  &sec,
		Disclaimer: defaultDisclaimer,
		Source:     "builtin",
	}
}

// UserConfigPath 返回 ~/.claude-scope/risk.json 的绝对路径（HOME 不存在时返回空串）。
func UserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude-scope", "risk.json")
}

// riskFileShape 是 risk.json 的 JSON shape（disclaimer 允许缺省）。
type riskFileShape struct {
	Primary    *WindowConfig `json:"primary"`
	Secondary  *WindowConfig `json:"secondary"`
	Disclaimer *string       `json:"disclaimer"`
}

// LoadRiskConfig 按下列优先级返回 RiskConfig + 来源 + 可选 err：
//
//   - 文件不存在 → builtin（err == nil）
//   - JSON 解析失败 / 字段缺失 / 参数非法 → builtin + err（main.go 打 stderr 警告，不阻塞 dashboard）
//   - 文件合法 → 用户 override（user-config:<path>），缺省字段用 builtin 兜底
//
// 设计原则：与 pricing.LoadPricingRules 一致 — 友好降级 + 透明 source，不让破损配置阻塞首次启动。
func LoadRiskConfig() (RiskConfig, error) {
	path := UserConfigPath()
	cfg := DefaultRiskConfig()
	if path == "" {
		return cfg, nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		brokenCfg := cfg
		brokenCfg.Source = fmt.Sprintf("user-config-broken:%s（fallback to builtin）", path)
		return brokenCfg, fmt.Errorf("读取 %s 失败：%w", path, err)
	}

	var shape riskFileShape
	if jerr := json.Unmarshal(buf, &shape); jerr != nil {
		brokenCfg := cfg
		brokenCfg.Source = fmt.Sprintf("user-config-broken:%s（fallback to builtin）", path)
		return brokenCfg, fmt.Errorf("解析 %s 失败：%w", path, jerr)
	}

	if shape.Primary == nil {
		brokenCfg := cfg
		brokenCfg.Source = fmt.Sprintf("user-config-broken:%s（缺少 primary，fallback to builtin）", path)
		return brokenCfg, fmt.Errorf("%s 缺少 primary 字段", path)
	}

	primary, perr := normalizeWindow(*shape.Primary, defaultPrimaryLabel, defaultPrimaryWindowMinutes, defaultPrimaryTokenThreshold)
	if perr != nil {
		brokenCfg := cfg
		brokenCfg.Source = fmt.Sprintf("user-config-broken:%s（%s，fallback to builtin）", path, perr.Error())
		return brokenCfg, fmt.Errorf("%s primary 字段非法：%w", path, perr)
	}
	cfg.Primary = primary

	if shape.Secondary != nil {
		secondary, serr := normalizeWindow(*shape.Secondary, defaultSecondaryLabel, defaultSecondaryWindowMinutes, defaultSecondaryTokenThreshold)
		if serr != nil {
			brokenCfg := cfg
			brokenCfg.Source = fmt.Sprintf("user-config-broken:%s（secondary：%s，fallback to builtin）", path, serr.Error())
			return brokenCfg, fmt.Errorf("%s secondary 字段非法：%w", path, serr)
		}
		cfg.Secondary = &secondary
	}

	if shape.Disclaimer != nil && *shape.Disclaimer != "" {
		cfg.Disclaimer = *shape.Disclaimer
	}

	cfg.Source = fmt.Sprintf("user-config:%s", path)
	return cfg, nil
}

// normalizeWindow 校验 + 兜底缺省字段。
//   - WindowMinutes <= 0 → error
//   - TokenThreshold < 0 → error
//   - TokenThreshold == 0 → 允许（视图显示 0%）
//   - Label 为空 → 用默认 label
func normalizeWindow(w WindowConfig, defaultLabel string, defaultWindow int, defaultThreshold int64) (WindowConfig, error) {
	if w.WindowMinutes <= 0 {
		return WindowConfig{}, fmt.Errorf("windowMinutes 必须 > 0（当前 %d）", w.WindowMinutes)
	}
	if w.TokenThreshold < 0 {
		return WindowConfig{}, fmt.Errorf("tokenThreshold 不可为负（当前 %d）", w.TokenThreshold)
	}
	if w.Label == "" {
		w.Label = defaultLabel
	}
	_ = defaultWindow
	_ = defaultThreshold
	return w, nil
}

// ComputePressure 对一组事件 + RiskConfig 计算两个窗口的 current + peak。
//
// 算法（docs/risk-config.md §3.2）：
//   - 双指针 O(n) 滑动窗口，按 Ts 升序扫一遍
//   - peak = 历史窗口和的最大值（出现在某个事件 Ts 时刻）
//   - current = [now - window, now] 区间累计
//
// 边界：
//   - events 为空 → 全 0（不 panic）
//   - threshold = 0 → percent = 0（不除零）
//   - WindowMinutes <= 0 → 全 0（防御）
func ComputePressure(events []parser.ClaudeUsageEvent, cfg RiskConfig, now time.Time) PressureSummary {
	sorted := sortByTs(events)
	summary := PressureSummary{
		Primary:    computeWindow(sorted, cfg.Primary, now),
		Disclaimer: cfg.Disclaimer,
	}
	if cfg.Secondary != nil {
		sec := computeWindow(sorted, *cfg.Secondary, now)
		summary.Secondary = &sec
	}
	if summary.Disclaimer == "" {
		summary.Disclaimer = defaultDisclaimer
	}
	return summary
}

func sortByTs(events []parser.ClaudeUsageEvent) []parser.ClaudeUsageEvent {
	out := make([]parser.ClaudeUsageEvent, 0, len(events))
	for _, e := range events {
		if e.Ts.IsZero() {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Ts.Before(out[j].Ts)
	})
	return out
}

func computeWindow(sorted []parser.ClaudeUsageEvent, cfg WindowConfig, now time.Time) WindowPressure {
	out := WindowPressure{
		Label:          cfg.Label,
		WindowMinutes:  cfg.WindowMinutes,
		TokenThreshold: cfg.TokenThreshold,
		Current: WindowSnapshot{
			AsOf: now.UnixMilli(),
		},
	}
	if cfg.WindowMinutes <= 0 || len(sorted) == 0 {
		return out
	}
	windowDur := time.Duration(cfg.WindowMinutes) * time.Minute

	// peak：双指针，左指针追上窗口下界
	var sum int64
	left := 0
	var peakTokens int64
	var peakTs time.Time
	for i, e := range sorted {
		sum += e.Total()
		lower := e.Ts.Add(-windowDur)
		for left <= i && sorted[left].Ts.Before(lower) {
			sum -= sorted[left].Total()
			left++
		}
		if sum > peakTokens {
			peakTokens = sum
			peakTs = e.Ts
		}
	}
	if peakTokens > 0 {
		out.Peak = WindowSnapshot{
			Tokens:  peakTokens,
			Percent: clampPercent(percentOf(peakTokens, cfg.TokenThreshold)),
			AtTime:  peakTs.UnixMilli(),
		}
	}

	// current：[now - window, now]
	var currentSum int64
	cutLow := now.Add(-windowDur)
	for _, e := range sorted {
		if e.Ts.Before(cutLow) {
			continue
		}
		if e.Ts.After(now) {
			break
		}
		currentSum += e.Total()
	}
	out.Current.Tokens = currentSum
	out.Current.Percent = clampPercent(percentOf(currentSum, cfg.TokenThreshold))
	return out
}

func percentOf(tokens, threshold int64) float64 {
	if threshold <= 0 {
		return 0
	}
	return float64(tokens) / float64(threshold) * 100.0
}

func clampPercent(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
