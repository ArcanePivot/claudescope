package parser

import (
	"testing"
	"time"
)

// TestDedupKeyPriority 验证 schema-v3.md §5.1 三级 key 优先级。
func TestDedupKeyPriority(t *testing.T) {
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		ev          ClaudeUsageEvent
		wantStrong  bool
		wantUuid    bool
		wantPrefix  string
	}{
		{
			name: "strong key (msgId + requestId)",
			ev: ClaudeUsageEvent{
				MessageID: "msg_a", RequestID: "req_a",
				Uuid: "uuid_a", Ts: ts, Sid: "sid", Model: "claude-sonnet-4-6",
				Input: 100,
			},
			wantStrong: true, wantUuid: false,
			wantPrefix: "strong:msg_a:req_a",
		},
		{
			name: "uuid fallback (no requestId, has uuid)",
			ev: ClaudeUsageEvent{
				MessageID: "msg_b",
				Uuid:      "uuid_b", Ts: ts, Sid: "sid", Model: "claude-sonnet-4-6",
				Input: 200,
			},
			wantStrong: false, wantUuid: true,
			wantPrefix: "uuid:uuid_b",
		},
		{
			name: "weak key (no requestId, no uuid)",
			ev: ClaudeUsageEvent{
				MessageID: "msg_c",
				Ts:        ts, Sid: "sid_c", Model: "claude-opus-4-7",
				Input: 1, CacheRead: 2, CacheCreate: 3, Output: 4,
			},
			wantStrong: false, wantUuid: false,
			wantPrefix: "weak:",
		},
		{
			name: "missing msgId but has requestId → uuid path (because strong needs both)",
			ev: ClaudeUsageEvent{
				RequestID: "req_only",
				Uuid:      "uuid_z", Ts: ts, Sid: "sid", Model: "claude-sonnet-4-6",
			},
			wantStrong: false, wantUuid: true,
			wantPrefix: "uuid:uuid_z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, isStrong, isUuid := DedupKey(tc.ev)
			if isStrong != tc.wantStrong {
				t.Errorf("isStrong = %v, want %v (key=%s)", isStrong, tc.wantStrong, key)
			}
			if isUuid != tc.wantUuid {
				t.Errorf("isUuid = %v, want %v (key=%s)", isUuid, tc.wantUuid, key)
			}
			if len(key) < len(tc.wantPrefix) || key[:len(tc.wantPrefix)] != tc.wantPrefix {
				t.Errorf("key = %q, want prefix %q", key, tc.wantPrefix)
			}
		})
	}
}

// TestDedupEventsAccumulation：strong/uuid/weak 三类各 1 条 + 1 条重复，验证统计累加。
func TestDedupEventsAccumulation(t *testing.T) {
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	events := []ClaudeUsageEvent{
		// strong key (msg_a + req_a)
		{MessageID: "msg_a", RequestID: "req_a", Uuid: "u1", Ts: ts, Sid: "s1", Model: "claude-sonnet-4-6", Input: 100},
		// duplicate of strong (different uuid, same msg_a+req_a)
		{MessageID: "msg_a", RequestID: "req_a", Uuid: "u1-dup", Ts: ts, Sid: "s1", Model: "claude-sonnet-4-6", Input: 100},
		// uuid fallback (no requestId)
		{MessageID: "msg_b", Uuid: "u2", Ts: ts, Sid: "s1", Model: "claude-sonnet-4-6", Input: 200},
		// weak key (no msg_id no uuid → must use 4 tokens + ts + sid + model)
		{Ts: ts, Sid: "s2", Model: "claude-opus-4-7", Input: 1, CacheRead: 2, CacheCreate: 3, Output: 4},
	}

	kept, stats := DedupEvents(events)

	if len(kept) != 3 {
		t.Errorf("kept len = %d, want 3", len(kept))
	}
	want := DedupStats{
		RawUsageRows: 4, KeptUsageRows: 3, DuplicatesSkipped: 1,
		UuidFallbackRows: 1, WeakKeyRows: 1,
	}
	if stats != want {
		t.Errorf("stats = %+v, want %+v", stats, want)
	}
}

// TestDedupEventsStrongDoesNotCountAsUuid 验证 strong key 行不计入 uuidFallbackRows。
func TestDedupEventsStrongDoesNotCountAsUuid(t *testing.T) {
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	// 同时有 msgId/requestId/uuid 的行：必须走 strong，不进 uuid fallback 计数。
	events := []ClaudeUsageEvent{
		{MessageID: "m", RequestID: "r", Uuid: "u", Ts: ts, Sid: "s", Model: "claude-sonnet-4-6", Input: 1},
	}
	_, stats := DedupEvents(events)
	if stats.UuidFallbackRows != 0 {
		t.Errorf("strong key event leaked into uuidFallbackRows: stats=%+v", stats)
	}
	if stats.WeakKeyRows != 0 {
		t.Errorf("strong key event leaked into weakKeyRows: stats=%+v", stats)
	}
	if stats.KeptUsageRows != 1 {
		t.Errorf("kept = %d, want 1", stats.KeptUsageRows)
	}
}

// TestDedupEventsEmpty 空输入返回空切片 + 全 0 stats。
func TestDedupEventsEmpty(t *testing.T) {
	kept, stats := DedupEvents(nil)
	if len(kept) != 0 {
		t.Errorf("empty kept = %d, want 0", len(kept))
	}
	if stats != (DedupStats{}) {
		t.Errorf("empty stats = %+v, want zero", stats)
	}
}

// TestDedupKeyWeakKeyIncludesAllDistinguishers 弱键应能区分 ts/sid/model/4 tokens 中任一变化。
func TestDedupKeyWeakKeyIncludesAllDistinguishers(t *testing.T) {
	ts1 := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(time.Second)

	base := ClaudeUsageEvent{
		Ts: ts1, Sid: "s", Model: "claude-sonnet-4-6",
		Input: 1, CacheRead: 2, CacheCreate: 3, Output: 4,
	}
	k0, _, _ := DedupKey(base)

	variants := []struct {
		name string
		mut  func(e *ClaudeUsageEvent)
	}{
		{"ts changed", func(e *ClaudeUsageEvent) { e.Ts = ts2 }},
		{"sid changed", func(e *ClaudeUsageEvent) { e.Sid = "s2" }},
		{"model changed", func(e *ClaudeUsageEvent) { e.Model = "claude-opus-4-7" }},
		{"input changed", func(e *ClaudeUsageEvent) { e.Input = 2 }},
		{"cacheRead changed", func(e *ClaudeUsageEvent) { e.CacheRead = 99 }},
		{"cacheCreate changed", func(e *ClaudeUsageEvent) { e.CacheCreate = 99 }},
		{"output changed", func(e *ClaudeUsageEvent) { e.Output = 99 }},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			e := base
			v.mut(&e)
			k, _, _ := DedupKey(e)
			if k == k0 {
				t.Errorf("%s should produce different key, got same=%q", v.name, k)
			}
		})
	}
}
