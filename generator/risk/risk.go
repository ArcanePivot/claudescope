// Package risk 实现 ClaudeScope 本地压力估算（v1.0.1：preset + weights + overflow ratio）。
//
// 这里产出的数字是**本地估算**，不是 Anthropic 官方剩余额度。请配合：
//
//   - docs/risk-config.md       §2 文件结构 / §3 算法 / §5 UI
//   - docs/v1.0.1-...           preset / baseline / weights / overflow / rate-limit 契约
//
// 字段命名一律 pressure* / risk* / preset / baseline；禁用 quota*。
package risk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claudescope/generator/parser"
)

// Preset 名称（v1.0.1）。custom 表示用户在 risk.json 里显式给阈值，不走 multiplier。
const (
	PresetPro    = "pro"
	PresetMax5x  = "max-5x"
	PresetMax20x = "max-20x"
	PresetCustom = "custom"

	// DefaultBaseline 是社区估算 baseline 的版本标签。
	// 数字本身是 Pro 档社区估算（19M / 5h、200M / 7d），非 Anthropic 官方。
	DefaultBaseline = "community-estimate-2026-05"
)

// Pro 档基线（社区估算，不是官方数字）。
const (
	proPrimaryWindowMin    = 300
	proPrimaryTokens       = int64(19_000_000)
	proSecondaryWindowMin  = 10080
	proSecondaryTokens     = int64(200_000_000)
	defaultPrimaryLabel    = "5 小时窗口"
	defaultSecondaryLabel  = "7 天窗口"
	defaultDisclaimer      = "本地压力估算 · 社区估算 · 不代表 Anthropic 官方剩余额度"
)

// Weights 权重，容量导向：cache_read 因为是被复用过的 prompt，对 rate-limit 压力贡献低。
type Weights struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheCreate float64 `json:"cacheCreate"`
	CacheRead   float64 `json:"cacheRead"`
}

// DefaultWeights 容量导向默认值（plan A4：v1.0.1 保留容量导向，不切成本权重）。
func DefaultWeights() Weights {
	return Weights{
		Input:       1.0,
		Output:      1.0,
		CacheCreate: 1.0,
		CacheRead:   0.1,
	}
}

// Apply 把权重作用到一个事件，返回加权 token 数（float64，最后 percent 再算）。
func (w Weights) Apply(e parser.ClaudeUsageEvent) float64 {
	return w.Input*float64(e.Input) +
		w.Output*float64(e.Output) +
		w.CacheCreate*float64(e.CacheCreate) +
		w.CacheRead*float64(e.CacheRead)
}

// WindowConfig 单窗口配置。
type WindowConfig struct {
	Label          string `json:"label"`
	WindowMinutes  int    `json:"windowMinutes"`
	TokenThreshold int64  `json:"tokenThreshold"`
}

// RiskConfig 顶层配置。Preset 决定怎么从 Pro 基线推导阈值；custom 用 explicit limits。
type RiskConfig struct {
	Preset     string        `json:"preset"`
	Baseline   string        `json:"baseline"`
	Official   bool          `json:"official"` // 永远 false（v1.0.x 没有官方信号）
	Primary    WindowConfig  `json:"primary"`
	Secondary  *WindowConfig `json:"secondary,omitempty"`
	Weights    Weights       `json:"weights"`
	Disclaimer string        `json:"disclaimer"`
	Source     string        `json:"source"`
}

// WindowSnapshot 一个窗口某时刻的累计 + 百分比 + 真实倍数。
//
// Percent 在 [0, 100] 内 clamp，给进度条用；Ratio 不 clamp，给「100%+ / 2.3× preset」显示用。
type WindowSnapshot struct {
	Tokens   int64   `json:"tokens"`
	Percent  float64 `json:"percent"`
	Ratio    float64 `json:"ratio"`              // 真实倍数（tokens / threshold），不 clamp
	Overflow bool    `json:"overflow,omitempty"` // ratio > 1.0
	AsOf     int64   `json:"asOf,omitempty"`     // current 用
	AtTime   int64   `json:"atTime,omitempty"`   // peak 用
}

// WindowPressure 单窗口压力。
type WindowPressure struct {
	Label          string         `json:"label"`
	WindowMinutes  int            `json:"windowMinutes"`
	TokenThreshold int64          `json:"tokenThreshold"`
	Current        WindowSnapshot `json:"current"`
	Peak           WindowSnapshot `json:"peak"`
}

// RateLimitHit 单次 rate-limit / overloaded 命中。
type RateLimitHit struct {
	Ts        int64  `json:"ts"`
	SessionID string `json:"sessionId,omitempty"`
	Model     string `json:"model,omitempty"`
	Kind      string `json:"kind"` // rate_limit / overloaded / timeout / other
}

// RateLimitSignal 聚合命中统计（plan A2：从 jsonl 真实失败行抓出来的高价值信号）。
type RateLimitSignal struct {
	Count7d   int            `json:"count7d"`
	Count30d  int            `json:"count30d"`
	CountAll  int            `json:"countAll"`
	LastHitTs int64          `json:"lastHitTs,omitempty"`
	Recent    []RateLimitHit `json:"recent,omitempty"` // 最近 10 次
}

// PressureSummary 写入 data.js 的 pressure 字段。
type PressureSummary struct {
	Preset     string           `json:"preset"`
	Baseline   string           `json:"baseline"`
	Official   bool             `json:"official"`
	Primary    WindowPressure   `json:"primary"`
	Secondary  *WindowPressure  `json:"secondary,omitempty"`
	RateLimit  *RateLimitSignal `json:"rateLimit,omitempty"`
	Disclaimer string           `json:"disclaimer"`
}

// PresetMultiplier 返回 preset 对应的倍数（相对 Pro）。
// pro=1×、max-5x=5×、max-20x=20×。custom 返回 (0, false) 表示「用 explicit limits」。
func PresetMultiplier(preset string) (float64, bool) {
	switch normalizePreset(preset) {
	case PresetPro:
		return 1.0, true
	case PresetMax5x:
		return 5.0, true
	case PresetMax20x:
		return 20.0, true
	case PresetCustom:
		return 0, false
	default:
		return 1.0, true
	}
}

func normalizePreset(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "pro":
		return PresetPro
	case "max-5x", "max5x", "max_5x":
		return PresetMax5x
	case "max-20x", "max20x", "max_20x":
		return PresetMax20x
	case "custom":
		return PresetCustom
	default:
		return p
	}
}

// DefaultRiskConfig 内置默认：Pro preset、容量导向 weights、社区估算 disclaimer。
func DefaultRiskConfig() RiskConfig {
	return PresetRiskConfig(PresetPro)
}

// PresetRiskConfig 按 preset 名生成 RiskConfig（Pro 基线 × 倍数）。
// preset == custom 时退化为 Pro 基线（custom 必须配合 risk.json explicit limits 使用）。
func PresetRiskConfig(preset string) RiskConfig {
	preset = normalizePreset(preset)
	mult, scalable := PresetMultiplier(preset)
	primary := WindowConfig{
		Label:          defaultPrimaryLabel,
		WindowMinutes:  proPrimaryWindowMin,
		TokenThreshold: proPrimaryTokens,
	}
	secondary := WindowConfig{
		Label:          defaultSecondaryLabel,
		WindowMinutes:  proSecondaryWindowMin,
		TokenThreshold: proSecondaryTokens,
	}
	if scalable && mult != 1.0 {
		primary.TokenThreshold = int64(float64(proPrimaryTokens) * mult)
		secondary.TokenThreshold = int64(float64(proSecondaryTokens) * mult)
	}
	return RiskConfig{
		Preset:     preset,
		Baseline:   DefaultBaseline,
		Official:   false,
		Primary:    primary,
		Secondary:  &secondary,
		Weights:    DefaultWeights(),
		Disclaimer: defaultDisclaimer,
		Source:     "builtin:preset=" + preset,
	}
}

// UserConfigPath 返回 ~/.claude-scope/risk.json 绝对路径（HOME 不存在时返回空串）。
func UserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude-scope", "risk.json")
}

// riskFileShape 是 risk.json 的 JSON shape。
// 所有字段都是 pointer / optional —— 缺省走 preset 默认。
type riskFileShape struct {
	Preset     *string       `json:"preset"`
	Baseline   *string       `json:"baseline"`
	Primary    *WindowConfig `json:"primary"`
	Secondary  *WindowConfig `json:"secondary"`
	Weights    *Weights      `json:"weights"`
	Disclaimer *string       `json:"disclaimer"`
}

// LoadRiskConfig 加载 ~/.claude-scope/risk.json，按以下规则：
//
//   - 文件不存在 → DefaultRiskConfig() (Pro preset)
//   - 解析失败 / 字段非法 → DefaultRiskConfig() + 返回 err（main.go 打 stderr 警告）
//   - preset 字段合法 → 按 preset 生成基线再用 explicit 字段 override
//   - preset 缺省但有 primary → 视作 custom
//
// 与 v1.0 的区别：本版本接受 preset 字段，并把 weights / baseline 暴露给用户。
func LoadRiskConfig() (RiskConfig, error) {
	return LoadRiskConfigWithPreset("")
}

// LoadRiskConfigWithPreset 在 LoadRiskConfig 基础上接受 CLI 钦定的 preset，
// 优先级：CLI preset > risk.json preset > 默认 pro。
func LoadRiskConfigWithPreset(cliPreset string) (RiskConfig, error) {
	cliPreset = strings.TrimSpace(cliPreset)
	if cliPreset != "" {
		cliPreset = normalizePreset(cliPreset)
	}
	path := UserConfigPath()
	if path == "" {
		return PresetRiskConfig(presetOrDefault(cliPreset, PresetPro)), nil
	}

	buf, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PresetRiskConfig(presetOrDefault(cliPreset, PresetPro)), nil
		}
		cfg := PresetRiskConfig(presetOrDefault(cliPreset, PresetPro))
		cfg.Source = fmt.Sprintf("user-config-broken:%s（fallback to builtin）", path)
		return cfg, fmt.Errorf("读取 %s 失败：%w", path, err)
	}

	var shape riskFileShape
	if jerr := json.Unmarshal(buf, &shape); jerr != nil {
		cfg := PresetRiskConfig(presetOrDefault(cliPreset, PresetPro))
		cfg.Source = fmt.Sprintf("user-config-broken:%s（fallback to builtin）", path)
		return cfg, fmt.Errorf("解析 %s 失败：%w", path, jerr)
	}

	// preset 解析：CLI > file > pro
	preset := PresetPro
	if shape.Preset != nil && *shape.Preset != "" {
		preset = normalizePreset(*shape.Preset)
	}
	if cliPreset != "" {
		preset = cliPreset
	}

	cfg := PresetRiskConfig(preset)

	if shape.Primary != nil {
		primary, perr := normalizeWindow(*shape.Primary, defaultPrimaryLabel)
		if perr != nil {
			broken := PresetRiskConfig(preset)
			broken.Source = fmt.Sprintf("user-config-broken:%s（primary：%s，fallback to builtin）", path, perr.Error())
			return broken, fmt.Errorf("%s primary 字段非法：%w", path, perr)
		}
		cfg.Primary = primary
	}

	if shape.Secondary != nil {
		secondary, serr := normalizeWindow(*shape.Secondary, defaultSecondaryLabel)
		if serr != nil {
			broken := PresetRiskConfig(preset)
			broken.Source = fmt.Sprintf("user-config-broken:%s（secondary：%s，fallback to builtin）", path, serr.Error())
			return broken, fmt.Errorf("%s secondary 字段非法：%w", path, serr)
		}
		cfg.Secondary = &secondary
	}

	if shape.Weights != nil {
		w, werr := normalizeWeights(*shape.Weights)
		if werr != nil {
			broken := PresetRiskConfig(preset)
			broken.Source = fmt.Sprintf("user-config-broken:%s（weights：%s，fallback to builtin）", path, werr.Error())
			return broken, fmt.Errorf("%s weights 字段非法：%w", path, werr)
		}
		cfg.Weights = w
	}

	if shape.Baseline != nil && *shape.Baseline != "" {
		cfg.Baseline = *shape.Baseline
	}
	if shape.Disclaimer != nil && *shape.Disclaimer != "" {
		cfg.Disclaimer = *shape.Disclaimer
	}

	cfg.Source = fmt.Sprintf("user-config:%s（preset=%s）", path, preset)
	return cfg, nil
}

func presetOrDefault(p, dft string) string {
	if p == "" {
		return dft
	}
	return p
}

func normalizeWindow(w WindowConfig, defaultLabel string) (WindowConfig, error) {
	if w.WindowMinutes <= 0 {
		return WindowConfig{}, fmt.Errorf("windowMinutes 必须 > 0（当前 %d）", w.WindowMinutes)
	}
	if w.TokenThreshold < 0 {
		return WindowConfig{}, fmt.Errorf("tokenThreshold 不可为负（当前 %d）", w.TokenThreshold)
	}
	if w.Label == "" {
		w.Label = defaultLabel
	}
	return w, nil
}

func normalizeWeights(w Weights) (Weights, error) {
	if w.Input < 0 || w.Output < 0 || w.CacheCreate < 0 || w.CacheRead < 0 {
		return Weights{}, fmt.Errorf("weights 字段不可为负 (input=%.2f output=%.2f cacheCreate=%.2f cacheRead=%.2f)",
			w.Input, w.Output, w.CacheCreate, w.CacheRead)
	}
	if w.Input == 0 && w.Output == 0 && w.CacheCreate == 0 && w.CacheRead == 0 {
		return Weights{}, fmt.Errorf("weights 不能全为 0（否则任何用量都算 0）")
	}
	return w, nil
}

// ComputePressure 计算 primary + secondary 窗口的 current / peak，并附带 rate-limit signal。
//
// 算法：
//   - 双指针 O(n) 滑动窗口
//   - tokens 以 weights 加权后参与 percent / ratio 计算（v1.0.1 新增）
//   - Percent clamp 到 [0, 100]，Ratio 不 clamp（给 100%+ / 2.3× 显示用）
//
// 调用方应在外层用 ComputeRateLimitSignal 把 RateLimit 字段填进来。
func ComputePressure(events []parser.ClaudeUsageEvent, cfg RiskConfig, now time.Time) PressureSummary {
	sorted := sortByTs(events)
	weights := cfg.Weights
	if weights == (Weights{}) {
		weights = DefaultWeights()
	}
	summary := PressureSummary{
		Preset:     cfg.Preset,
		Baseline:   cfg.Baseline,
		Official:   cfg.Official,
		Primary:    computeWindow(sorted, cfg.Primary, weights, now),
		Disclaimer: cfg.Disclaimer,
	}
	if cfg.Secondary != nil {
		sec := computeWindow(sorted, *cfg.Secondary, weights, now)
		summary.Secondary = &sec
	}
	if summary.Disclaimer == "" {
		summary.Disclaimer = defaultDisclaimer
	}
	if summary.Preset == "" {
		summary.Preset = PresetPro
	}
	if summary.Baseline == "" {
		summary.Baseline = DefaultBaseline
	}
	return summary
}

// ComputeRateLimitSignal 从 NoUsageErrors（含 ErrorKind）里抓 rate_limit / overloaded 命中。
//
// 返回 nil 表示没有任何相关命中。
func ComputeRateLimitSignal(errs []parser.ClaudeUsageEvent, now time.Time) *RateLimitSignal {
	cutoff7d := now.Add(-7 * 24 * time.Hour)
	cutoff30d := now.Add(-30 * 24 * time.Hour)

	var all []parser.ClaudeUsageEvent
	for _, e := range errs {
		if !isRateLimitKind(e.ErrorKind) {
			continue
		}
		all = append(all, e)
	}
	if len(all) == 0 {
		return nil
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Ts.Before(all[j].Ts)
	})

	sig := &RateLimitSignal{
		CountAll: len(all),
	}
	for _, e := range all {
		if e.Ts.After(cutoff7d) {
			sig.Count7d++
		}
		if e.Ts.After(cutoff30d) {
			sig.Count30d++
		}
	}

	// 最近 10 次（按时间倒序）
	last := all
	if len(last) > 10 {
		last = last[len(last)-10:]
	}
	for i := len(last) - 1; i >= 0; i-- {
		e := last[i]
		sig.Recent = append(sig.Recent, RateLimitHit{
			Ts:        e.Ts.UnixMilli(),
			SessionID: e.Sid,
			Model:     e.Model,
			Kind:      e.ErrorKind,
		})
	}
	if len(all) > 0 {
		sig.LastHitTs = all[len(all)-1].Ts.UnixMilli()
	}
	return sig
}

func isRateLimitKind(k string) bool {
	return k == parser.ErrorKindRateLimit || k == parser.ErrorKindOverloaded
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

func computeWindow(sorted []parser.ClaudeUsageEvent, cfg WindowConfig, weights Weights, now time.Time) WindowPressure {
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

	// peak：双指针 + 加权 sum
	var sum float64
	left := 0
	var peakWeighted float64
	var peakTs time.Time
	for i, e := range sorted {
		sum += weights.Apply(e)
		lower := e.Ts.Add(-windowDur)
		for left <= i && sorted[left].Ts.Before(lower) {
			sum -= weights.Apply(sorted[left])
			left++
		}
		if sum > peakWeighted {
			peakWeighted = sum
			peakTs = e.Ts
		}
	}
	if peakWeighted > 0 {
		out.Peak = makeSnapshot(int64(peakWeighted), cfg.TokenThreshold)
		out.Peak.AtTime = peakTs.UnixMilli()
	}

	// current：[now - window, now]
	var currentWeighted float64
	cutLow := now.Add(-windowDur)
	for _, e := range sorted {
		if e.Ts.Before(cutLow) {
			continue
		}
		if e.Ts.After(now) {
			break
		}
		currentWeighted += weights.Apply(e)
	}
	curSnap := makeSnapshot(int64(currentWeighted), cfg.TokenThreshold)
	curSnap.AsOf = now.UnixMilli()
	out.Current = curSnap
	return out
}

func makeSnapshot(tokens, threshold int64) WindowSnapshot {
	snap := WindowSnapshot{Tokens: tokens}
	if threshold <= 0 {
		return snap
	}
	ratio := float64(tokens) / float64(threshold)
	snap.Ratio = ratio
	if ratio < 0 {
		snap.Percent = 0
	} else if ratio > 1 {
		snap.Percent = 100
		snap.Overflow = true
	} else {
		snap.Percent = ratio * 100
	}
	return snap
}
