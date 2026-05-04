#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: release-linux-amd64.sh [tag] [options]

Builds the Linux amd64 binary and publishes it as a new GitHub release.
If tag is omitted, the next vMAJOR.MINOR.PATCH tag is selected automatically.
If the requested tag already exists, the patch version is increased until an
available tag is found.

Requirements: git, curl, GITHUB_TOKEN with repo release permissions, go, npm.

Options:
  --title <title>        Release title. Defaults to the tag.
  --notes <notes>        Release notes. Defaults to "Linux amd64 release."
  --draft                Create the release as a draft.
  --prerelease           Mark the release as a prerelease.
  --dry-run              Build and print the release action without publishing.
  -h, --help             Show this help.

Examples:
  ./release-linux-amd64.sh
  ./release-linux-amd64.sh v0.1.0
  ./release-linux-amd64.sh v0.1.1 --notes "Bug fixes"
EOF
}

increment_patch() {
  local raw="${1#v}"
  IFS=. read -r major minor patch <<<"$raw"
  printf 'v%s.%s.%s\n' "$major" "$minor" "$((patch + 1))"
}

is_semver_tag() {
  [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

latest_semver_tag() {
  {
    git tag --list 'v[0-9]*.[0-9]*.[0-9]*'
    git ls-remote --tags --refs origin 'v[0-9]*.[0-9]*.[0-9]*' | awk '{print $2}' | sed 's#refs/tags/##'
  } | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1
}

tag_exists() {
  git rev-parse -q --verify "refs/tags/$1" >/dev/null ||
    git ls-remote --exit-code --tags origin "refs/tags/$1" >/dev/null 2>&1
}

next_available_tag() {
  local candidate="$1"
  while tag_exists "$candidate"; do
    candidate="$(increment_patch "$candidate")"
  done
  printf '%s\n' "$candidate"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "$script_dir" rev-parse --show-toplevel)"
cd "$repo_root"

github_repo() {
  local url repo
  url="$(git config --get remote.origin.url)"
  case "$url" in
    https://github.com/* | https://*@github.com/*)
      repo="${url#*github.com/}"
      ;;
    git@github.com:*)
      repo="${url#git@github.com:}"
      ;;
    *)
      echo "cannot derive GitHub repo from origin URL: $url" >&2
      exit 1
      ;;
  esac
  repo="${repo%.git}"
  printf '%s\n' "$repo"
}

json_payload() {
  node - "$tag" "$commit" "$title" "$notes" "$draft" "$prerelease" <<'NODE'
const [tag, target, title, notes, draft, prerelease] = process.argv.slice(2);
process.stdout.write(JSON.stringify({
  tag_name: tag,
  target_commitish: target,
  name: title,
  body: notes,
  draft: draft === "true",
  prerelease: prerelease === "true",
  make_latest: prerelease === "true" ? "false" : "true",
}));
NODE
}

json_field() {
  node -e 'let s=""; process.stdin.on("data", d => s += d); process.stdin.on("end", () => {
    const v = JSON.parse(s)[process.argv[1]];
    if (v !== undefined && v !== null) process.stdout.write(String(v));
  });' "$1"
}

api_request() {
  local method="$1" url="$2" data="${3:-}" out status
  out="$(mktemp)"
  if [[ -n "$data" ]]; then
    status="$(curl -sS -w '%{http_code}' -o "$out" -X "$method" \
      -H "Authorization: Bearer $GITHUB_TOKEN" \
      -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      -H "Content-Type: application/json" \
      --data "$data" \
      "$url" || true)"
  else
    status="$(curl -sS -w '%{http_code}' -o "$out" -X "$method" \
      -H "Authorization: Bearer $GITHUB_TOKEN" \
      -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      "$url" || true)"
  fi

  if [[ ! "$status" =~ ^2 ]]; then
    echo "GitHub API request failed ($status): $url" >&2
    cat "$out" >&2
    rm -f "$out"
    exit 1
  fi

  cat "$out"
  rm -f "$out"
}

tag=""
title=""
notes="Linux amd64 release."
dry_run=0
draft=false
prerelease=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --title)
      [[ $# -ge 2 ]] || { echo "--title requires a value" >&2; exit 1; }
      title="$2"
      shift 2
      ;;
    --notes)
      [[ $# -ge 2 ]] || { echo "--notes requires a value" >&2; exit 1; }
      notes="$2"
      shift 2
      ;;
    --draft)
      draft=true
      shift
      ;;
    --prerelease)
      prerelease=true
      shift
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    --*)
      echo "unknown option: $1" >&2
      usage
      exit 1
      ;;
    *)
      if [[ -n "$tag" ]]; then
        echo "unexpected argument: $1" >&2
        usage
        exit 1
      fi
      tag="$1"
      shift
      ;;
  esac
done

asset="dist/flowpanel-linux-amd64"
commit="$(git rev-parse HEAD)"

for cmd in git curl node go npm; do
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "$cmd is required" >&2
    exit 1
  }
done

repo="$(github_repo)"

if [[ -n "$tag" ]] && ! is_semver_tag "$tag"; then
  echo "tag must use vMAJOR.MINOR.PATCH format, for example v0.1.0" >&2
  exit 1
fi

if [[ -z "$tag" ]]; then
  latest_tag="$(latest_semver_tag || true)"
  tag="$(increment_patch "${latest_tag:-v0.0.0}")"
fi

requested_tag="$tag"
tag="$(next_available_tag "$tag")"
title="${title:-$tag}"

if [[ "$tag" != "$requested_tag" ]]; then
  echo "Tag $requested_tag exists; using $tag"
else
  echo "Using tag $tag"
fi

if [[ "${ALLOW_DIRTY:-0}" != "1" && -n "$(git status --porcelain)" ]]; then
  echo "working tree is dirty; commit or stash changes first, or run with ALLOW_DIRTY=1" >&2
  exit 1
fi

echo "Building $asset"
./build.sh linux amd64

if [[ ! -x "$asset" ]]; then
  echo "expected executable asset was not created: $asset" >&2
  exit 1
fi

if [[ "$dry_run" == "1" ]]; then
  echo "Dry run: would create release $tag in $repo and upload $asset"
  exit 0
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "GITHUB_TOKEN is required to publish the release" >&2
  exit 1
fi

echo "Publishing $tag"
release_json="$(api_request POST "https://api.github.com/repos/$repo/releases" "$(json_payload)")"
upload_url="$(printf '%s' "$release_json" | json_field upload_url)"
upload_url="${upload_url%\{*}?name=$(basename "$asset")"

curl -fsS -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  -H "Content-Type: application/octet-stream" \
  --data-binary "@$asset" \
  "$upload_url" >/dev/null

echo "Published $tag with $asset"
