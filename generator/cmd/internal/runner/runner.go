// Package runner 把 ClaudeScope generator 的核心流程提炼出来，
// 既给 cmd/generate（早期单命令入口）用，也给 cmd/claudescope（v1.0 多子命令入口）用。
//
// 这里只做装配；扫描 / 解析 / pricing / risk / catalog 仍在各自的 package。
package runner

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claudescope/generator/parser"
	"claudescope/generator/pricing"
	"claudescope/generator/risk"
)

const SchemaVersion = 3
const Tool = "claudescope"

// Options 是 Run 的全部输入。零值合法。
type Options struct {
	Root       string    // Claude Code 日志根目录；缺省 ~/.claude/projects
	Out        string    // 输出 data.js 路径；缺省 data.js
	Since      time.Time // 仅保留之后的事件；零值表示全量
	WindowDays int       // 写入 windowDays 元数据
	Stderr     *os.File  // 进度/警告输出；nil 时回退 os.Stderr
}

// Result 是 Run 的执行报告。失败时 Err 非 nil。
type Result struct {
	FilesScanned int
	RowsKept     int
	OutPath      string
}

// Run 执行一次完整的 generator 流程。
func Run(opts Options) (Result, error) {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	root := opts.Root
	if root == "" {
		root = DefaultClaudeRoot()
	}
	outPath := opts.Out
	if outPath == "" {
		outPath = "data.js"
	}

	paths, err := collectJsonl(root)
	if err != nil {
		return Result{}, fmt.Errorf("扫描 %s 失败：%w", root, err)
	}

	res, err := parser.ParseClaudeJsonlFiles(paths, opts.Since)
	if err != nil {
		return Result{}, fmt.Errorf("全局解析失败：%w", err)
	}

	cat := parser.BuildCatalog(res.KeptEvents, res.NoUsageErrors)
	base := parser.EarliestTsMs(res.KeptEvents, res.NoUsageErrors)
	rows := parser.BuildRecordsV3(res.KeptEvents, cat, base)
	failRows := parser.BuildFailureRecordsV3(res.NoUsageErrors, cat, base)

	rowsArr := rowsAsArrays(rows)
	failArr := failureRowsAsArrays(failRows)
	availableRange := computeAvailableRange(rowsArr, failArr, base)

	pricingRules, pricingFallback, pricingSource, pricingErr := pricing.LoadPricingRules()
	if pricingErr != nil {
		fmt.Fprintf(stderr, "[警告] %v\n", pricingErr)
	}

	riskCfg, riskErr := risk.LoadRiskConfig()
	if riskErr != nil {
		fmt.Fprintf(stderr, "[警告] %v\n", riskErr)
	}
	pressure := risk.ComputePressure(res.KeptEvents, riskCfg, time.Now())

	payload := dataPayload{
		SchemaVersion: SchemaVersion,
		Tool:          Tool,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		WindowDays:    opts.WindowDays,

		Catalog: payloadCatalog{
			Sessions: sessionsAsArrays(cat.Sessions),
			Models:   cat.Models,
		},

		RecordBase:     base,
		AvailableRange: availableRange,

		RecordsV3:        rowsArr,
		FailureRecordsV3: failArr,

		DedupStats:     res.DedupStats,
		FilteredCounts: res.FilteredCounts,
		ErrorStats:     res.ErrorStats,

		PricingRules:    pricingRulesAsPayload(pricingRules),
		PricingFallback: pricingFallbackAsPayload(pricingFallback),
		PricingSource:   pricingSource,

		RiskConfig: riskCfg,
		Pressure:   pressure,

		Notes: payloadNotes{
			ThirdPartyDefaultExcluded: true,
			SubagentMergeApplied:      true,
			PricingApplied:            len(pricingRules) > 0,
			RiskApplied:               true,
			Disclaimer:                "Phase 3C-M4：pricing / risk / subagent 父子合并全部启用；第三方模型默认不计入主统计",
		},
	}

	if err := writeDataJS(outPath, payload); err != nil {
		return Result{}, fmt.Errorf("写 %s 失败：%w", outPath, err)
	}

	fmt.Fprintf(stderr, "已扫描 %d 文件 → %d kept rows，写入 %s\n",
		len(paths), len(rows), outPath)

	return Result{
		FilesScanned: len(paths),
		RowsKept:     len(rows),
		OutPath:      outPath,
	}, nil
}

// DefaultClaudeRoot 返回 ~/.claude/projects 的绝对路径，HOME 取不到时返回空串。
func DefaultClaudeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

type dataPayload struct {
	SchemaVersion int    `json:"schemaVersion"`
	Tool          string `json:"tool"`
	GeneratedAt   string `json:"generatedAt"`
	WindowDays    int    `json:"windowDays,omitempty"`

	Catalog payloadCatalog `json:"catalog"`

	RecordBase     int64       `json:"recordBase"`
	AvailableRange payloadTime `json:"availableRange"`

	RecordsV3        [][10]int64 `json:"recordsV3"`
	FailureRecordsV3 [][3]int64  `json:"failureRecordsV3"`

	DedupStats     parser.DedupStats     `json:"dedupStats"`
	FilteredCounts parser.FilteredCounts `json:"filteredCounts"`
	ErrorStats     parser.ErrorStats     `json:"errorStats"`

	PricingRules    []pricingRulePayload `json:"pricingRules"`
	PricingFallback pricingRulePayload   `json:"pricingFallback"`
	PricingSource   string               `json:"pricingSource"`

	RiskConfig risk.RiskConfig      `json:"riskConfig"`
	Pressure   risk.PressureSummary `json:"pressure"`

	Notes payloadNotes `json:"notes"`
}

type pricingRulePayload struct {
	Label       string   `json:"label"`
	Patterns    []string `json:"patterns"`
	Input       float64  `json:"input"`
	CacheRead   float64  `json:"cacheRead"`
	CacheCreate float64  `json:"cacheCreate"`
	Output      float64  `json:"output"`
}

type payloadCatalog struct {
	Sessions [][5]any `json:"sessions"`
	Models   []string `json:"models"`
}

type payloadTime struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type payloadNotes struct {
	ThirdPartyDefaultExcluded bool   `json:"thirdPartyDefaultExcluded"`
	SubagentMergeApplied      bool   `json:"subagentMergeApplied"`
	PricingApplied            bool   `json:"pricingApplied"`
	RiskApplied               bool   `json:"riskApplied"`
	Disclaimer                string `json:"disclaimer"`
}

func pricingRulesAsPayload(rules []pricing.PricingRule) []pricingRulePayload {
	out := make([]pricingRulePayload, 0, len(rules))
	for _, r := range rules {
		out = append(out, pricingRulePayload{
			Label:       r.Label,
			Patterns:    r.Patterns,
			Input:       r.Input,
			CacheRead:   r.CacheRead,
			CacheCreate: r.CacheCreate,
			Output:      r.Output,
		})
	}
	return out
}

func pricingFallbackAsPayload(r pricing.PricingRule) pricingRulePayload {
	return pricingRulePayload{
		Label:       r.Label,
		Patterns:    r.Patterns,
		Input:       r.Input,
		CacheRead:   r.CacheRead,
		CacheCreate: r.CacheCreate,
		Output:      r.Output,
	}
}

func sessionsAsArrays(rows []parser.SessionRow) [][5]any {
	out := make([][5]any, 0, len(rows))
	for _, s := range rows {
		out = append(out, [5]any{s.Sid, s.DisplayName, s.PrimaryModel, s.ParentSidIdx, s.EventCount})
	}
	return out
}

func rowsAsArrays(rows []parser.RecordV3) [][10]int64 {
	out := make([][10]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.AsArray())
	}
	return out
}

func failureRowsAsArrays(rows []parser.FailureRecordV3) [][3]int64 {
	out := make([][3]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, [3]int64{r.TsDelta, int64(r.SidIdx), int64(r.ModelIdx)})
	}
	return out
}

func computeAvailableRange(rows [][10]int64, failRows [][3]int64, base int64) payloadTime {
	if len(rows) == 0 && len(failRows) == 0 {
		return payloadTime{}
	}
	first := true
	var minDelta, maxDelta int64
	consider := func(d int64) {
		if first {
			minDelta, maxDelta = d, d
			first = false
			return
		}
		if d < minDelta {
			minDelta = d
		}
		if d > maxDelta {
			maxDelta = d
		}
	}
	for _, r := range rows {
		consider(r[0])
	}
	for _, r := range failRows {
		consider(r[0])
	}
	return payloadTime{Start: base + minDelta, End: base + maxDelta}
}

func writeDataJS(path string, p dataPayload) error {
	buf, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	header := "// AUTO-GENERATED by claudescope generator. DO NOT EDIT BY HAND.\n"
	body := "window.CLAUDESCOPE_DATA = " + string(buf) + ";\n"
	return os.WriteFile(path, []byte(header+body), 0644)
}

func collectJsonl(root string) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(paths)
	return paths, nil
}
