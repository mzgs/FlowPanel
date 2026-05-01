#!/bin/sh
set -eu

APP="flowpanel"
REPO="mzgs/FlowPanel"
BIN_DIR="/usr/local/bin"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 is required." >&2
    exit 1
  }
}

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    need_cmd sudo
    sudo "$@"
  fi
}

download() {
  url="$1"
  out="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    echo "curl or wget is required." >&2
    exit 1
  fi
}

random_secret() {
  dd if=/dev/urandom bs=32 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
}

install_binary() {
  goos="$1"
  arch="$2"
  tmp_file="$(mktemp)"
  url="https://github.com/$REPO/releases/latest/download/$APP-$goos-$arch"

  trap 'rm -f "$tmp_file"' EXIT INT TERM
  echo "Downloading FlowPanel latest release for $goos/$arch..."
  download "$url" "$tmp_file"

  as_root mkdir -p "$BIN_DIR"
  as_root install -m 0755 "$tmp_file" "$BIN_DIR/$APP"
  rm -f "$tmp_file"
  trap - EXIT INT TERM
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

install_linux_service() {
  need_cmd systemctl

  env_dir="/etc/flowpanel"
  env_file="$env_dir/flowpanel.env"
  data_dir="/var/lib/flowpanel"
  service_file="/etc/systemd/system/$APP.service"

  as_root mkdir -p "$env_dir" "$data_dir"

  if [ ! -f "$env_file" ]; then
    secret="$(random_secret)"
    as_root sh -c "cat > '$env_file'" <<EOF
FLOWPANEL_ENV=production
FLOWPANEL_SESSION_SECRET=$secret
FLOWPANEL_ADMIN_LISTEN_ADDR=:8080
FLOWPANEL_DB_PATH=$data_dir/flowpanel.db
EOF
    as_root chmod 600 "$env_file"
  fi

  as_root sh -c "cat > '$service_file'" <<EOF
[Unit]
Description=FlowPanel
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
EnvironmentFile=$env_file
WorkingDirectory=$data_dir
ExecStart=$BIN_DIR/$APP
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  as_root systemctl daemon-reload
  as_root systemctl enable "$APP"
  as_root systemctl restart "$APP"

  echo "FlowPanel installed and started."
  echo "Service: systemctl status $APP"
  echo "Config:  $env_file"
}

install_macos_service() {
  env_dir="/usr/local/etc/flowpanel"
  env_file="$env_dir/flowpanel.env"
  data_dir="/Library/Application Support/FlowPanel"
  log_dir="/Library/Logs/FlowPanel"
  plist_file="/Library/LaunchDaemons/com.mzgs.flowpanel.plist"

  as_root mkdir -p "$env_dir" "$data_dir" "$log_dir"

  if [ ! -f "$env_file" ]; then
    secret="$(random_secret)"
    as_root sh -c "cat > '$env_file'" <<EOF
export FLOWPANEL_ENV=production
export FLOWPANEL_SESSION_SECRET=$secret
export FLOWPANEL_ADMIN_LISTEN_ADDR=:8080
export FLOWPANEL_DB_PATH='$data_dir/flowpanel.db'
EOF
    as_root chmod 600 "$env_file"
  fi

  as_root sh -c "cat > '$plist_file'" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.mzgs.flowpanel</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>-c</string>
    <string>. "$env_file"; exec "$BIN_DIR/$APP"</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$data_dir</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$log_dir/flowpanel.log</string>
  <key>StandardErrorPath</key>
  <string>$log_dir/flowpanel.err.log</string>
</dict>
</plist>
EOF

  as_root chown root:wheel "$plist_file"
  as_root chmod 644 "$plist_file"

  as_root launchctl bootout system "$plist_file" >/dev/null 2>&1 || true
  as_root launchctl bootstrap system "$plist_file"
  as_root launchctl enable system/com.mzgs.flowpanel
  as_root launchctl kickstart -k system/com.mzgs.flowpanel

  echo "FlowPanel installed and started."
  echo "Service: launchctl print system/com.mzgs.flowpanel"
  echo "Config:  $env_file"
}

arch="$(detect_arch)"

case "$(uname -s)" in
  Linux)
    install_binary linux "$arch"
    install_linux_service
    ;;
  Darwin)
    install_binary darwin "$arch"
    install_macos_service
    ;;
  *)
    echo "Unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac
