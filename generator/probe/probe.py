#!/usr/bin/env python3
"""
ClaudeScope Schema Probe

只读、脱敏的字段存在性扫描工具。Phase 2.6 工具：在 generator 真正动手解析前，
先验证 ~/.claude/projects/ 下真实 jsonl 的字段命名、分布、覆盖率。

严格隐私边界：本工具**绝不输出** prompt / message.content / 完整 cwd /
完整 uuid / API key / OAuth URL / jsonl 原文。
只输出字段存在性计数与模型名分布。

用法：
    python3 generator/probe/probe.py --root ~/.claude/projects --out probe-report.json
    python3 generator/probe/probe.py --root ./fixtures

设计选型说明：
    v1.0 计划写的是 Go 版本（generator/probe/main.go.future 保留为 Phase 3 移植参考）。
    实际执行时 Master 钦定"probe 语言自选，优先能最快跑通"，本环境无 Go 工具链，
    故先用 Python 3 (>=3.9) 跑通。逻辑与 Go 版一致，便于后续平移。
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import defaultdict
from datetime import datetime
from pathlib import Path

# 已知顶层字段（其它的进 unknownTopKeys）
KNOWN_TOP_KEYS = {
    "type", "uuid", "sessionId", "agentId", "requestId", "timestamp",
    "cwd", "gitBranch", "parentUuid", "isSidechain", "isApiErrorMessage",
    "message", "summary", "leafUuid", "version", "userType",
    "isCompactSummary", "isMeta", "level", "toolUseResult", "toolUseID",
}


def redact_root_path(root: str) -> str:
    """不输出完整路径，仅输出脱敏概要"""
    abs_path = os.path.abspath(root)
    home = os.path.expanduser("~")
    if home and abs_path.startswith(home):
        return "$HOME" + abs_path[len(home):]
    return os.path.basename(abs_path)


class Report:
    def __init__(self, root: str):
        self.generated_at = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        self.root_summary = redact_root_path(root)
        self.files = 0
        self.lines = 0
        self.parse_errors = 0
        self.subagent_path_files = 0

        self.type_distribution: dict[str, int] = defaultdict(int)
        self.assistant_rows = 0
        self.has_message_id = 0
        self.has_request_id = 0
        self.has_uuid = 0
        self.has_session_id = 0
        self.has_agent_id = 0
        self.has_parent_uuid = 0
        self.has_git_branch = 0
        self.has_cwd = 0
        self.has_usage = 0
        self.has_is_sidechain_true = 0
        self.has_is_api_error_true = 0

        self.model_distribution: dict[str, int] = defaultdict(int)
        self.usage_key_distribution: dict[str, int] = defaultdict(int)
        self.unknown_top_keys: dict[str, int] = defaultdict(int)

        self.third_party_rows = 0
        self.duplicate_rows = 0  # 文件内重复

    def to_dict(self) -> dict:
        return {
            "generatedAt": self.generated_at,
            "rootSummary": self.root_summary,
            "files": self.files,
            "lines": self.lines,
            "parseErrors": self.parse_errors,
            "subagentPathFiles": self.subagent_path_files,
            "typeDistribution": dict(self.type_distribution),
            "assistantRows": self.assistant_rows,
            "hasMessageId": self.has_message_id,
            "hasRequestId": self.has_request_id,
            "hasUuid": self.has_uuid,
            "hasSessionId": self.has_session_id,
            "hasAgentId": self.has_agent_id,
            "hasParentUuid": self.has_parent_uuid,
            "hasGitBranch": self.has_git_branch,
            "hasCwd": self.has_cwd,
            "hasUsage": self.has_usage,
            "hasIsSidechainTrue": self.has_is_sidechain_true,
            "hasIsApiErrorTrue": self.has_is_api_error_true,
            "modelDistribution": dict(self.model_distribution),
            "usageKeyDistribution": dict(self.usage_key_distribution),
            "unknownTopKeys": dict(self.unknown_top_keys),
            "thirdPartyRows": self.third_party_rows,
            "duplicateRowsWithinFile": self.duplicate_rows,
        }


def find_jsonl(root: str) -> list[str]:
    out: list[str] = []
    for dirpath, _, filenames in os.walk(root):
        for name in filenames:
            if name.lower().endswith(".jsonl"):
                out.append(os.path.join(dirpath, name))
    return out


def scan_file(path: str, report: Report) -> None:
    try:
        f = open(path, "r", encoding="utf-8", errors="replace")
    except OSError:
        report.parse_errors += 1
        return

    seen_strong: set[str] = set()
    with f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            report.lines += 1
            try:
                row = json.loads(line)
            except (json.JSONDecodeError, ValueError):
                report.parse_errors += 1
                continue
            process_row(row, report, seen_strong)


def process_row(row: dict, report: Report, seen_strong: set[str]) -> None:
    if not isinstance(row, dict):
        return

    # 顶层 type
    t = row.get("type")
    if isinstance(t, str):
        report.type_distribution[t] += 1

    # 未知顶层键
    for k in row:
        if k not in KNOWN_TOP_KEYS:
            report.unknown_top_keys[k] += 1

    if t != "assistant":
        return

    report.assistant_rows += 1

    # 顶层字段存在性
    if "uuid" in row:
        report.has_uuid += 1
    if "sessionId" in row:
        report.has_session_id += 1
    if "agentId" in row:
        report.has_agent_id += 1
    if "requestId" in row:
        report.has_request_id += 1
    if "parentUuid" in row:
        report.has_parent_uuid += 1
    if "gitBranch" in row:
        report.has_git_branch += 1
    if "cwd" in row:
        report.has_cwd += 1
    if row.get("isSidechain") is True:
        report.has_is_sidechain_true += 1
    if row.get("isApiErrorMessage") is True:
        report.has_is_api_error_true += 1

    msg = row.get("message")
    if not isinstance(msg, dict):
        return

    msg_id = msg.get("id") if isinstance(msg.get("id"), str) else ""
    if msg_id:
        report.has_message_id += 1

    # 模型名（不敏感，可输出）
    model = msg.get("model")
    if isinstance(model, str) and model:
        report.model_distribution[model] += 1
        if not model.startswith("claude-") and model != "<synthetic>":
            report.third_party_rows += 1

    # usage
    usage = msg.get("usage")
    if isinstance(usage, dict):
        report.has_usage += 1
        for k in usage:
            report.usage_key_distribution[k] += 1

    # 文件内 message.id+requestId 去重检测
    request_id = row.get("requestId") if isinstance(row.get("requestId"), str) else ""
    if msg_id and request_id:
        key = f"{msg_id}:{request_id}"
        if key in seen_strong:
            report.duplicate_rows += 1
        else:
            seen_strong.add(key)


def pct(n: int, total: int) -> float:
    return 0.0 if total == 0 else (n * 100.0 / total)


def print_sorted_top(m: dict[str, int], top: int = 0) -> None:
    items = sorted(m.items(), key=lambda kv: (-kv[1], kv[0]))
    if top > 0:
        shown = items[:top]
        rest = len(items) - len(shown)
    else:
        shown = items
        rest = 0
    for k, v in shown:
        print(f"  {k:<40s} {v}")
    if rest > 0:
        print(f"  ... 其余 {rest} 个")


def print_summary(r: Report) -> None:
    print("================================================================")
    print("  ClaudeScope Schema Probe · 脱敏字段统计")
    print("================================================================")
    print(f"生成时间    : {r.generated_at}")
    print(f"扫描根目录  : {r.root_summary}")
    print(f"文件数      : {r.files}")
    print(f"总行数      : {r.lines}")
    print(f"解析失败行  : {r.parse_errors}")
    print(f"子代理路径  : {r.subagent_path_files} 个文件")
    print()

    print("type 分布：")
    print_sorted_top(r.type_distribution)
    print()

    print(f"assistant 行数 : {r.assistant_rows}")
    if r.assistant_rows > 0:
        print(f"  含 message.id      : {r.has_message_id} ({pct(r.has_message_id, r.assistant_rows):.1f}%)")
        print(f"  含 requestId       : {r.has_request_id} ({pct(r.has_request_id, r.assistant_rows):.1f}%)")
        print(f"  含 uuid            : {r.has_uuid} ({pct(r.has_uuid, r.assistant_rows):.1f}%)")
        print(f"  含 sessionId       : {r.has_session_id} ({pct(r.has_session_id, r.assistant_rows):.1f}%)")
        print(f"  含 agentId         : {r.has_agent_id} ({pct(r.has_agent_id, r.assistant_rows):.1f}%)")
        print(f"  含 parentUuid      : {r.has_parent_uuid} ({pct(r.has_parent_uuid, r.assistant_rows):.1f}%)")
        print(f"  含 gitBranch       : {r.has_git_branch} ({pct(r.has_git_branch, r.assistant_rows):.1f}%)")
        print(f"  含 cwd             : {r.has_cwd} ({pct(r.has_cwd, r.assistant_rows):.1f}%)")
        print(f"  含 message.usage   : {r.has_usage} ({pct(r.has_usage, r.assistant_rows):.1f}%)")
        print(f"  isSidechain=true   : {r.has_is_sidechain_true} ({pct(r.has_is_sidechain_true, r.assistant_rows):.1f}%)")
        print(f"  isApiErrorMessage  : {r.has_is_api_error_true} ({pct(r.has_is_api_error_true, r.assistant_rows):.1f}%)")
        print(f"  第三方模型行       : {r.third_party_rows} ({pct(r.third_party_rows, r.assistant_rows):.1f}%)")
        print(f"  文件内重复行       : {r.duplicate_rows} ({pct(r.duplicate_rows, r.assistant_rows):.1f}%)")
    print()

    print("usage 内键分布（前 12）：")
    print_sorted_top(r.usage_key_distribution, top=12)
    print()

    print("模型名分布（前 20）：")
    print_sorted_top(r.model_distribution, top=20)
    print()

    if r.unknown_top_keys:
        print("⚠ 未知顶层键（schema 可能漂移）：")
        print_sorted_top(r.unknown_top_keys, top=20)
        print()

    print("================================================================")
    print("  报告完成 · 严格脱敏 · 不含 prompt / content / 完整路径")
    print("================================================================")


def main() -> int:
    parser = argparse.ArgumentParser(description="ClaudeScope Schema Probe (脱敏)")
    parser.add_argument("--root", default="", help="扫描根目录（默认 ~/.claude/projects）")
    parser.add_argument("--out", default="", help="输出 JSON 报告路径（默认仅 stdout 摘要）")
    parser.add_argument("--max-files", type=int, default=0, help="最多扫描文件数（0=全部）")
    args = parser.parse_args()

    root = args.root or os.path.expanduser("~/.claude/projects")
    if not os.path.exists(root):
        print(f"扫描根目录不存在：{redact_root_path(root)}", file=sys.stderr)
        return 1

    report = Report(root)
    files = find_jsonl(root)
    if args.max_files and len(files) > args.max_files:
        files = files[: args.max_files]
    report.files = len(files)

    for path in files:
        if "/subagents/" in path or os.path.basename(path).startswith("agent-"):
            report.subagent_path_files += 1
        scan_file(path, report)

    if args.out:
        with open(args.out, "w", encoding="utf-8") as f:
            json.dump(report.to_dict(), f, indent=2, ensure_ascii=False)

    print_summary(report)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
