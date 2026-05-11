#!/usr/bin/env bash
# ClaudeScope 前端构建脚本（Phase 3B-P1 hotfix）
#
# 用途：在非交互 SSH / mini 复审环境下稳定加载 nvm 后再跑 tsc。
# 交互式终端通常会自动 source ~/.nvm/nvm.sh；非交互 shell（ssh -T、CI 单步）不会，
# 直接调 ./node_modules/.bin/tsc 会报 `env: node: No such file or directory`。
#
# 用法：
#   ./scripts/build-frontend.sh          # 编译 app.ts → app.js
#   ./scripts/build-frontend.sh --watch  # 监听增量编译

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# 1. 加载 nvm（非交互 shell 必备）
if [ -s "$HOME/.nvm/nvm.sh" ]; then
  # shellcheck disable=SC1091
  . "$HOME/.nvm/nvm.sh"
fi

# 2. 校验 node 可用
if ! command -v node >/dev/null 2>&1; then
  echo "[build-frontend] 找不到 node。请安装 Node.js 或 nvm（推荐 v18+）。" >&2
  exit 127
fi

# 3. 校验 tsc 可用
if [ ! -x "./node_modules/.bin/tsc" ]; then
  echo "[build-frontend] 缺少 ./node_modules/.bin/tsc。请先 npm install。" >&2
  exit 127
fi

# 4. 编译
exec ./node_modules/.bin/tsc -p tsconfig.json "$@"
