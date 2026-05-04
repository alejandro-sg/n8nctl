#!/usr/bin/env sh

# Source this file to resolve .env.1password references into the current shell.
# Example: source scripts/load-1password-env.sh

is_sourced() {
  if [ -n "${ZSH_VERSION:-}" ]; then
    case "${ZSH_EVAL_CONTEXT:-}" in
      *:file:*) return 0 ;;
    esac
    return 1
  fi

  if [ -n "${BASH_VERSION:-}" ]; then
    [ "${BASH_SOURCE:-}" != "$0" ]
    return $?
  fi

  return 1
}

trim() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

unquote() {
  value="$1"
  case "$value" in
    \"*\")
      value="${value#\"}"
      value="${value%\"}"
      ;;
    \'*\')
      value="${value#\'}"
      value="${value%\'}"
      ;;
  esac
  printf '%s' "$value"
}

fail() {
  printf 'load-1password-env: %s\n' "$1" >&2
  return 1
}

find_env_file() {
  if [ -n "${1:-}" ]; then
    printf '%s\n' "$1"
    return 0
  fi

  dir="$PWD"
  while [ "$dir" != "/" ]; do
    if [ -f "$dir/.env.1password" ]; then
      printf '%s\n' "$dir/.env.1password"
      return 0
    fi
    dir="$(dirname "$dir")"
  done

  printf '%s\n' ".env.1password"
}

if ! is_sourced; then
  printf 'load-1password-env: this script must be sourced so it can export variables into your current shell\n' >&2
  printf 'usage: source scripts/load-1password-env.sh [env-file]\n' >&2
  exit 1
fi

env_file="$(find_env_file "${1:-}")"

if [ ! -f "$env_file" ]; then
  fail "env file not found: $env_file"
  return 1
fi

if ! command -v op >/dev/null 2>&1; then
  fail "1Password CLI not found on PATH"
  return 1
fi

loaded=0

while IFS= read -r raw_line || [ -n "$raw_line" ]; do
  line="$(trim "$raw_line")"

  case "$line" in
    ''|\#*) continue ;;
  esac

  case "$line" in
    *=*) ;;
    *)
      fail "invalid line in $env_file: $raw_line"
      return 1
      ;;
  esac

  name="$(trim "${line%%=*}")"
  value="$(trim "${line#*=}")"
  value="$(unquote "$value")"

  if ! printf '%s' "$name" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]*$'; then
    fail "invalid environment variable name: $name"
    return 1
  fi

  case "$value" in
    op://*) ;;
    *)
      fail "$name must use an op:// secret reference"
      return 1
      ;;
  esac

  secret="$(op read "$value")" || {
    fail "failed to read 1Password secret for $name"
    return 1
  }

  export "$name=$secret"
  loaded=$((loaded + 1))
done < "$env_file"

printf 'load-1password-env: loaded %s secret(s) from %s into this shell\n' "$loaded" "$env_file"
