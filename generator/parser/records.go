package parser

import "strings"

// BuildCatalog 由 KeptEvents + NoUsageErrors 推断 sessions / models。
//
// Phase 3C-M4：subagent 父子真合并 — 每个独立 sessionKey 仍单独占一行（子代理用
// "agent-<agentId>" 作 key 与主 session 区分，便于前端 [展开子代理] toggle 还原），
// 但子代理 SessionRow 的 ParentSidIdx 指向其父 session 的 catalog idx；主 session
// 自身的 ParentSidIdx 仍自引用。
//
// 父查找规则：subagent 事件的 Sid 字段直接是父 session id（Anthropic Claude Code
// 在子代理上下文里复用父 sessionId）。若父 session 在本次扫描里完全缺失（父 jsonl
// 被删 / cutoff 排除），fallback 到自引用，避免悬空 idx。
func BuildCatalog(kept []ClaudeUsageEvent, noUsageErrors []ClaudeUsageEvent) Catalog {
	sessionIdx := map[string]int{}
	mainSidToIdx := map[string]int{} // sessionId → 主 session catalog 行（只记录非 sidechain 出处）
	modelIdx := map[string]int{}
	sessions := []SessionRow{}
	models := []string{}

	register := func(e ClaudeUsageEvent) {
		key := sessionKeyFor(e)
		if _, ok := sessionIdx[key]; !ok {
			sessions = append(sessions, SessionRow{
				Sid:          key,
				DisplayName:  displayNameFor(e),
				PrimaryModel: e.Model,
				ParentSidIdx: len(sessions), // 先自引用，下文回填子代理
				EventCount:   0,
			})
			sessionIdx[key] = len(sessions) - 1
		}
		// 主 session 出现 → 记录 sessionId → catalog idx，用于子代理回填父
		if !e.IsSidechain && e.Sid != "" {
			if _, exists := mainSidToIdx[e.Sid]; !exists {
				mainSidToIdx[e.Sid] = sessionIdx[key]
			}
		}
		if e.Model != "" {
			if _, ok := modelIdx[e.Model]; !ok {
				models = append(models, e.Model)
				modelIdx[e.Model] = len(models) - 1
			}
		}
		idx := sessionIdx[key]
		sessions[idx].EventCount++
	}

	for _, e := range kept {
		register(e)
	}
	for _, e := range noUsageErrors {
		register(e)
	}

	// 第二轮：把每个 subagent SessionRow 的 ParentSidIdx 指向真父；父缺失则保持自引用
	for i := range sessions {
		row := &sessions[i]
		if !strings.HasPrefix(row.Sid, "agent-") {
			continue
		}
		parentSid := mainSidForKey(row.Sid, kept, noUsageErrors)
		if parentSid == "" {
			continue
		}
		if parentIdx, ok := mainSidToIdx[parentSid]; ok && parentIdx != i {
			row.ParentSidIdx = parentIdx
		}
	}

	return Catalog{Sessions: sessions, Models: models}
}

// mainSidForKey 在事件流里找一个 subagent key 对应的父 sessionId。
// subagent 的 Sid 字段直接是父 sessionId（Claude Code 复用），所以扫到第一条同 key 的
// 子代理事件就能拿父。
func mainSidForKey(subagentKey string, kept, noUsage []ClaudeUsageEvent) string {
	for _, e := range kept {
		if e.IsSidechain && e.AgentID != "" && "agent-"+e.AgentID == subagentKey {
			return e.Sid
		}
	}
	for _, e := range noUsage {
		if e.IsSidechain && e.AgentID != "" && "agent-"+e.AgentID == subagentKey {
			return e.Sid
		}
	}
	return ""
}

// sessionKeyFor 决定一个事件归属哪一行 catalog.sessions：
//   - 子代理（IsSidechain && AgentID 非空）→ "agent-<agentId>"
//   - 主 session                           → sessionId
//
// 未来 3C 在做 sessionId+agentId 合并策略时，可改为查父子表，但 3A 保持简单。
func sessionKeyFor(e ClaudeUsageEvent) string {
	if e.IsSidechain && e.AgentID != "" {
		return "agent-" + e.AgentID
	}
	return e.Sid
}

func displayNameFor(e ClaudeUsageEvent) string {
	if e.IsSidechain && e.AgentID != "" {
		tail := e.AgentID
		if len(tail) > 8 {
			tail = tail[len(tail)-8:]
		}
		return projectName(e.Cwd) + " · subagent " + tail
	}
	return projectName(e.Cwd)
}

func projectName(cwd string) string {
	if cwd == "" {
		return "(unknown)"
	}
	parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
	if len(parts) == 0 {
		return "(unknown)"
	}
	last := parts[len(parts)-1]
	if last == "" {
		return "(unknown)"
	}
	return last
}

// BuildRecordsV3 把 KeptEvents 转 schema-v3.md §2 的 10 列行。
// recordBase 是顶层 recordBase（ms），通常取 KeptEvents 中最早 ts 的 UnixMilli。
//
// Phase 3C-M4：父子真合并 —
//   - 主 session 事件：SidIdx == SourceSidIdx == 自己的 catalog idx
//   - subagent 事件：SidIdx = 父的 catalog idx（合并到主会话统计），
//     SourceSidIdx = subagent 自己的 catalog idx（前端 [展开子代理] toggle 用）
//
// 父缺失时（孤儿 subagent）回退到 SidIdx == SourceSidIdx == subagent 自己（不合并）。
func BuildRecordsV3(kept []ClaudeUsageEvent, cat Catalog, recordBase int64) []RecordV3 {
	sidIdx := map[string]int{}
	for i, s := range cat.Sessions {
		sidIdx[s.Sid] = i
	}
	modelIdx := map[string]int{}
	for i, m := range cat.Models {
		modelIdx[m] = i
	}

	out := make([]RecordV3, 0, len(kept))
	for _, e := range kept {
		ownIdx := sidIdx[sessionKeyFor(e)]
		mergedIdx := ownIdx
		if e.IsSidechain {
			// 子代理：合并到父（catalog 第二轮已把 ParentSidIdx 指向父）
			parentIdx := cat.Sessions[ownIdx].ParentSidIdx
			if parentIdx != ownIdx {
				mergedIdx = parentIdx
			}
		}
		flags := 0
		if e.IsSidechain {
			flags |= FlagIsSubagent
		}
		if e.IsApiError {
			flags |= FlagIsFailure | FlagHasError
		}
		if e.IsThirdParty {
			flags |= FlagIsThirdPartyModel
		}

		out = append(out, RecordV3{
			TsDelta:      e.Ts.UnixMilli() - recordBase,
			SidIdx:       mergedIdx, // 合并后主 session（M4）
			ModelIdx:     modelIdx[e.Model],
			Input:        e.Input,
			CacheRead:    e.CacheRead,
			CacheCreate:  e.CacheCreate,
			Output:       e.Output,
			Total:        e.Total(),
			SourceSidIdx: ownIdx, // 子代理自己的 catalog 行（M4 用于 toggle 还原）
			Flags:        flags,
		})
	}
	return out
}

// BuildFailureRecordsV3 输出无 usage 错误行的独立列表（schema-v3.md §6）。
// M4：subagent 失败行也按 catalog.ParentSidIdx 合并到父 session。
func BuildFailureRecordsV3(noUsageErrors []ClaudeUsageEvent, cat Catalog, recordBase int64) []FailureRecordV3 {
	sidIdx := map[string]int{}
	for i, s := range cat.Sessions {
		sidIdx[s.Sid] = i
	}
	modelIdx := map[string]int{}
	for i, m := range cat.Models {
		modelIdx[m] = i
	}

	out := make([]FailureRecordV3, 0, len(noUsageErrors))
	for _, e := range noUsageErrors {
		ownIdx := sidIdx[sessionKeyFor(e)]
		mergedIdx := ownIdx
		if e.IsSidechain {
			parentIdx := cat.Sessions[ownIdx].ParentSidIdx
			if parentIdx != ownIdx {
				mergedIdx = parentIdx
			}
		}
		out = append(out, FailureRecordV3{
			TsDelta:  e.Ts.UnixMilli() - recordBase,
			SidIdx:   mergedIdx,
			ModelIdx: modelIdx[e.Model],
		})
	}
	return out
}

// EarliestTsMs 在 KeptEvents + NoUsageErrors 中找最早 ts 的 UnixMilli，
// 用作顶层 recordBase。空输入返回 0。
func EarliestTsMs(kept []ClaudeUsageEvent, noUsageErrors []ClaudeUsageEvent) int64 {
	var earliest int64
	first := true
	consider := func(e ClaudeUsageEvent) {
		if e.Ts.IsZero() {
			return
		}
		ms := e.Ts.UnixMilli()
		if first || ms < earliest {
			earliest = ms
			first = false
		}
	}
	for _, e := range kept {
		consider(e)
	}
	for _, e := range noUsageErrors {
		consider(e)
	}
	return earliest
}
