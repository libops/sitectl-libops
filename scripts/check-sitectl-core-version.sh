#!/bin/sh
set -eu

minimum="${1:-v1.0.0}"
root_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
go_mod="$root_dir/go.mod"
version="$(awk '
  $1 == "require" && $2 == "(" { in_require = 1; next }
  in_require && $1 == ")" { in_require = 0; next }
  $1 == "require" && $2 == "github.com/libops/sitectl" { print $3; exit }
  in_require && $1 == "github.com/libops/sitectl" { print $2; exit }
' "$go_mod")"

if [ -z "$version" ]; then
  echo "github.com/libops/sitectl must be a direct go.mod requirement" >&2
  exit 1
fi

if awk '
  $1 == "replace" && $2 == "(" { in_replace = 1; next }
  in_replace && $1 == ")" { in_replace = 0; next }
  $1 == "replace" && $2 == "github.com/libops/sitectl" { found = 1 }
  in_replace && $1 == "github.com/libops/sitectl" { found = 1 }
  END { exit !found }
' "$go_mod"; then
  echo "github.com/libops/sitectl must not use a go.mod replace directive in a release" >&2
  exit 1
fi

if ! printf '%s\n' "$minimum" | grep -Eq '^v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$'; then
  echo "minimum sitectl version $minimum is not a stable semantic version" >&2
  exit 1
fi

if ! printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$'; then
  echo "github.com/libops/sitectl $version is not a stable release; release plugins against stable $minimum or newer" >&2
  exit 1
fi

version_is_at_least() {
  current="${1#v}"
  required="${2#v}"
  original_ifs="$IFS"

  IFS=.
  set -- $current
  current_major="$1"
  current_minor="$2"
  current_patch="$3"
  set -- $required
  required_major="$1"
  required_minor="$2"
  required_patch="$3"
  IFS="$original_ifs"

  [ "$current_major" -gt "$required_major" ] && return 0
  [ "$current_major" -lt "$required_major" ] && return 1
  [ "$current_minor" -gt "$required_minor" ] && return 0
  [ "$current_minor" -lt "$required_minor" ] && return 1
  [ "$current_patch" -ge "$required_patch" ]
}

if ! version_is_at_least "$version" "$minimum"; then
  echo "github.com/libops/sitectl $version is too old; bump go.mod to stable $minimum or newer before releasing" >&2
  exit 1
fi
