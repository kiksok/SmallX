#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/kiksok/liteone.git"
INSTALL_DIR="/opt/liteone"
CONFIG_DIR="/etc/liteone"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
BINARY_PATH="/usr/local/bin/liteone"
SERVICE_FILE="/etc/systemd/system/liteone.service"
GO_VERSION="1.22.12"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"

PANEL_URL=""
TOKEN=""
NODE_ID=""
NODE_TYPE="shadowsocks"
RUNTIME="ss-prototype"
PULL_INTERVAL="60s"
STATUS_INTERVAL="60s"

usage() {
  cat <<EOF
Usage:
  install.sh --panel-url URL --token TOKEN --node-id ID [options]

Required:
  --panel-url URL          Xboard panel URL
  --token TOKEN            Xboard server token
  --node-id ID             Xboard node id

Optional:
  --node-type TYPE         Node type, default: shadowsocks
  --runtime NAME           Runtime adapter, default: ss-prototype
  --pull-interval DUR      Pull interval, default: 60s
  --status-interval DUR    Status interval, default: 60s
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel-url)
      PANEL_URL="$2"
      shift 2
      ;;
    --token)
      TOKEN="$2"
      shift 2
      ;;
    --node-id)
      NODE_ID="$2"
      shift 2
      ;;
    --node-type)
      NODE_TYPE="$2"
      shift 2
      ;;
    --runtime)
      RUNTIME="$2"
      shift 2
      ;;
    --pull-interval)
      PULL_INTERVAL="$2"
      shift 2
      ;;
    --status-interval)
      STATUS_INTERVAL="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "Please run as root" >&2
  exit 1
fi

if [[ -z "$PANEL_URL" || -z "$TOKEN" || -z "$NODE_ID" ]]; then
  usage
  exit 1
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This installer currently supports Linux only" >&2
  exit 1
fi

if [[ "$(uname -m)" != "x86_64" ]]; then
  echo "This installer currently supports Linux AMD64 only" >&2
  exit 1
fi

apt-get update
apt-get install -y ca-certificates curl git tar

need_go_install=1
if command -v go >/dev/null 2>&1; then
  current_go="$(go version | awk '{print $3}' | sed 's/go//')"
  case "$current_go" in
    1.22.*|1.23.*|1.24.*|1.25.*)
      need_go_install=0
      ;;
  esac
fi

if [[ $need_go_install -eq 1 ]]; then
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT
  curl -fsSL "$GO_URL" -o "${tmp_dir}/${GO_TARBALL}"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${tmp_dir}/${GO_TARBALL}"
  export PATH="/usr/local/go/bin:${PATH}"
else
  export PATH="/usr/local/go/bin:${PATH}"
fi

if [[ ! -d "${INSTALL_DIR}/.git" ]]; then
  git clone "$REPO_URL" "$INSTALL_DIR"
else
  git -C "$INSTALL_DIR" fetch --all --tags
  git -C "$INSTALL_DIR" reset --hard origin/main
fi

mkdir -p "$CONFIG_DIR"
cat > "$CONFIG_FILE" <<EOF
panel:
  provider: xboard
  base_url: "${PANEL_URL}"
  token: "${TOKEN}"
  node_id: ${NODE_ID}
  node_type: ${NODE_TYPE}
  timeout: 10s

sync:
  pull_interval: ${PULL_INTERVAL}
  status_interval: ${STATUS_INTERVAL}

runtime:
  adapter: ${RUNTIME}
  work_dir: /var/lib/liteone
  apply_timeout: 15s

log:
  level: info
EOF

mkdir -p /var/lib/liteone
cd "$INSTALL_DIR"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" go build -o "$BINARY_PATH" ./cmd/liteone

cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=liteone
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BINARY_PATH} -config ${CONFIG_FILE}
Restart=always
RestartSec=3
WorkingDirectory=/var/lib/liteone
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now liteone
systemctl --no-pager --full status liteone || true

cat <<EOF

liteone has been installed.

Config:   ${CONFIG_FILE}
Binary:   ${BINARY_PATH}
Service:  liteone.service

Useful commands:
  journalctl -u liteone -f
  systemctl restart liteone
  systemctl status liteone
EOF
