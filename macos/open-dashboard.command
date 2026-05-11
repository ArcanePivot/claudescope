#!/bin/zsh
# ClaudeScope macOS 启动器
#
# 行为：
#   1. 定位 dashboard 目录（兼容 release zip 的 ClaudeScope Files/ 布局，也支持源码仓直接双击）
#   2. 优先用预构建 claudescope-darwin-arm64 跑 generate
#   3. 没有二进制时回退到 go build（仅源码仓场景，需本机有 go）
#   4. 完成后 open index.html
set -e

cd "$(dirname "$0")"

dashboard_dir="$PWD"
generator="./claudescope-darwin-arm64"
data_path="data.js"

if [ -f "ClaudeScope Files/app/index.html" ]; then
  dashboard_dir="$PWD/ClaudeScope Files/app"
  generator="$PWD/ClaudeScope Files/bin/claudescope-darwin-arm64"
  data_path="$dashboard_dir/data.js"
elif [ -f "app/index.html" ]; then
  dashboard_dir="$PWD/app"
  generator="$PWD/bin/claudescope-darwin-arm64"
  data_path="$dashboard_dir/data.js"
elif [ ! -f index.html ]; then
  cd ..
  dashboard_dir="$PWD"
fi

if [ -f "$generator" ]; then
  chmod +x "$generator" 2>/dev/null || true
fi

if [ -x "$generator" ] && "$generator" generate --out "$data_path"; then
  open "$dashboard_dir/index.html"
  exit 0
fi

# 源码仓本地构建产物兜底
if [ -x ./bin/claudescope ] && ./bin/claudescope generate --out "$data_path"; then
  open "$dashboard_dir/index.html"
  exit 0
fi

if command -v go >/dev/null 2>&1; then
  go build -trimpath -ldflags="-s -w" -o ./bin/claudescope ./generator/cmd/claudescope
  ./bin/claudescope generate --out "$data_path"
else
  echo "未找到预构建的 claudescope，且本机未安装 Go。" >&2
  echo "请从 GitHub Releases 下载 ClaudeScope-mac.zip。" >&2
  exit 1
fi

open "$dashboard_dir/index.html"
