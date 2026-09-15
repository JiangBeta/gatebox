#!/usr/bin/env bash
# GateBox 安装脚本（多发行版 / 多 init 系统）。
#
# 用法：
#   scripts/install.sh [--data-dir DIR] [--bin PATH] [--tools DIR]
#
#   --data-dir  运行时数据根目录（默认 /opt/gatebox）
#   --bin       gatebox 二进制路径（默认脚本同目录/../dist 下最新产物，或 PATH 中 gatebox）
#   --tools     外部组件制品目录（可选；铺到 $DATA_DIR/tools/<id>/）
#
# 支持 init：systemd / openrc / procd(OpenWrt)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA_DIR="/opt/gatebox"
BIN=""
TOOLS=""

while [ $# -gt 0 ]; do
  case "$1" in
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --bin) BIN="$2"; shift 2 ;;
    --tools) TOOLS="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" != "0" ]; then
  echo "请以 root 运行（安装服务需写系统目录）" >&2
  exit 1
fi

# 定位二进制
if [ -z "$BIN" ]; then
  if [ -x "$ROOT/dist/gatebox" ]; then
    BIN="$ROOT/dist/gatebox"
  else
    BIN="$(ls -1 "$ROOT"/dist/gatebox-* 2>/dev/null | head -1 || true)"
  fi
fi
if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
  BIN="$(command -v gatebox || true)"
fi
if [ -z "$BIN" ]; then
  echo "未找到 gatebox 二进制，请先 scripts/build.sh 或用 --bin 指定" >&2
  exit 1
fi

echo "==> 安装二进制: $BIN"
install -m 0755 "$BIN" /usr/local/bin/gatebox

echo "==> 创建数据目录: $DATA_DIR"
mkdir -p "$DATA_DIR/conf" "$DATA_DIR/tools" "$DATA_DIR/appData"

if [ -n "$TOOLS" ] && [ -d "$TOOLS" ]; then
  echo "==> 铺设外部组件制品: $TOOLS → $DATA_DIR/tools"
  cp -a "$TOOLS/." "$DATA_DIR/tools/" 2>/dev/null || true
fi

# ensure_service_user 创建低权服务用户 gatebox(系统服务以 User=gatebox 运行)。
# 低端口能力由 systemd AmbientCapabilities 授予,不依赖二进制文件能力。
ensure_service_user() {
  if id gatebox >/dev/null 2>&1; then
    return
  fi
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin gatebox 2>/dev/null \
      || useradd -r -d "$DATA_DIR" -s /sbin/nologin gatebox 2>/dev/null || true
  fi
  if id gatebox >/dev/null 2>&1; then
    echo "==> 已创建服务用户 gatebox"
  else
    echo "!! 未能创建用户 gatebox,请手动创建后重装 caddy 单元" >&2
  fi
}

# install_caddy_unit 安装系统级 caddy 服务(AmbientCapabilities 授低端口能力)。
install_caddy_unit() {
  local unit=/etc/systemd/system/caddy.service
  install -m 0644 "$ROOT/configs/services/caddy.service" "$unit"
  sed -i "s#/opt/gatebox#$DATA_DIR#g" "$unit"
  if ! id gatebox >/dev/null 2>&1; then
    # 无 gatebox 用户时退回 root 运行(无需能力)。
    sed -i "s#^User=gatebox#User=root#; s#^AmbientCapabilities=.*##; s#^CapabilityBoundingSet=.*##" "$unit"
  fi
  # polkit:允许 gatebox(若存在)重启 caddy.service(GateBox 组件页用)。
  if id gatebox >/dev/null 2>&1 && [ -d /etc/polkit-1/rules.d ]; then
    cat > /etc/polkit-1/rules.d/49-gatebox-caddy.rules <<'RULES'
polkit.addRule(function(action, subject) {
  if (action.id == "org.freedesktop.systemd1.manage-units" &&
      subject.user == "gatebox" &&
      action.lookup("unit") == "caddy.service") {
    return polkit.Result.YES;
  }
});
RULES
    echo "==> 已写入 polkit 规则(允许 gatebox 重启 caddy.service)"
  fi
  systemctl enable caddy.service
}

install_service() {
  if command -v systemctl >/dev/null 2>&1; then
    echo "==> 安装 systemd 服务"
    ensure_service_user
    chown -R gatebox:gatebox "$DATA_DIR" 2>/dev/null || true
    install -m 0644 "$ROOT/configs/services/gatebox.service" /etc/systemd/system/gatebox.service
    sed -i "s#GATEBOX_DATA_DIR=/opt/gatebox#GATEBOX_DATA_DIR=$DATA_DIR#" /etc/systemd/system/gatebox.service
    install_caddy_unit
    systemctl daemon-reload
    systemctl enable gatebox.service
    echo "  启动：systemctl start gatebox caddy"
  elif [ -x /etc/init.d/gatebox ] || command -v rc-update >/dev/null 2>&1; then
    echo "==> 安装 OpenRC 服务"
    install -m 0755 "$ROOT/configs/services/gatebox.openrc" /etc/init.d/gatebox
    rc-update add gatebox default 2>/dev/null || true
    echo "  启动：rc-service gatebox start"
  elif [ -d /etc/init.d ] && command -v procd >/dev/null 2>&1; then
    echo "==> 安装 procd 服务"
    install -m 0755 "$ROOT/configs/services/gatebox.procd" /etc/init.d/gatebox
    /etc/init.d/gatebox enable
    echo "  启动：/etc/init.d/gatebox start"
  else
    echo "!! 未识别的 init 系统，请手动配置服务" >&2
  fi
}

install_service
echo "==> 完成。数据目录：$DATA_DIR；首次启动会生成 conf/gatebox.conf"
