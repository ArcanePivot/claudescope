// Package parser 实现 ClaudeScope M1：jsonl → ClaudeUsageEvent → recordsV3。
//
// 契约对应文档：
//   - docs/schema-v3.md       §1 顶层 dedupStats / filteredCounts
//   - docs/schema-v3.md       §2 recordsV3 10 列 + flags 位
//   - docs/schema-v3.md       §5.1 dedupKey 三级优先级
//   - docs/schema-v2-compat.md V3 catalog 5 列
//   - fixtures/README.md      7 fixture 期望行为
package parser

import "time"

// Flag 位 — 与 docs/schema-v3.md §2.1 同步
const (
	FlagIsSubagent        = 0x01
	FlagWasSynthetic      = 0x02
	FlagIsFailure         = 0x04
	FlagHasError          = 0x08
	FlagIsThirdPartyModel = 0x10
)

// ErrorKind 枚举（v1.0.1）：把 api-error 行的错误类型粗分类，用于 rate-limit signal。
// 只从 message.content[0].text 的前缀里识别——这段文本是 Claude Code 客户端归类后的标签，
// 不是用户 prompt，可以安全提取。
const (
	ErrorKindRateLimit  = "rate_limit"
	ErrorKindOverloaded = "overloaded"
	ErrorKindTimeout    = "timeout"
	ErrorKindOther      = "other"
)

// ClaudeUsageEvent 是单行 jsonl 解析后的中间事件。
// 字段命名遵循"原 jsonl 字段名小写 → Go Pascal 大写"约定。
type ClaudeUsageEvent struct {
	Ts           time.Time
	Sid          string // sessionId
	AgentID      string // agentId（仅子代理出现）
	IsSidechain  bool
	Uuid         string // 行 uuid，dedup uuid fallback 用
	MessageID    string // message.id，强键之一
	RequestID    string // requestId，强键之一（probe 实测仅 71.8% 覆盖）
	Cwd          string // 项目路径，用于 displayName
	Model        string
	Input        int64
	CacheRead    int64
	CacheCreate  int64
	Output       int64
	HasUsage     bool // message.usage 块是否存在
	IsApiError   bool // 顶层 isApiErrorMessage:true
	IsThirdParty bool // 非 claude-* 前缀（且非 <synthetic>）
	ErrorKind    string // v1.0.1: rate_limit / overloaded / timeout / other（仅 IsApiError 行有效，否则空）
	SourceFile   string
}

// Total = input + cacheRead + cacheCreate + output（与 schema-v3.md §2 第 7 列一致）
func (e ClaudeUsageEvent) Total() int64 {
	return e.Input + e.CacheRead + e.CacheCreate + e.Output
}

// FilteredCounts 累计被过滤但仍要 surface 的行数（schema-v3.md §1）
type FilteredCounts struct {
	Synthetic    int `json:"synthetic"`
	Malformed    int `json:"malformed"`
	SubagentRows int `json:"subagentRows"`
	ThirdParty   int `json:"thirdParty"`
}

// DedupStats 去重统计（schema-v3.md §1 + §5.1）
type DedupStats struct {
	RawUsageRows      int `json:"rawUsageRows"`
	KeptUsageRows     int `json:"keptUsageRows"`
	DuplicatesSkipped int `json:"duplicatesSkipped"`
	UuidFallbackRows  int `json:"uuidFallbackRows"`
	WeakKeyRows       int `json:"weakKeyRows"`
}

// ErrorStats API 错误两分支统计
type ErrorStats struct {
	ApiErrorsTotal     int `json:"apiErrorsTotal"`
	ApiErrorsWithUsage int `json:"apiErrorsWithUsage"` // → recordsV3 with IS_FAILURE|HAS_ERROR
	ApiErrorsNoUsage   int `json:"apiErrorsNoUsage"`   // → failureRecordsV3
}

// ParseResult 是 dedup 后的解析结果（单文件包装 / 多文件全局聚合）
type ParseResult struct {
	KeptEvents     []ClaudeUsageEvent `json:"-"` // 去重后保留的有效事件
	NoUsageErrors  []ClaudeUsageEvent `json:"-"` // 无 usage 的 api-error 行
	DedupStats     DedupStats         `json:"dedupStats"`
	FilteredCounts FilteredCounts     `json:"filteredCounts"`
	ErrorStats     ErrorStats         `json:"errorStats"`
}

// RawParseResult 是 ParseClaudeJsonlRaw 的输出：未 dedup 的单文件原始事件流。
// 多文件全局 dedup 通过把多个 RawParseResult 的 RawEvents 拼起来再调一次 DedupEvents 实现。
type RawParseResult struct {
	RawEvents      []ClaudeUsageEvent
	NoUsageErrors  []ClaudeUsageEvent
	FilteredCounts FilteredCounts
	ErrorStats     ErrorStats
}

// Merge 把 other 累加到当前结果（用于多文件聚合）
func (r *ParseResult) Merge(other ParseResult) {
	r.KeptEvents = append(r.KeptEvents, other.KeptEvents...)
	r.NoUsageErrors = append(r.NoUsageErrors, other.NoUsageErrors...)
	r.DedupStats.RawUsageRows += other.DedupStats.RawUsageRows
	r.DedupStats.KeptUsageRows += other.DedupStats.KeptUsageRows
	r.DedupStats.DuplicatesSkipped += other.DedupStats.DuplicatesSkipped
	r.DedupStats.UuidFallbackRows += other.DedupStats.UuidFallbackRows
	r.DedupStats.WeakKeyRows += other.DedupStats.WeakKeyRows
	r.FilteredCounts.Synthetic += other.FilteredCounts.Synthetic
	r.FilteredCounts.Malformed += other.FilteredCounts.Malformed
	r.FilteredCounts.SubagentRows += other.FilteredCounts.SubagentRows
	r.FilteredCounts.ThirdParty += other.FilteredCounts.ThirdParty
	r.ErrorStats.ApiErrorsTotal += other.ErrorStats.ApiErrorsTotal
	r.ErrorStats.ApiErrorsWithUsage += other.ErrorStats.ApiErrorsWithUsage
	r.ErrorStats.ApiErrorsNoUsage += other.ErrorStats.ApiErrorsNoUsage
}

// SessionRow catalog.sessions 的一行（schema-v3.md §3 V3 5 列）
type SessionRow struct {
	Sid          string
	DisplayName  string
	PrimaryModel string
	ParentSidIdx int // Phase 3A 不合并 → 自引用
	EventCount   int
}

// Catalog 顶层 catalog 字段
type Catalog struct {
	Sessions []SessionRow
	Models   []string
}

// RecordV3 单行 recordsV3（schema-v3.md §2，10 列）
type RecordV3 struct {
	TsDelta      int64
	SidIdx       int
	ModelIdx     int
	Input        int64
	CacheRead    int64
	CacheCreate  int64
	Output       int64
	Total        int64
	SourceSidIdx int // Phase 3A 不合并 → 等于 SidIdx
	Flags        int
}

// AsArray 把行序列化为 schema-v3.md §2 文本里的 JSON array 形式
// （前端 decodeUsageRowsV3 直接读这个 10 元数组）
func (r RecordV3) AsArray() [10]int64 {
	return [10]int64{
		r.TsDelta,
		int64(r.SidIdx),
		int64(r.ModelIdx),
		r.Input,
		r.CacheRead,
		r.CacheCreate,
		r.Output,
		r.Total,
		int64(r.SourceSidIdx),
		int64(r.Flags),
	}
}

// FailureRecordV3 无 usage 的 api-error 行（schema-v3.md §6）
type FailureRecordV3 struct {
	TsDelta  int64
	SidIdx   int
	ModelIdx int
}
