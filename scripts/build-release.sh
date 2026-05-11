#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
MAC_DIR="$DIST_DIR/ClaudeScope-mac"
WIN_DIR="$DIST_DIR/ClaudeScope-windows"
MAC_FILES_DIR="$MAC_DIR/ClaudeScope Files"
WIN_FILES_DIR="$WIN_DIR/ClaudeScope Files"

# Version 注入到 generator/cmd/claudescope main.Version；优先环境变量，否则取 package.json
VERSION="${CLAUDESCOPE_VERSION:-$(node -p "require('$ROOT_DIR/package.json').version")}"

cd "$ROOT_DIR"

npm run build:frontend

rm -rf "$DIST_DIR"
mkdir -p "$MAC_FILES_DIR/app" "$MAC_FILES_DIR/bin" "$WIN_FILES_DIR/app" "$WIN_FILES_DIR/bin"

app_files=(
  "index.html"
  "styles.css"
  "app.js"
  "data.sample.js"
)

for file in "${app_files[@]}"; do
  cp "$file" "$MAC_FILES_DIR/app/$file"
  cp "$file" "$WIN_FILES_DIR/app/$file"
done

cp "LICENSE" "$MAC_FILES_DIR/LICENSE"
cp "LICENSE" "$WIN_FILES_DIR/LICENSE"

printf 'window.CLAUDESCOPE_DATA = window.CLAUDESCOPE_DATA || null;\n' > "$MAC_FILES_DIR/app/data.js"
printf 'window.CLAUDESCOPE_DATA = window.CLAUDESCOPE_DATA || null;\n' > "$WIN_FILES_DIR/app/data.js"
printf 'window.CLAUDESCOPE_RAW_DATA = window.CLAUDESCOPE_RAW_DATA || null;\n' > "$MAC_FILES_DIR/app/data.raw.js"
printf 'window.CLAUDESCOPE_RAW_DATA = window.CLAUDESCOPE_RAW_DATA || null;\n' > "$WIN_FILES_DIR/app/data.raw.js"

cp "macos/open-dashboard.command" "$MAC_DIR/Open ClaudeScope.command"
cp "windows/open-dashboard.cmd" "$WIN_DIR/Open ClaudeScope.cmd"

cat > "$MAC_DIR/START-HERE.txt" <<'TXT'
ClaudeScope macOS 用户先看

1. 双击 Open ClaudeScope.command。
2. 如果 macOS 拦截，打开 系统设置 > 隐私与安全性，点击 仍要打开。
3. ClaudeScope Files 文件夹不用点，程序会自动使用里面的文件。
4. 这个包已经内置编译好的程序，不需要安装 Go。

如果你下载的是 GitHub 自动生成的 Source code (zip)，那是给开发者看的源码包，不是普通用户推荐下载。

1. Double-click Open ClaudeScope.command.
2. If macOS blocks it, open System Settings > Privacy & Security, then click Open Anyway.
3. You do not need to open ClaudeScope Files manually.
4. This package already includes the compiled generator. You do not need Go.
TXT

cat > "$WIN_DIR/START-HERE.txt" <<'TXT'
ClaudeScope Windows 用户先看

1. 双击 Open ClaudeScope.cmd。
2. ClaudeScope Files 文件夹不用点，程序会自动使用里面的文件。
3. 这个包已经内置编译好的程序，不需要安装 Go。

如果你下载的是 GitHub 自动生成的 Source code (zip)，那是给开发者看的源码包，不是普通用户推荐下载。

1. Double-click Open ClaudeScope.cmd.
2. You do not need to open ClaudeScope Files manually.
3. This package already includes the compiled generator. You do not need Go.
TXT

LDFLAGS="-s -w -buildid= -X main.Version=${VERSION}"
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="$LDFLAGS" -o "$MAC_FILES_DIR/bin/claudescope-darwin-arm64"  ./generator/cmd/claudescope
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$LDFLAGS" -o "$WIN_FILES_DIR/bin/claudescope-windows-amd64.exe" ./generator/cmd/claudescope

chmod +x "$MAC_DIR/Open ClaudeScope.command" "$MAC_FILES_DIR/bin/claudescope-darwin-arm64"

(
  cd "$DIST_DIR"
  zip -qr "ClaudeScope-mac.zip" "ClaudeScope-mac"
  zip -qr "ClaudeScope-windows.zip" "ClaudeScope-windows"
)

printf 'Built release packages:\n  %s\n  %s\n' \
  "$DIST_DIR/ClaudeScope-mac.zip" \
  "$DIST_DIR/ClaudeScope-windows.zip"
