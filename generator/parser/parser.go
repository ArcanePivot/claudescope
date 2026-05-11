package parser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// ParseClaudeJsonlRaw 解析单个 jsonl 文件**但不做 dedup**，返回原始事件流。
//
// 调用方（多文件场景）应汇总所有文件的 RawEvents 后再调用 DedupEvents，
// 才能得到全局 dedup 结果（schema-v3.md §5.2：dedup 作用域是扫描范围）。
//
// 单文件 fixture / 便利场景请用 ParseClaudeJsonl（内部组合 Raw + DedupEvents）。
//
// 隐私：本函数**绝不**读取 message.content 文本字段；只读 usage / 标识 / 错误标记。
func ParseClaudeJsonlRaw(path string, cutoff time.Time) (RawParseResult, error) {
	res := RawParseResult{}

	f, err := os.Open(path)
	if err != nil {
		return res, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// 单行容量上限 16MB（应对极长 assistant 响应）
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	sourceFile := filepath.Base(path)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if !gjson.Valid(line) {
			res.FilteredCounts.Malformed++
			continue
		}

		t := gjson.Get(line, "type").String()
		if t != "assistant" {
			continue
		}

		e := extractEvent(line)
		e.SourceFile = sourceFile

		if !cutoff.IsZero() && e.Ts.Before(cutoff) {
			continue
		}

		// <synthetic> drop（schema-v3.md：不进 dedup，filteredCounts.synthetic 累加）
		if e.Model == "<synthetic>" {
			res.FilteredCounts.Synthetic++
			continue
		}

		// API error 无 usage 分支（fixtures/README.md 第 4 条）
		if e.IsApiError && !e.HasUsage {
			res.NoUsageErrors = append(res.NoUsageErrors, e)
			res.ErrorStats.ApiErrorsTotal++
			res.ErrorStats.ApiErrorsNoUsage++
			continue
		}

		// 非 api-error 但缺 usage：算 malformed，不进主流
		if !e.HasUsage {
			res.FilteredCounts.Malformed++
			continue
		}

		// 子代理统计（仅计原始 raw subagent 行，dedup 前）
		if e.IsSidechain {
			res.FilteredCounts.SubagentRows++
		}
		if e.IsThirdParty {
			res.FilteredCounts.ThirdParty++
		}
		if e.IsApiError {
			res.ErrorStats.ApiErrorsTotal++
			res.ErrorStats.ApiErrorsWithUsage++
		}

		res.RawEvents = append(res.RawEvents, e)
	}

	if err := sc.Err(); err != nil {
		return res, fmt.Errorf("scan %s: %w", path, err)
	}

	return res, nil
}

// ParseClaudeJsonl 单文件便利包装：raw + dedup 一步到位。
//
// **多文件场景请勿逐文件调用本函数后 Merge**——那会漏跨文件重复。
// 多文件请用 ParseClaudeJsonlFiles 走全局 dedup 路径。
func ParseClaudeJsonl(path string, cutoff time.Time) (ParseResult, error) {
	raw, err := ParseClaudeJsonlRaw(path, cutoff)
	if err != nil {
		return ParseResult{}, err
	}
	return assembleParseResult([]RawParseResult{raw}), nil
}

// ParseClaudeJsonlFiles 多文件全局 dedup 入口（Phase 3A v2 P0 修复）。
//
// 流程：
//  1. 对每个文件调用 ParseClaudeJsonlRaw，收集 RawEvents/NoUsageErrors/计数
//  2. 全部 raw events 汇总后调用一次 DedupEvents，获得真正的全局 dedup
//
// 任一文件解析失败立即返回错误（不静默吞）。
func ParseClaudeJsonlFiles(paths []string, cutoff time.Time) (ParseResult, error) {
	rawResults := make([]RawParseResult, 0, len(paths))
	for _, p := range paths {
		raw, err := ParseClaudeJsonlRaw(p, cutoff)
		if err != nil {
			return ParseResult{}, fmt.Errorf("parse %s: %w", p, err)
		}
		rawResults = append(rawResults, raw)
	}
	return assembleParseResult(rawResults), nil
}

func assembleParseResult(rawResults []RawParseResult) ParseResult {
	res := ParseResult{}
	for _, r := range rawResults {
		res.NoUsageErrors = append(res.NoUsageErrors, r.NoUsageErrors...)
		res.FilteredCounts.Synthetic += r.FilteredCounts.Synthetic
		res.FilteredCounts.Malformed += r.FilteredCounts.Malformed
		res.FilteredCounts.SubagentRows += r.FilteredCounts.SubagentRows
		res.FilteredCounts.ThirdParty += r.FilteredCounts.ThirdParty
		res.ErrorStats.ApiErrorsTotal += r.ErrorStats.ApiErrorsTotal
		res.ErrorStats.ApiErrorsWithUsage += r.ErrorStats.ApiErrorsWithUsage
		res.ErrorStats.ApiErrorsNoUsage += r.ErrorStats.ApiErrorsNoUsage
	}
	// 汇总后一次性 dedup（全局作用域）
	all := make([]ClaudeUsageEvent, 0)
	for _, r := range rawResults {
		all = append(all, r.RawEvents...)
	}
	kept, stats := DedupEvents(all)
	res.KeptEvents = kept
	res.DedupStats = stats
	return res
}

func extractEvent(line string) ClaudeUsageEvent {
	e := ClaudeUsageEvent{}
	e.Sid = gjson.Get(line, "sessionId").String()
	e.AgentID = gjson.Get(line, "agentId").String()
	e.IsSidechain = gjson.Get(line, "isSidechain").Bool()
	e.Uuid = gjson.Get(line, "uuid").String()
	e.RequestID = gjson.Get(line, "requestId").String()
	e.Cwd = gjson.Get(line, "cwd").String()
	e.IsApiError = gjson.Get(line, "isApiErrorMessage").Bool()

	if ts := gjson.Get(line, "timestamp").String(); ts != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			e.Ts = parsed
		}
	}

	e.MessageID = gjson.Get(line, "message.id").String()
	e.Model = gjson.Get(line, "message.model").String()

	usage := gjson.Get(line, "message.usage")
	if usage.Exists() && usage.IsObject() {
		e.HasUsage = true
		// 只取 4 个 token 字段，其它 metadata 静默忽略（probe 实测 §10.4）
		e.Input = usage.Get("input_tokens").Int()
		e.CacheRead = usage.Get("cache_read_input_tokens").Int()
		e.CacheCreate = usage.Get("cache_creation_input_tokens").Int()
		e.Output = usage.Get("output_tokens").Int()
	}

	// 第三方/历史模型识别：非 claude-* 前缀且非 <synthetic>
	if e.Model != "" && !strings.HasPrefix(e.Model, "claude-") && e.Model != "<synthetic>" {
		e.IsThirdParty = true
	}

	return e
}
