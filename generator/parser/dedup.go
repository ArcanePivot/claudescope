package parser

import "fmt"

// DedupKey 实现 schema-v3.md §5.1 三级 key 优先级：
//
//	strong  : message.id + requestId 都有 → "strong:<msg>:<req>"
//	uuid    : 缺 strong 但有 uuid       → "uuid:<uuid>"
//	weak    : 缺 strong 且缺 uuid       → "weak:<tsMs>:<sid>:<model>:<4 tokens>"
//
// 第二/第三个返回值用于 DedupEvents 的累加规则。
func DedupKey(e ClaudeUsageEvent) (key string, isStrong bool, isUuid bool) {
	if e.MessageID != "" && e.RequestID != "" {
		return "strong:" + e.MessageID + ":" + e.RequestID, true, false
	}
	if e.Uuid != "" {
		return "uuid:" + e.Uuid, false, true
	}
	return fmt.Sprintf("weak:%d:%s:%s:%d:%d:%d:%d",
		e.Ts.UnixMilli(), e.Sid, e.Model,
		e.Input, e.CacheRead, e.CacheCreate, e.Output), false, false
}

// DedupEvents 按 schema-v3.md §5.1 累加规则去重。
// 输入是 raw events（已过滤 synthetic / 无 usage 非 api-error 行）。
func DedupEvents(events []ClaudeUsageEvent) ([]ClaudeUsageEvent, DedupStats) {
	seen := make(map[string]bool, len(events))
	kept := make([]ClaudeUsageEvent, 0, len(events))
	stats := DedupStats{}

	for _, e := range events {
		stats.RawUsageRows++
		key, isStrong, isUuid := DedupKey(e)
		if seen[key] {
			stats.DuplicatesSkipped++
			continue
		}
		seen[key] = true
		kept = append(kept, e)
		stats.KeptUsageRows++
		switch {
		case isStrong:
			// strong key：不计 uuid/weak
		case isUuid:
			stats.UuidFallbackRows++
		default:
			stats.WeakKeyRows++
		}
	}

	return kept, stats
}
