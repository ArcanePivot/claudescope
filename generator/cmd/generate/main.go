// Command generate 是 ClaudeScope generator 的早期单命令入口。
// v1.0 起核心流程已迁到 generator/cmd/internal/runner，本命令仅做 flag 转发。
// 新代码请改用 generator/cmd/claudescope（多子命令入口）。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"claudescope/generator/cmd/internal/runner"
)

func main() {
	root := flag.String("root", runner.DefaultClaudeRoot(), "Claude Code 日志根目录（含 *.jsonl）")
	out := flag.String("out", "data.js", "输出文件路径（包成 window.CLAUDESCOPE_DATA）")
	since := flag.String("since", "", "RFC3339 时间，仅保留之后的事件（缺省全量）")
	windowDaysFlag := flag.Int("window-days", 0, "限定 windowDays 元数据（仅展示，不影响过滤）")
	flag.Parse()

	cutoff := time.Time{}
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "解析 --since 失败：%v\n", err)
			os.Exit(2)
		}
		cutoff = t
	}

	if _, err := runner.Run(runner.Options{
		Root:       *root,
		Out:        *out,
		Since:      cutoff,
		WindowDays: *windowDaysFlag,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
