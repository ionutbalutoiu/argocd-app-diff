#!/usr/bin/env sh

set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"

cd "$repo_root"

files="$(git ls-files '*.go')"

if [ -z "$files" ]; then
	exit 0
fi

unformatted="$(printf '%s\n' "$files" | xargs gofmt -l)"

if [ -n "$unformatted" ]; then
	echo "The following files are not gofmt-formatted:"
	echo "$unformatted"
	exit 1
fi
