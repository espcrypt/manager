#!/usr/bin/env bash
set -euo pipefail

REPO="espcrypt/manager"
BIN_DIR="${ESPCRYPT_BIN_DIR:-/usr/local/bin}"

bold=$'\033[1m'; dim=$'\033[2m'; green=$'\033[32m'; red=$'\033[31m'; reset=$'\033[0m'
say()  { printf '%s==>%s %s\n' "$bold" "$reset" "$*"; }
ok()   { printf '%s✓%s %s\n' "$green" "$reset" "$*"; }
die()  { printf '%s✗ %s%s\n' "$red" "$*" "$reset" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || die "this installer is for macOS (use install-linux.sh on Linux)"

token="${GITHUB_TOKEN:-}"
if [ -z "$token" ] && command -v gh >/dev/null 2>&1; then token="$(gh auth token 2>/dev/null || true)"; fi
auth=()
[ -n "$token" ] && auth=(-H "Authorization: Bearer $token")

api() { curl -fsSL "${auth[@]}" -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/$1"; }

say "Looking up the latest release"
if [ -n "${ESPCRYPT_VERSION:-}" ]; then
  release="$(api "releases/tags/v${ESPCRYPT_VERSION#v}")" || die "release v${ESPCRYPT_VERSION#v} not found"
else
  release="$(api releases/latest)" || die "cannot reach the release list (private repository? set GITHUB_TOKEN)"
fi
tag="$(printf '%s' "$release" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')"
[ -n "$tag" ] || die "no release found"

asset_url() {
  printf '%s' "$release" | tr -d '\n' \
    | grep -o "\"url\": *\"https://api.github.com/repos/[^\"]*/releases/assets/[0-9]*\"[^{}]*\"name\": *\"$1\"" \
    | head -1 | grep -o 'https://api.github.com/repos/[^"]*/releases/assets/[0-9]*' || true
}

download() {
  curl -fL --progress-bar "${auth[@]}" -H "Accept: application/octet-stream" -o "$2" "$1"
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cli_url="$(asset_url 'espcrypt-cli-[^"]*-macOS-universal\.tar\.gz')"
[ -n "$cli_url" ] || die "release $tag has no macOS CLI"
say "Downloading espcrypt CLI $tag"
download "$cli_url" "$tmp/cli.tar.gz"
tar -xzf "$tmp/cli.tar.gz" -C "$tmp"
xattr -c "$tmp/espcrypt" 2>/dev/null || true
chmod 755 "$tmp/espcrypt"

sudo=""
if ! mkdir -p "$BIN_DIR" 2>/dev/null || [ ! -w "$BIN_DIR" ]; then
  say "Installing to $BIN_DIR needs your password"
  sudo="sudo"
  $sudo mkdir -p "$BIN_DIR"
fi
$sudo rm -f "$BIN_DIR/espcrypt"
$sudo install -m 755 "$tmp/espcrypt" "$BIN_DIR/espcrypt"
ok "espcrypt installed in $BIN_DIR"

if [ "${ESPCRYPT_CLI_ONLY:-0}" != "1" ]; then
  app_url="$(asset_url 'ESP-Crypt-[^"]*-macOS-universal\.zip')"
  if [ -n "$app_url" ]; then
    say "Downloading ESP Crypt.app $tag"
    download "$app_url" "$tmp/app.zip"
    ditto -x -k "$tmp/app.zip" "$tmp/app"
    app="$(find "$tmp/app" -maxdepth 1 -name '*.app' | head -1)"
    [ -n "$app" ] || die "the app archive is empty"
    pkill -x "ESP-Crypt" 2>/dev/null || true
    dest="/Applications"
    if [ ! -w "$dest" ]; then dest="$HOME/Applications"; mkdir -p "$dest"; fi
    rm -rf "$dest/$(basename "$app")"
    ditto "$app" "$dest/$(basename "$app")"
    xattr -dr com.apple.quarantine "$dest/$(basename "$app")" 2>/dev/null || true
    /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$dest/$(basename "$app")" >/dev/null 2>&1 || true
    ok "$(basename "$app") installed in $dest"
  fi
fi

case ":$PATH:" in *":$BIN_DIR:"*) ;; *) printf '%sAdd %s to your PATH to use espcrypt.%s\n' "$dim" "$BIN_DIR" "$reset" ;; esac
printf '\n%sDone.%s Run %sespcrypt%s to get started.\n' "$bold" "$reset" "$bold" "$reset"
