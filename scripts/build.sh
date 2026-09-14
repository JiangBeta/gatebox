#!/usr/bin/env bash
# 构建发布产物：前端 → go:embed → 后端单二进制 → dist/ 目录（含制品与工具）.
#
# 用法：scripts/build.sh [版本号]
#   VERSION  版本号（默认取 git describe）
#   GOOS/GOARCH 目标平台（默认当前平台）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-${VERSION:-$(git -C "$ROOT" describe --tags --always 2>/dev/null || echo dev)}}"
OS="${GOOS:-linux}"
ARCH="${GOARCH:-$(go env GOARCH)}"
OUT="$ROOT/dist"
BIN="gatebox-${VERSION}-${OS}-${ARCH}"

echo "==> 构建前端"
(cd "$ROOT/frontend" && pnpm install --frozen-lockfile && pnpm build)

echo "==> 构建后端 ($OS/$ARCH)"
mkdir -p "$OUT"
(cd "$ROOT/backend" && CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o "$OUT/$BIN" ./cmd/gatebox)

echo "==> 打包"
(cd "$ROOT" && tar -czf "$OUT/gatebox-${VERSION}-${OS}-${ARCH}.tar.gz" -C "$OUT" "$BIN")
echo "完成: $OUT/$BIN"
