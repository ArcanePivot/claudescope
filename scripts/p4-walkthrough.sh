#!/usr/bin/env bash
# Phase 4 浏览器走查辅助脚本
#
# 用法：
#   scripts/p4-walkthrough.sh v2          # 切换到 V2 legacy fixture（验 P4-1：toggle 隐藏）
#   scripts/p4-walkthrough.sh unpriced    # 切换到只匹配 Opus 4.7 的 fixture（验 P4-3：未定价 banner / 徽章）
#   scripts/p4-walkthrough.sh restore     # 恢复真实 data.js（任何切换后必须跑这条）
#
# 切换机制：把当前 data.js 备份成 data.js.p4-bak，再把 fixture 拷成 data.js。
# 重要：恢复前不要再次切换，否则备份会被覆盖丢失真实数据。

set -euo pipefail

cd "$(dirname "$0")/.."

DATA="data.js"
BAK="data.js.p4-bak"

case "${1:-}" in
  v2)
    [[ -f "$BAK" ]] && { echo "已有 $BAK；先跑 restore 再切。"; exit 1; }
    [[ -f fixtures/data-v2-legacy.js ]] || { echo "fixtures/data-v2-legacy.js 缺失。"; exit 2; }
    cp "$DATA" "$BAK"
    cp fixtures/data-v2-legacy.js "$DATA"
    echo "已切到 V2 legacy fixture。打开 index.html 走查："
    echo "  - schemaVersion = 2"
    echo "  - 会话排行右上角 [聚合主会话]/[展开子代理] toggle 应不可见"
    echo "  - 控制台无 JS 报错"
    echo "  - 数据正常显示（只 200 行截取，token 数会很小）"
    echo "走查完跑：scripts/p4-walkthrough.sh restore"
    ;;
  unpriced)
    [[ -f "$BAK" ]] && { echo "已有 $BAK；先跑 restore 再切。"; exit 1; }
    [[ -f fixtures/data-p4-3-unpriced.js ]] || { echo "fixtures/data-p4-3-unpriced.js 缺失。"; exit 2; }
    cp "$DATA" "$BAK"
    cp fixtures/data-p4-3-unpriced.js "$DATA"
    echo "已切到 P4-3 fixture（只匹配 Opus 4.7，其它 claude-* 全未定价）。打开 index.html 走查："
    echo "  - 费用面板顶部出现琥珀 banner，列出 3 个未定价模型"
    echo "  - 模型排行 sonnet-4-6 / opus-4-6 / haiku-4-5 行右侧显示「未定价」徽章而非 \$0.00"
    echo "  - hover 徽章 tooltip 含未定价 token 量"
    echo "走查完跑：scripts/p4-walkthrough.sh restore"
    ;;
  restore)
    [[ -f "$BAK" ]] || { echo "$BAK 不存在；可能没切换过或已恢复。"; exit 0; }
    mv "$BAK" "$DATA"
    echo "已恢复真实 data.js"
    ;;
  *)
    echo "用法：$0 {v2|unpriced|restore}"
    exit 1
    ;;
esac
