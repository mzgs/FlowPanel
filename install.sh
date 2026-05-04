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

random_hex() {
  dd if=/dev/urandom bs="$1" count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
}

random_secret() {
  random_hex 32
}

random_admin_username() {
  printf 'admin-%s\n' "$(random_hex 4)"
}

random_admin_password() {
  random_hex 24
}

ensure_env_key() {
  env_file="$1"
  key="$2"
  value="$3"
  prefix="$4"

  if ! grep -q "^$prefix$key=" "$env_file" 2>/dev/null; then
    as_root sh -c "cat >> '$env_file'" <<EOF
$prefix$key=$value
EOF
  fi
}

read_env_key() {
  sed -n "s/^\(export \)\?$2=//p" "$1" 2>/dev/null | tail -n 1
}

public_ip() {
  ip_addr=""
  for url in https://api.ipify.org https://ipv4.icanhazip.com https://v4.ident.me; do
    if command -v curl >/dev/null 2>&1; then
      ip_addr="$(curl -fsSL --max-time 5 "$url" 2>/dev/null | tr -d '[:space:]' || true)"
    elif command -v wget >/dev/null 2>&1; then
      ip_addr="$(wget -qO- -T 5 -t 1 "$url" 2>/dev/null | tr -d '[:space:]' || true)"
    else
      break
    fi
    case "$ip_addr" in
      *[!0-9.]* | "" | *.*.*.*.*) ip_addr="" ;;
      *.*.*.*) printf '%s\n' "$ip_addr"; return 0 ;;
    esac
  done
  return 0
}

primary_ip() {
  ip_addr=""
  if command -v ip >/dev/null 2>&1; then
    ip_addr="$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([^ ]*\).*/\1/p' | head -n 1 || true)"
  fi
  if [ -z "$ip_addr" ] && command -v hostname >/dev/null 2>&1; then
    ip_addr="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  fi
  if [ -z "$ip_addr" ] && command -v ipconfig >/dev/null 2>&1; then
    for iface in en0 en1; do
      ip_addr="$(ipconfig getifaddr "$iface" 2>/dev/null || true)"
      [ -n "$ip_addr" ] && break
    done
  fi
  if [ -z "$ip_addr" ] && command -v ifconfig >/dev/null 2>&1; then
    ip_addr="$(ifconfig 2>/dev/null | awk '/inet / && $2 != "127.0.0.1" { sub(/^addr:/, "", $2); print $2; exit }' || true)"
  fi
  printf '%s\n' "$ip_addr"
}

admin_panel_host() {
  ip_addr="$(public_ip)"
  [ -n "$ip_addr" ] || ip_addr="$(primary_ip)"
  printf '%s\n' "${ip_addr:-localhost}"
}

admin_panel_url() {
  addr="$(read_env_key "$1" FLOWPANEL_ADMIN_LISTEN_ADDR | sed "s/^['\"]//;s/['\"]$//")"
  if [ -z "$addr" ]; then
    addr=":8080"
  fi

  case "$addr" in
    :*) printf 'http://%s%s\n' "$(admin_panel_host)" "$addr" ;;
    0.0.0.0:*) printf 'http://%s:%s\n' "$(admin_panel_host)" "${addr#0.0.0.0:}" ;;
    "[::]:"*) printf 'http://%s:%s\n' "$(admin_panel_host)" "${addr##*:}" ;;
    *) printf 'http://%s\n' "$addr" ;;
  esac
}

print_success() {
  action="$1"
  service_command="$2"
  env_file="$3"
  admin_username="$4"
  admin_password="$5"
  installed_version="$6"

  if [ "$action" = "update" ]; then
    echo
    echo "FlowPanel updated and restarted successfully."
  else
    echo
    cat <<'EOF'
  _____ _                 ____                  _
 |  ___| | _____      __ |  _ \ __ _ _ __   ___| |
 | |_  | |/ _ \ \ /\ / / | |_) / _` | '_ \ / _ \ |
 |  _| | | (_) \ V  V /  |  __/ (_| | | | |  __/ |
 |_|   |_|\___/ \_/\_/   |_|   \__,_|_| |_|\___|_|
EOF
    echo "FlowPanel installed and started successfully."
  fi
  if [ -n "$installed_version" ]; then
    echo "Version:     $installed_version"
  fi
  echo "Admin panel: $(admin_panel_url "$env_file")"
  echo "Username:    $admin_username"
  echo "Password:    $admin_password"
  echo "Service:     $service_command"
  echo "Config:      $env_file"
}

installed_action() {
  if "$@"; then
    echo "update"
  else
    echo "install"
  fi
}

linux_installed() {
  [ -x "$BIN_DIR/$APP" ] || [ -f "/etc/systemd/system/$APP.service" ] || [ -f "/etc/flowpanel/flowpanel.env" ]
}

macos_installed() {
  [ -x "$BIN_DIR/$APP" ] || [ -f "/Library/LaunchDaemons/com.mzgs.flowpanel.plist" ] || [ -f "/usr/local/etc/flowpanel/flowpanel.env" ]
}

install_binary() {
  goos="$1"
  arch="$2"
  action="$3"
  tmp_file="$(mktemp)"
  url="https://github.com/$REPO/releases/latest/download/$APP-$goos-$arch"

  trap 'rm -f "$tmp_file"' EXIT INT TERM
  if [ "$action" = "update" ]; then
    echo "FlowPanel is already installed. Downloading latest release for $goos/$arch..."
  else
    echo "Downloading FlowPanel latest release for $goos/$arch..."
  fi
  download "$url" "$tmp_file"

  as_root mkdir -p "$BIN_DIR"
  as_root install -m 0755 "$tmp_file" "$BIN_DIR/$APP"
  rm -f "$tmp_file"
  trap - EXIT INT TERM
}

installed_version() {
  if [ -x "$BIN_DIR/$APP" ]; then
    "$BIN_DIR/$APP" version 2>/dev/null || true
  fi
}

stop_linux_service() {
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet "$APP"; then
    echo "Stopping running FlowPanel service..."
    as_root systemctl stop "$APP"
  fi
}

stop_macos_service() {
  plist_file="/Library/LaunchDaemons/com.mzgs.flowpanel.plist"
  if [ -f "$plist_file" ] && as_root launchctl print system/com.mzgs.flowpanel >/dev/null 2>&1; then
    echo "Stopping running FlowPanel service..."
    as_root launchctl bootout system "$plist_file" >/dev/null 2>&1 || true
  fi
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
  action="$1"

  need_cmd systemctl

  env_dir="/etc/flowpanel"
  env_file="$env_dir/flowpanel.env"
  data_dir="/var/lib/flowpanel"
  service_file="/etc/systemd/system/$APP.service"

  as_root mkdir -p "$env_dir" "$data_dir"

  if [ ! -f "$env_file" ]; then
    secret="$(random_secret)"
    admin_username="$(random_admin_username)"
    admin_password="$(random_admin_password)"
    as_root sh -c "cat > '$env_file'" <<EOF
FLOWPANEL_ENV=production
FLOWPANEL_ENV_FILE=$env_file
FLOWPANEL_SESSION_SECRET=$secret
FLOWPANEL_SESSION_COOKIE_SECURE=false
FLOWPANEL_ADMIN_LISTEN_ADDR=:8080
FLOWPANEL_DB_PATH=$data_dir/flowpanel.db
FLOWPANEL_ADMIN_USERNAME=$admin_username
FLOWPANEL_ADMIN_PASSWORD=$admin_password
EOF
    as_root chmod 600 "$env_file"
  else
    ensure_env_key "$env_file" FLOWPANEL_ENV_FILE "$env_file" ""
    admin_username="$(read_env_key "$env_file" FLOWPANEL_ADMIN_USERNAME)"
    admin_password="$(read_env_key "$env_file" FLOWPANEL_ADMIN_PASSWORD)"
    if [ -z "$admin_username" ]; then
      ensure_env_key "$env_file" FLOWPANEL_ADMIN_USERNAME "$(random_admin_username)" ""
      admin_username="$(read_env_key "$env_file" FLOWPANEL_ADMIN_USERNAME)"
    fi
    if [ -z "$admin_password" ]; then
      ensure_env_key "$env_file" FLOWPANEL_ADMIN_PASSWORD "$(random_admin_password)" ""
      admin_password="$(read_env_key "$env_file" FLOWPANEL_ADMIN_PASSWORD)"
    fi
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
ExecStart=$BIN_DIR/$APP serve
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  as_root systemctl daemon-reload
  as_root systemctl enable "$APP"
  as_root systemctl restart "$APP"

  print_success "$action" "systemctl status $APP" "$env_file" "$admin_username" "$admin_password" "$(installed_version)"
}

install_macos_service() {
  action="$1"

  env_dir="/usr/local/etc/flowpanel"
  env_file="$env_dir/flowpanel.env"
  data_dir="/Library/Application Support/FlowPanel"
  log_dir="/Library/Logs/FlowPanel"
  plist_file="/Library/LaunchDaemons/com.mzgs.flowpanel.plist"

  as_root mkdir -p "$env_dir" "$data_dir" "$log_dir"

  if [ ! -f "$env_file" ]; then
    secret="$(random_secret)"
    admin_username="$(random_admin_username)"
    admin_password="$(random_admin_password)"
    as_root sh -c "cat > '$env_file'" <<EOF
export FLOWPANEL_ENV=production
export FLOWPANEL_ENV_FILE='$env_file'
export FLOWPANEL_SESSION_SECRET=$secret
export FLOWPANEL_SESSION_COOKIE_SECURE=false
export FLOWPANEL_ADMIN_LISTEN_ADDR=:8080
export FLOWPANEL_DB_PATH='$data_dir/flowpanel.db'
export FLOWPANEL_ADMIN_USERNAME=$admin_username
export FLOWPANEL_ADMIN_PASSWORD=$admin_password
EOF
    as_root chmod 600 "$env_file"
  else
    ensure_env_key "$env_file" FLOWPANEL_ENV_FILE "'$env_file'" "export "
    admin_username="$(read_env_key "$env_file" FLOWPANEL_ADMIN_USERNAME)"
    admin_password="$(read_env_key "$env_file" FLOWPANEL_ADMIN_PASSWORD)"
    if [ -z "$admin_username" ]; then
      ensure_env_key "$env_file" FLOWPANEL_ADMIN_USERNAME "$(random_admin_username)" "export "
      admin_username="$(read_env_key "$env_file" FLOWPANEL_ADMIN_USERNAME)"
    fi
    if [ -z "$admin_password" ]; then
      ensure_env_key "$env_file" FLOWPANEL_ADMIN_PASSWORD "$(random_admin_password)" "export "
      admin_password="$(read_env_key "$env_file" FLOWPANEL_ADMIN_PASSWORD)"
    fi
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
    <string>. "$env_file"; exec "$BIN_DIR/$APP" serve</string>
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

  print_success "$action" "launchctl print system/com.mzgs.flowpanel" "$env_file" "$admin_username" "$admin_password" "$(installed_version)"
}

arch="$(detect_arch)"

case "$(uname -s)" in
  Linux)
    action="$(installed_action linux_installed)"
    stop_linux_service
    install_binary linux "$arch" "$action"
    install_linux_service "$action"
    ;;
  Darwin)
    action="$(installed_action macos_installed)"
    stop_macos_service
    install_binary darwin "$arch" "$action"
    install_macos_service "$action"
    ;;
  *)
    echo "Unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac
