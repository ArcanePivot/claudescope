// Command claudescope 是 ClaudeScope v1.0 的多子命令入口。
//
// 用法：
//
//	claudescope generate [--root --out --since --window-days]
//	claudescope open     [--out path]
//	claudescope version
//
// 不带子命令时打印 usage 并退出 2。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"claudescope/generator/cmd/internal/runner"
)

// Version 由 ldflags 在 build-release 阶段注入；缺省值反映源码状态。
var Version = "1.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "generate":
		runGenerate(os.Args[2:])
	case "open":
		runOpen(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("claudescope %s (%s/%s, go %s)\n",
			Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令：%s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func runGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	root := fs.String("root", runner.DefaultClaudeRoot(), "Claude Code 日志根目录（含 *.jsonl）")
	out := fs.String("out", "data.js", "输出文件路径（包成 window.CLAUDESCOPE_DATA）")
	since := fs.String("since", "", "RFC3339 时间，仅保留之后的事件（缺省全量）")
	windowDaysFlag := fs.Int("window-days", 0, "限定 windowDays 元数据（仅展示，不影响过滤）")
	preset := fs.String("preset", "", "本地压力 preset：pro / max-5x / max-20x / custom（缺省走 risk.json 或 builtin pro）")
	fs.Parse(args)

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
		Preset:     *preset,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runOpen(args []string) {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	out := fs.String("out", "data.js", "data.js 路径；用来定位同目录的 index.html")
	fs.Parse(args)

	indexPath, err := locateIndexHTML(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := openInBrowser(indexPath); err != nil {
		fmt.Fprintf(os.Stderr, "打开浏览器失败：%v\n（路径：%s）\n", err, indexPath)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已在浏览器中打开 %s\n", indexPath)
}

// locateIndexHTML 优先取与 data.js 同目录的 index.html，
// 找不到再退到当前工作目录。两处都没有时报错。
func locateIndexHTML(dataPath string) (string, error) {
	abs, err := filepath.Abs(dataPath)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(filepath.Dir(abs), "index.html")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	candidate = filepath.Join(cwd, "index.html")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("找不到 index.html（已尝试 %s 同目录与当前工作目录 %s）",
		filepath.Dir(abs), cwd)
}

func openInBrowser(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Run()
	case "windows":
		// 用 rundll32 而非 cmd /c start，避免 path 带空格时被 cmd 切词。
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Run()
	default:
		return exec.Command("xdg-open", path).Run()
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `claudescope - Claude Code 本地用量仪表盘 (v1.0)

用法：
  claudescope generate [--root <dir>] [--out <file>] [--since <RFC3339>] [--window-days <n>] [--preset <pro|max-5x|max-20x|custom>]
  claudescope open     [--out <file>]
  claudescope version
  claudescope help

generate：扫描 ~/.claude/projects/ 下全部 *.jsonl，写入 data.js
open：    在系统默认浏览器中打开 index.html
version： 打印版本号

配置文件：
  ~/.claude-scope/pricing.json  自定义模型价格（见 docs/pricing-config.md）
  ~/.claude-scope/risk.json     自定义本地压力阈值（见 docs/risk-config.md）

`)
}
