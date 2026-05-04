#!/usr/bin/env bash
set -euo pipefail

# Make common package-manager binary locations visible in stripped shells.
PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

repo="${N8NCTL_REPO:-LogicMonitor-IT/n8nctl}"
module_path="${N8NCTL_MODULE:-github.com/LogicMonitor-IT/n8nctl}"
requested_version="latest"
setup_path="false"
install_dir="${N8NCTL_INSTALL_DIR:-}"
release_error=""
gh_bin=""
go_bin=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --setup-path)
      setup_path="true"
      shift
      ;;
    -*)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
    *)
      requested_version="$1"
      shift
      ;;
  esac
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$arch" in
  x86_64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

case "$os" in
  darwin|linux) ;;
  *)
    echo "unsupported operating system: $os" >&2
    exit 1
    ;;
esac

find_tool() {
  local tool_name="$1"
  local candidate

  if candidate="$(command -v "$tool_name" 2>/dev/null)"; then
    printf '%s\n' "$candidate"
    return 0
  fi

  for candidate in "/opt/homebrew/bin/$tool_name" "/usr/local/bin/$tool_name"; do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

path_contains() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

existing_install_dir() {
  local current_bin current_dir

  if ! current_bin="$(command -v n8nctl 2>/dev/null)"; then
    return 1
  fi

  current_dir="$(dirname "$current_bin")"
  if [[ -w "$current_dir" ]]; then
    printf '%s\n' "$current_dir"
    return 0
  fi

  return 1
}

gopath_bin_dir() {
  local gopath

  if [[ -z "$go_bin" ]]; then
    return 1
  fi

  gopath="$("$go_bin" env GOPATH 2>/dev/null || true)"
  if [[ -z "$gopath" ]]; then
    return 1
  fi

  printf '%s\n' "$gopath/bin"
}

pick_install_dir() {
  local candidate

  if [[ -n "${install_dir}" ]]; then
    printf '%s\n' "${install_dir}"
    return 0
  fi

  if candidate="$(existing_install_dir)"; then
    printf '%s\n' "$candidate"
    return 0
  fi

  if candidate="$(gopath_bin_dir)"; then
    if [[ -x "$candidate/n8nctl" ]] || path_contains "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  fi

  local candidates=(
    "$HOME/.local/bin"
    "$HOME/bin"
  )

  for candidate in "${candidates[@]}"; do
    if path_contains "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  printf '%s\n' "$HOME/.local/bin"
}

detect_shell_rc() {
  local shell_name
  shell_name="$(basename "${SHELL:-}")"

  case "$shell_name" in
    zsh) printf '%s\n' "$HOME/.zshrc" ;;
    bash) printf '%s\n' "$HOME/.bashrc" ;;
    *) printf '%s\n' "" ;;
  esac
}

ensure_path_message() {
  local target_dir="$1"

  if path_contains "$target_dir"; then
    echo "n8nctl is available on PATH"
    return 0
  fi

  local export_line="export PATH=\"$target_dir:\$PATH\""
  local shell_rc
  shell_rc="$(detect_shell_rc)"

  if [[ "$setup_path" != "true" ]]; then
    echo "n8nctl was installed to $target_dir, which is not currently on PATH"
    echo "add it for this shell with:"
    echo "  $export_line"
    if [[ -n "$shell_rc" ]]; then
      echo "persist it for future shells with:"
      echo "  echo '$export_line' >> \"$shell_rc\""
      echo "  source \"$shell_rc\""
    fi
    return 0
  fi

  if [[ -z "$shell_rc" ]]; then
    echo "cannot auto-configure PATH for shell ${SHELL:-unknown}" >&2
    echo "add this line manually:" >&2
    echo "  $export_line" >&2
    exit 1
  fi

  mkdir -p "$(dirname "$shell_rc")"
  touch "$shell_rc"

  if ! grep -Fqx "$export_line" "$shell_rc"; then
    {
      echo ""
      echo "# Added by n8nctl installer"
      echo "$export_line"
    } >> "$shell_rc"
  fi

  echo "added $target_dir to PATH in $shell_rc"
  echo "run this to activate it now:"
  echo "  source \"$shell_rc\""
}

find_local_module_root() {
  local dir="${PWD}"
  local mod_file
  local module_name

  while [[ "$dir" != "/" ]]; do
    mod_file="$dir/go.mod"
    if [[ -f "$mod_file" ]]; then
      module_name="$(sed -n 's/^module[[:space:]]*//p' "$mod_file" | head -n 1)"
      if [[ "$module_name" == "$module_path" ]]; then
        printf '%s\n' "$dir"
        return 0
      fi
    fi
    dir="$(dirname "$dir")"
  done

  return 1
}

install_from_release() {
  local version asset_version asset tmp_dir

  if [[ -z "$gh_bin" ]]; then
    release_error=""
    return 1
  fi

  if [[ "$requested_version" == "latest" ]]; then
    if ! version="$("$gh_bin" release view --repo "$repo" --json tagName --jq .tagName 2>/dev/null)"; then
      release_error="no GitHub release found for $repo"
      return 1
    fi
  else
    version="$requested_version"
    if ! "$gh_bin" release view "$version" --repo "$repo" >/dev/null 2>&1; then
      release_error="GitHub release $version not found for $repo"
      return 1
    fi
  fi

  asset_version="${version#v}"
  asset="n8nctl_${asset_version}_${os}_${arch}.tar.gz"
  tmp_dir="$(mktemp -d)"

  if ! "$gh_bin" release download "$version" --repo "$repo" --pattern "$asset" --dir "$tmp_dir"; then
    rm -rf "$tmp_dir"
    release_error="failed to download $asset from GitHub Releases"
    return 1
  fi

  if ! tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"; then
    rm -rf "$tmp_dir"
    release_error="failed to unpack $asset"
    return 1
  fi

  if ! install "$tmp_dir/n8nctl" "$install_dir/n8nctl"; then
    rm -rf "$tmp_dir"
    release_error="failed to install n8nctl into $install_dir"
    return 1
  fi
  rm -rf "$tmp_dir"

  echo "installed n8nctl ${version} to ${install_dir}/n8nctl"
  ensure_path_message "$install_dir"
  return 0
}

install_from_local_checkout() {
  local local_root

  if [[ -z "$go_bin" ]]; then
    return 1
  fi

  if ! local_root="$(find_local_module_root)"; then
    return 1
  fi

  if ! (
    cd "$local_root"
    GOBIN="$install_dir" "$go_bin" install .
  ); then
    return 1
  fi

  echo "installed n8nctl to ${install_dir}/n8nctl from the local checkout"
  ensure_path_message "$install_dir"
  return 0
}

install_from_module() {
  if [[ -z "$go_bin" ]]; then
    return 1
  fi

  if ! GOBIN="$install_dir" "$go_bin" install "${module_path}@${requested_version}"; then
    return 1
  fi
  echo "installed n8nctl to ${install_dir}/n8nctl using go install"
  ensure_path_message "$install_dir"
  return 0
}

gh_bin="$(find_tool gh || true)"
go_bin="$(find_tool go || true)"
install_dir="$(pick_install_dir)"
mkdir -p "$install_dir"

if install_from_release; then
  exit 0
fi

if [[ -n "$release_error" ]]; then
  echo "$release_error; falling back to go install" >&2
fi

if install_from_local_checkout; then
  exit 0
fi

if install_from_module; then
  exit 0
fi

echo "neither gh nor go is installed in a discoverable location; cannot install n8nctl" >&2
exit 1
