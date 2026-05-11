package parser

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixturesDir = "../../fixtures"

func parseFixture(t *testing.T, name string) ParseResult {
	t.Helper()
	path := filepath.Join(fixturesDir, name)
	res, err := ParseClaudeJsonl(path, time.Time{})
	if err != nil {
		t.Fatalf("ParseClaudeJsonl(%s) error: %v", name, err)
	}
	return res
}

func TestEmpty(t *testing.T) {
	res := parseFixture(t, "empty.jsonl")
	if got := res.DedupStats.RawUsageRows; got != 0 {
		t.Errorf("empty rawUsageRows = %d, want 0", got)
	}
	if len(res.KeptEvents) != 0 {
		t.Errorf("empty keptEvents = %d, want 0", len(res.KeptEvents))
	}
	if len(res.NoUsageErrors) != 0 {
		t.Errorf("empty noUsageErrors = %d, want 0", len(res.NoUsageErrors))
	}
}

func TestMainSession(t *testing.T) {
	res := parseFixture(t, "main-session.jsonl")
	want := DedupStats{
		RawUsageRows: 3, KeptUsageRows: 3, DuplicatesSkipped: 0,
		UuidFallbackRows: 3, WeakKeyRows: 0,
	}
	if res.DedupStats != want {
		t.Errorf("main-session DedupStats = %+v, want %+v", res.DedupStats, want)
	}
	if res.FilteredCounts.SubagentRows != 0 {
		t.Errorf("main-session subagentRows = %d, want 0", res.FilteredCounts.SubagentRows)
	}
	if res.FilteredCounts.Synthetic != 0 || res.FilteredCounts.ThirdParty != 0 {
		t.Errorf("main-session unexpected filteredCounts: %+v", res.FilteredCounts)
	}
}

// TestSubagent — 孤儿 subagent（父 session 不在本次扫描里）→ 回退到不合并，
// SidIdx == SourceSidIdx。这条路径保护 M4 不让"父缺失"破掉统计。
func TestSubagent(t *testing.T) {
	res := parseFixture(t, "subagent.jsonl")
	want := DedupStats{2, 2, 0, 2, 0}
	if res.DedupStats != want {
		t.Errorf("subagent DedupStats = %+v, want %+v", res.DedupStats, want)
	}
	if res.FilteredCounts.SubagentRows != 2 {
		t.Errorf("subagent subagentRows = %d, want 2", res.FilteredCounts.SubagentRows)
	}

	cat := BuildCatalog(res.KeptEvents, res.NoUsageErrors)
	base := EarliestTsMs(res.KeptEvents, res.NoUsageErrors)
	rows := BuildRecordsV3(res.KeptEvents, cat, base)
	if len(rows) != 2 {
		t.Fatalf("subagent recordsV3 len = %d, want 2", len(rows))
	}
	for i, r := range rows {
		if r.Flags&FlagIsSubagent == 0 {
			t.Errorf("row[%d] missing IS_SUBAGENT flag, flags=0x%02x", i, r.Flags)
		}
		// M4 孤儿 fallback：父不在 catalog → SidIdx 等于 SourceSidIdx（不合并）
		if r.SourceSidIdx != r.SidIdx {
			t.Errorf("row[%d] sourceSidIdx=%d sidIdx=%d, 孤儿 subagent 应回退自引用",
				i, r.SourceSidIdx, r.SidIdx)
		}
	}
}

// TestSubagentMergedWithParent — 父主会话 + 子代理同时存在 → M4 真合并：
// SidIdx 指向父 catalog 行，SourceSidIdx 指向 subagent 自己。前端用 SourceSidIdx
// 做 [展开子代理] 还原；按 SidIdx 聚合自然把子代理 token 计入主会话。
func TestSubagentMergedWithParent(t *testing.T) {
	pathMain := filepath.Join(fixturesDir, "main-session.jsonl")
	pathSub := filepath.Join(fixturesDir, "subagent.jsonl")
	res, err := ParseClaudeJsonlFiles([]string{pathMain, pathSub}, time.Time{})
	if err != nil {
		t.Fatalf("ParseClaudeJsonlFiles: %v", err)
	}
	if len(res.KeptEvents) != 5 {
		t.Fatalf("KeptEvents = %d, want 5（main 3 + sub 2）", len(res.KeptEvents))
	}

	cat := BuildCatalog(res.KeptEvents, res.NoUsageErrors)
	if len(cat.Sessions) != 2 {
		t.Fatalf("catalog sessions = %d, want 2（main + 1 subagent）", len(cat.Sessions))
	}

	// 找出 subagent 那一行 + 找出主 session 行
	var subIdx, mainIdx int = -1, -1
	for i, s := range cat.Sessions {
		if strings.HasPrefix(s.Sid, "agent-") {
			subIdx = i
		} else {
			mainIdx = i
		}
	}
	if subIdx < 0 || mainIdx < 0 {
		t.Fatalf("找不到 subagent 或 main 行：%+v", cat.Sessions)
	}

	// catalog：subagent 的 ParentSidIdx 必须指向 main
	if cat.Sessions[subIdx].ParentSidIdx != mainIdx {
		t.Errorf("subagent.ParentSidIdx = %d, want %d (main)", cat.Sessions[subIdx].ParentSidIdx, mainIdx)
	}
	// catalog：main 的 ParentSidIdx 仍自引用
	if cat.Sessions[mainIdx].ParentSidIdx != mainIdx {
		t.Errorf("main.ParentSidIdx = %d, want self %d", cat.Sessions[mainIdx].ParentSidIdx, mainIdx)
	}

	base := EarliestTsMs(res.KeptEvents, res.NoUsageErrors)
	rows := BuildRecordsV3(res.KeptEvents, cat, base)
	if len(rows) != 5 {
		t.Fatalf("recordsV3 len = %d, want 5", len(rows))
	}

	subagentRows := 0
	for _, r := range rows {
		if r.Flags&FlagIsSubagent != 0 {
			subagentRows++
			if r.SidIdx != mainIdx {
				t.Errorf("subagent row.SidIdx = %d, want main %d (M4 真合并)", r.SidIdx, mainIdx)
			}
			if r.SourceSidIdx != subIdx {
				t.Errorf("subagent row.SourceSidIdx = %d, want subagent %d (M4 保留自身索引)", r.SourceSidIdx, subIdx)
			}
		} else {
			if r.SidIdx != mainIdx || r.SourceSidIdx != mainIdx {
				t.Errorf("main row.SidIdx/SourceSidIdx = %d/%d, want %d/%d",
					r.SidIdx, r.SourceSidIdx, mainIdx, mainIdx)
			}
		}
	}
	if subagentRows != 2 {
		t.Errorf("subagent 行数 = %d, want 2", subagentRows)
	}

	// 按 SidIdx 聚合：所有 5 条都应进 main，token 总和 = main 三条 + sub 两条
	var mergedMain int64
	for _, r := range rows {
		if r.SidIdx == mainIdx {
			mergedMain += r.Total
		}
	}
	// main: (1234+8000+5000+250) + (420+13200+0+380) + (880+15600+2400+1100) = 14484 + 14000 + 19980 = 48464
	// sub:  (800+2000+0+150) + (300+2300+0+90) = 2950 + 2690 = 5640
	const wantMerged = int64(48464 + 5640)
	if mergedMain != wantMerged {
		t.Errorf("合并后 main token 总和 = %d, want %d", mergedMain, wantMerged)
	}
}

func TestSynthetic(t *testing.T) {
	res := parseFixture(t, "synthetic.jsonl")
	if res.FilteredCounts.Synthetic != 2 {
		t.Errorf("synthetic dropped = %d, want 2", res.FilteredCounts.Synthetic)
	}
	want := DedupStats{1, 1, 0, 1, 0}
	if res.DedupStats != want {
		t.Errorf("synthetic DedupStats = %+v, want %+v", res.DedupStats, want)
	}
	for _, e := range res.KeptEvents {
		if e.Model == "<synthetic>" {
			t.Errorf("synthetic event leaked into KeptEvents: %+v", e)
		}
	}
	// 真实 opus 行应保留
	if len(res.KeptEvents) != 1 || res.KeptEvents[0].Model != "claude-opus-4-7" {
		t.Errorf("expected exactly 1 kept event with model=claude-opus-4-7, got %+v", res.KeptEvents)
	}
}

func TestApiError(t *testing.T) {
	res := parseFixture(t, "api-error.jsonl")

	// 错误1 (usage 全 0) + 成功 → kept (2)
	// 错误2 (无 usage)              → noUsageErrors (1)
	wantDedup := DedupStats{2, 2, 0, 2, 0}
	if res.DedupStats != wantDedup {
		t.Errorf("api-error DedupStats = %+v, want %+v", res.DedupStats, wantDedup)
	}
	if len(res.NoUsageErrors) != 1 {
		t.Errorf("NoUsageErrors len = %d, want 1", len(res.NoUsageErrors))
	}
	wantErr := ErrorStats{ApiErrorsTotal: 2, ApiErrorsWithUsage: 1, ApiErrorsNoUsage: 1}
	if res.ErrorStats != wantErr {
		t.Errorf("api-error ErrorStats = %+v, want %+v", res.ErrorStats, wantErr)
	}

	cat := BuildCatalog(res.KeptEvents, res.NoUsageErrors)
	base := EarliestTsMs(res.KeptEvents, res.NoUsageErrors)
	rows := BuildRecordsV3(res.KeptEvents, cat, base)

	// 至少一行带 IS_FAILURE | HAS_ERROR
	failureCount := 0
	for _, r := range rows {
		if r.Flags&FlagIsFailure != 0 && r.Flags&FlagHasError != 0 {
			failureCount++
			if r.Total != 0 || r.Input != 0 || r.Output != 0 {
				t.Errorf("usage=0 failure row should have all-zero tokens, got %+v", r)
			}
		}
	}
	if failureCount != 1 {
		t.Errorf("recordsV3 IS_FAILURE|HAS_ERROR rows = %d, want 1", failureCount)
	}

	// failureRecordsV3 = 1 (无 usage 错误)
	failRows := BuildFailureRecordsV3(res.NoUsageErrors, cat, base)
	if len(failRows) != 1 {
		t.Errorf("failureRecordsV3 len = %d, want 1", len(failRows))
	}

	// 无 usage 错误行不进 token total：检查 main rows token 总和
	var tokenTotal int64
	for _, r := range rows {
		tokenTotal += r.Total
	}
	// 期望：错误1 全 0，成功行 600+0+1500+120 = 2220
	if tokenTotal != 2220 {
		t.Errorf("api-error rows token total = %d, want 2220 (only success row contributes)", tokenTotal)
	}
}

func TestDuplicate(t *testing.T) {
	res := parseFixture(t, "duplicate-message.jsonl")
	want := DedupStats{
		RawUsageRows: 5, KeptUsageRows: 3, DuplicatesSkipped: 2,
		UuidFallbackRows: 1, WeakKeyRows: 0,
	}
	if res.DedupStats != want {
		t.Errorf("duplicate DedupStats = %+v, want %+v", res.DedupStats, want)
	}
	if len(res.KeptEvents) != 3 {
		t.Errorf("KeptEvents len = %d, want 3", len(res.KeptEvents))
	}
}

func TestThirdParty(t *testing.T) {
	res := parseFixture(t, "third-party-history.jsonl")
	want := DedupStats{3, 3, 0, 0, 0} // 全部带 strong key（含 requestId）
	if res.DedupStats != want {
		t.Errorf("third-party DedupStats = %+v, want %+v", res.DedupStats, want)
	}
	if res.FilteredCounts.ThirdParty != 2 {
		t.Errorf("thirdParty count = %d, want 2", res.FilteredCounts.ThirdParty)
	}

	cat := BuildCatalog(res.KeptEvents, res.NoUsageErrors)
	base := EarliestTsMs(res.KeptEvents, res.NoUsageErrors)
	rows := BuildRecordsV3(res.KeptEvents, cat, base)
	if len(rows) != 3 {
		t.Fatalf("third-party recordsV3 len = %d, want 3", len(rows))
	}

	thirdPartyRows := 0
	nativeRows := 0
	for _, r := range rows {
		if r.Flags&FlagIsThirdPartyModel != 0 {
			thirdPartyRows++
		} else {
			nativeRows++
		}
	}
	if thirdPartyRows != 2 {
		t.Errorf("rows with IS_THIRD_PARTY_MODEL flag = %d, want 2", thirdPartyRows)
	}
	if nativeRows != 1 {
		t.Errorf("native (claude-*) rows = %d, want 1", nativeRows)
	}
}

// allFixtureNames 是 fixture 目录里所有 *.jsonl 的稳定列表（含 v2 P0 修复后新加的跨文件对）。
var allFixtureNames = []string{
	"empty.jsonl",
	"main-session.jsonl",
	"subagent.jsonl",
	"synthetic.jsonl",
	"api-error.jsonl",
	"duplicate-message.jsonl",
	"third-party-history.jsonl",
	"duplicate-cross-file-a.jsonl",
	"duplicate-cross-file-b.jsonl",
}

func allFixturePaths() []string {
	out := make([]string, 0, len(allFixtureNames))
	for _, n := range allFixtureNames {
		out = append(out, filepath.Join(fixturesDir, n))
	}
	return out
}

// TestAllFixturesAggregatePerFile 旧聚合路径（每文件先 dedup 再 sum）。
//
// 这个测试仅验证 ParseResult.Merge 累加正确，**不**验证全局 dedup（那是 P0 漏洞）。
// 真正的全局 dedup 由 TestAllFixturesAggregateGlobalDedup 验证。
func TestAllFixturesAggregatePerFile(t *testing.T) {
	var agg ParseResult
	for _, name := range allFixtureNames {
		res, err := ParseClaudeJsonl(filepath.Join(fixturesDir, name), time.Time{})
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		agg.Merge(res)
	}

	// 单文件路径下：A、B 两个跨文件 fixture 各自独立保留 1 条（strong 唯一）→ kept 各 1
	wantDedup := DedupStats{
		RawUsageRows:      0 + 3 + 2 + 1 + 2 + 5 + 3 + 1 + 1, // 18
		KeptUsageRows:     0 + 3 + 2 + 1 + 2 + 3 + 3 + 1 + 1, // 16
		DuplicatesSkipped: 0 + 0 + 0 + 0 + 0 + 2 + 0 + 0 + 0, // 2 (per-file 看不到跨文件 dup)
		UuidFallbackRows:  0 + 3 + 2 + 1 + 2 + 1 + 0 + 0 + 0, // 9 (cross-file 行有 strong key)
		WeakKeyRows:       0,
	}
	if agg.DedupStats != wantDedup {
		t.Errorf("per-file aggregate DedupStats = %+v, want %+v", agg.DedupStats, wantDedup)
	}
}

// TestAllFixturesAggregateGlobalDedup 全局 dedup 路径（v2 P0 修复后的正路）。
//
// 关键差异：跨文件 strong key 重复必须在全局层面被去掉。
func TestAllFixturesAggregateGlobalDedup(t *testing.T) {
	res, err := ParseClaudeJsonlFiles(allFixturePaths(), time.Time{})
	if err != nil {
		t.Fatalf("ParseClaudeJsonlFiles: %v", err)
	}

	// 全局 dedup：A+B 共 2 条 raw，去重后只剩 1 → 跨文件 dup +1
	wantDedup := DedupStats{
		RawUsageRows:      18,
		KeptUsageRows:     15, // 16 - 1（跨文件去重）
		DuplicatesSkipped: 3,  // 2（文件内）+ 1（跨文件）
		UuidFallbackRows:  9,
		WeakKeyRows:       0,
	}
	if res.DedupStats != wantDedup {
		t.Errorf("global aggregate DedupStats = %+v, want %+v", res.DedupStats, wantDedup)
	}

	wantFiltered := FilteredCounts{
		Synthetic:    2,
		Malformed:    0,
		SubagentRows: 2,
		ThirdParty:   2,
	}
	if res.FilteredCounts != wantFiltered {
		t.Errorf("global FilteredCounts = %+v, want %+v", res.FilteredCounts, wantFiltered)
	}

	wantErr := ErrorStats{ApiErrorsTotal: 2, ApiErrorsWithUsage: 1, ApiErrorsNoUsage: 1}
	if res.ErrorStats != wantErr {
		t.Errorf("global ErrorStats = %+v, want %+v", res.ErrorStats, wantErr)
	}
}

// TestDedupAcrossFiles 钦定 P0 验证：两文件各 1 条相同 strong key，全局只保留 1 条。
//
// 子断言：
//  1. 单文件分别 parse 时各自保留 1 条（per-file 看不到 dup）
//  2. ParseClaudeJsonlFiles 全局 dedup 时只保留 1 条，duplicatesSkipped=1
func TestDedupAcrossFiles(t *testing.T) {
	pathA := filepath.Join(fixturesDir, "duplicate-cross-file-a.jsonl")
	pathB := filepath.Join(fixturesDir, "duplicate-cross-file-b.jsonl")

	// 子断言 1：per-file
	resA, err := ParseClaudeJsonl(pathA, time.Time{})
	if err != nil {
		t.Fatalf("parse A: %v", err)
	}
	resB, err := ParseClaudeJsonl(pathB, time.Time{})
	if err != nil {
		t.Fatalf("parse B: %v", err)
	}
	if resA.DedupStats.KeptUsageRows != 1 || resB.DedupStats.KeptUsageRows != 1 {
		t.Errorf("per-file kept != 1 (A=%d B=%d)", resA.DedupStats.KeptUsageRows, resB.DedupStats.KeptUsageRows)
	}

	// 子断言 2：全局 dedup
	res, err := ParseClaudeJsonlFiles([]string{pathA, pathB}, time.Time{})
	if err != nil {
		t.Fatalf("ParseClaudeJsonlFiles: %v", err)
	}
	want := DedupStats{
		RawUsageRows:      2,
		KeptUsageRows:     1,
		DuplicatesSkipped: 1,
		UuidFallbackRows:  0,
		WeakKeyRows:       0,
	}
	if res.DedupStats != want {
		t.Errorf("global cross-file DedupStats = %+v, want %+v", res.DedupStats, want)
	}
	if len(res.KeptEvents) != 1 {
		t.Errorf("global cross-file KeptEvents = %d, want 1", len(res.KeptEvents))
	}
}

// TestBuildCatalogConsistency catalog 索引应正确反映 sessions/models
func TestBuildCatalogConsistency(t *testing.T) {
	res := parseFixture(t, "third-party-history.jsonl")
	cat := BuildCatalog(res.KeptEvents, res.NoUsageErrors)

	// 1 个 session（同 sessionId，不同 model）
	if len(cat.Sessions) != 1 {
		t.Errorf("third-party sessions = %d, want 1", len(cat.Sessions))
	}
	// 3 个 model：gpt-5.4 / kimi-k2.6 / claude-sonnet-4-6
	if len(cat.Models) != 3 {
		t.Errorf("third-party models = %d, want 3, got %v", len(cat.Models), cat.Models)
	}
	// 自引用：ParentSidIdx == 自己 idx
	for i, s := range cat.Sessions {
		if s.ParentSidIdx != i {
			t.Errorf("session[%d] ParentSidIdx = %d, want self-ref %d", i, s.ParentSidIdx, i)
		}
	}
	// EventCount 总和 = KeptEvents 长度
	var total int
	for _, s := range cat.Sessions {
		total += s.EventCount
	}
	if total != len(res.KeptEvents)+len(res.NoUsageErrors) {
		t.Errorf("session eventCount total = %d, want %d (kept+noUsage)",
			total, len(res.KeptEvents)+len(res.NoUsageErrors))
	}
}
