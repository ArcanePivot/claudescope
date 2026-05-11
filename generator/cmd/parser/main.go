// Command parser 是 Phase 3A 的 CLI 入口，演示 jsonl → recordsV3 全流程。
//
// 用法：
//
//	# 跑全部 fixtures，输出每文件 + 聚合统计到 stdout
//	go run ./generator/cmd/parser --fixtures fixtures
//
//	# 扫真实 ~/.claude/projects/，输出脱敏 JSON 到指定文件
//	go run ./generator/cmd/parser --real ~/.claude/projects/ --out probe-real-go.json
//
// 隐私：--real 模式下所有 sid/cwd/agentId 被替换为短 hash 前缀，
// 用户路径只保留 "(redacted-N)"，绝不出现 message 文本。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claudescope/generator/parser"
)

func main() {
	fixturesDir := flag.String("fixtures", "", "fixtures 目录（含 *.jsonl），按文件名排序逐个解析")
	realDir := flag.String("real", "", "真实 ~/.claude/projects/ 路径，递归扫所有 *.jsonl（输出脱敏）")
	outPath := flag.String("out", "", "--real 模式下输出 JSON 文件路径（缺省 stdout）")
	since := flag.String("since", "", "RFC3339 时间，仅保留之后的事件（缺省全量）")
	flag.Parse()

	if *fixturesDir == "" && *realDir == "" {
		fmt.Fprintln(os.Stderr, "用法：--fixtures DIR 或 --real DIR --out FILE")
		os.Exit(2)
	}

	cutoff := time.Time{}
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "解析 --since 失败：%v\n", err)
			os.Exit(2)
		}
		cutoff = t
	}

	if *fixturesDir != "" {
		runFixtures(*fixturesDir, cutoff)
		return
	}

	runReal(*realDir, *outPath, cutoff)
}

// runFixtures 扫 fixtures 目录每个 jsonl，单文件 parse 后还要走一次全局 dedup（Phase 3A v2 P0 修复）。
//
// 输出包含：
//  - 每文件 per-file dedup 数字（便于人工对照单文件预期）
//  - per-file sum 聚合（Merge 累加，看不到跨文件 dup）
//  - 全局 dedup 聚合（ParseClaudeJsonlFiles，看得到跨文件 dup）
func runFixtures(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败：%v\n", dir, err)
		os.Exit(1)
	}

	names := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var perFileAgg parser.ParseResult
	paths := make([]string, 0, len(names))
	fmt.Printf("=== ClaudeScope Phase 3A · fixtures 解析 ===\n")
	fmt.Printf("目录：%s\n\n", dir)

	for _, name := range names {
		path := filepath.Join(dir, name)
		paths = append(paths, path)
		res, err := parser.ParseClaudeJsonl(path, cutoff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s: %v\n", name, err)
			continue
		}
		printPerFile(name, res)
		perFileAgg.Merge(res)
	}

	fmt.Println("=== per-file 聚合（仅 Merge 累加，看不到跨文件 dup）===")
	printResultBlock(perFileAgg)

	globalRes, err := parser.ParseClaudeJsonlFiles(paths, cutoff)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] global parse: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n=== 全局 dedup 聚合（ParseClaudeJsonlFiles，事实层正路）===")
	printResultBlock(globalRes)

	if extra := globalRes.DedupStats.DuplicatesSkipped - perFileAgg.DedupStats.DuplicatesSkipped; extra > 0 {
		fmt.Printf("  跨文件 dedup 多去掉 %d 条（per-file → global）\n", extra)
	}

	cat := parser.BuildCatalog(globalRes.KeptEvents, globalRes.NoUsageErrors)
	base := parser.EarliestTsMs(globalRes.KeptEvents, globalRes.NoUsageErrors)
	rows := parser.BuildRecordsV3(globalRes.KeptEvents, cat, base)
	failRows := parser.BuildFailureRecordsV3(globalRes.NoUsageErrors, cat, base)

	fmt.Printf("\ncatalog.sessions = %d\n", len(cat.Sessions))
	fmt.Printf("catalog.models   = %d (%v)\n", len(cat.Models), cat.Models)
	fmt.Printf("recordsV3        = %d\n", len(rows))
	fmt.Printf("failureRecordsV3 = %d\n", len(failRows))

	flagCount := map[string]int{}
	var tokenTotal int64
	for _, r := range rows {
		tokenTotal += r.Total
		if r.Flags&parser.FlagIsSubagent != 0 {
			flagCount["IS_SUBAGENT"]++
		}
		if r.Flags&parser.FlagIsFailure != 0 {
			flagCount["IS_FAILURE"]++
		}
		if r.Flags&parser.FlagHasError != 0 {
			flagCount["HAS_ERROR"]++
		}
		if r.Flags&parser.FlagIsThirdPartyModel != 0 {
			flagCount["IS_THIRD_PARTY_MODEL"]++
		}
	}
	fmt.Printf("recordsV3 token total = %d\n", tokenTotal)
	fmt.Printf("flag counts: %v\n", flagCount)
}

func printPerFile(name string, res parser.ParseResult) {
	fmt.Printf("--- %s ---\n", name)
	printResultBlock(res)
	fmt.Println()
}

func printResultBlock(res parser.ParseResult) {
	fmt.Printf("  dedupStats     = %+v\n", res.DedupStats)
	fmt.Printf("  filteredCounts = %+v\n", res.FilteredCounts)
	fmt.Printf("  errorStats     = %+v\n", res.ErrorStats)
	fmt.Printf("  keptEvents     = %d\n", len(res.KeptEvents))
	fmt.Printf("  noUsageErrors  = %d\n", len(res.NoUsageErrors))
}

// realOutput 是 --real 模式的脱敏输出结构。
// 不包含 raw events / sid / cwd / agentId，只输出聚合统计 + 模型分布 + flag 分布。
type realOutput struct {
	GeneratedAt        string                  `json:"generatedAt"`
	Root               string                  `json:"root"`               // 已 redact
	FilesScanned       int                     `json:"filesScanned"`
	FilesFailed        int                     `json:"filesFailed"`
	DedupStats         parser.DedupStats       `json:"dedupStats"`
	FilteredCounts     parser.FilteredCounts   `json:"filteredCounts"`
	ErrorStats         parser.ErrorStats       `json:"errorStats"`
	SessionsTotal      int                     `json:"sessionsTotal"`
	ModelsTotal        int                     `json:"modelsTotal"`
	ModelDistribution  []modelStat             `json:"modelDistribution"`
	RecordsV3Total     int                     `json:"recordsV3Total"`
	FailureRecordsV3   int                     `json:"failureRecordsV3"`
	FlagCounts         map[string]int          `json:"flagCounts"`
	TokenTotalsByCol   tokenTotals             `json:"tokenTotalsByCol"`
}

type modelStat struct {
	ModelHash string `json:"modelHash"` // 真实模型名 → sha256 前 8 位
	Family    string `json:"family"`    // claude / non-claude / synthetic（已被 drop 不会出现）
	Rows      int    `json:"rows"`
}

type tokenTotals struct {
	Input       int64 `json:"input"`
	CacheRead   int64 `json:"cacheRead"`
	CacheCreate int64 `json:"cacheCreate"`
	Output      int64 `json:"output"`
	Total       int64 `json:"total"`
}

// runReal 递归扫 root，所有 *.jsonl 全 parse，输出脱敏聚合 JSON。
// **绝不**输出 raw events、cwd、sid、agentId、message 文本。
func runReal(root string, outPath string, cutoff time.Time) {
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析路径失败：%v\n", err)
		os.Exit(1)
	}
	root = abs

	failed := 0
	paths := []string{}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			failed++
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		fmt.Fprintf(os.Stderr, "扫描失败：%v\n", walkErr)
		os.Exit(1)
	}
	sort.Strings(paths)

	// 全局 dedup（Phase 3A v2 P0 修复：跨文件重复必须去掉）
	agg, err := parser.ParseClaudeJsonlFiles(paths, cutoff)
	if err != nil {
		fmt.Fprintf(os.Stderr, "全局解析失败：%v\n", err)
		os.Exit(1)
	}
	scanned := len(paths)

	cat := parser.BuildCatalog(agg.KeptEvents, agg.NoUsageErrors)
	base := parser.EarliestTsMs(agg.KeptEvents, agg.NoUsageErrors)
	rows := parser.BuildRecordsV3(agg.KeptEvents, cat, base)
	failRows := parser.BuildFailureRecordsV3(agg.NoUsageErrors, cat, base)

	flagCounts := map[string]int{}
	var tt tokenTotals
	modelRows := map[int]int{}
	for _, r := range rows {
		tt.Input += r.Input
		tt.CacheRead += r.CacheRead
		tt.CacheCreate += r.CacheCreate
		tt.Output += r.Output
		tt.Total += r.Total
		if r.Flags&parser.FlagIsSubagent != 0 {
			flagCounts["IS_SUBAGENT"]++
		}
		if r.Flags&parser.FlagIsFailure != 0 {
			flagCounts["IS_FAILURE"]++
		}
		if r.Flags&parser.FlagHasError != 0 {
			flagCounts["HAS_ERROR"]++
		}
		if r.Flags&parser.FlagIsThirdPartyModel != 0 {
			flagCounts["IS_THIRD_PARTY_MODEL"]++
		}
		modelRows[r.ModelIdx]++
	}

	dist := make([]modelStat, 0, len(cat.Models))
	for i, m := range cat.Models {
		family := "non-claude"
		if strings.HasPrefix(m, "claude-") {
			family = "claude"
		}
		dist = append(dist, modelStat{
			ModelHash: shortHash(m),
			Family:    family,
			Rows:      modelRows[i],
		})
	}
	sort.Slice(dist, func(i, j int) bool { return dist[i].Rows > dist[j].Rows })

	out := realOutput{
		GeneratedAt:       time.Now().Format(time.RFC3339),
		Root:              redactPath(root),
		FilesScanned:      scanned,
		FilesFailed:       failed,
		DedupStats:        agg.DedupStats,
		FilteredCounts:    agg.FilteredCounts,
		ErrorStats:        agg.ErrorStats,
		SessionsTotal:     len(cat.Sessions),
		ModelsTotal:       len(cat.Models),
		ModelDistribution: dist,
		RecordsV3Total:    len(rows),
		FailureRecordsV3:  len(failRows),
		FlagCounts:        flagCounts,
		TokenTotalsByCol:  tt,
	}

	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON 编码失败：%v\n", err)
		os.Exit(1)
	}

	if outPath == "" {
		os.Stdout.Write(buf)
		os.Stdout.Write([]byte("\n"))
		return
	}

	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "写 %s 失败：%v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已写入 %s（%d 字节，已脱敏）\n", outPath, len(buf))
}

// shortHash 把任意字符串映射到 sha256 前 8 hex 位，用于输出脱敏的稳定 ID。
func shortHash(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// redactPath 把绝对路径替换成只保留尾部 1 段的形式，加 hash 防碰撞。
func redactPath(p string) string {
	base := filepath.Base(strings.TrimRight(p, string(filepath.Separator)))
	return fmt.Sprintf("(redacted)/%s#%s", base, shortHash(p))
}
