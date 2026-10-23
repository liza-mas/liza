#!/bin/sh
# PAIRING-INDEX-HOOK: managed
set -eu

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$repo_root" ]; then
	exit 0
fi

hook_name="${PAIRING_INDEX_HOOK_NAME:-$(basename "$0")}"
unset PAIRING_INDEX_HOOK_NAME
if [ "$hook_name" = "post-checkout" ] && [ "${3:-}" = "0" ]; then
	exit 0
fi

case "$repo_root" in
	*/.worktrees/*)
		exit 0
		;;
esac

hook_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 0
script="$hook_dir/liza-index.sh"
if [ ! -x "$script" ]; then
	exit 0
fi

cd "$repo_root"
semble_gate=$(printf '%s' "${LIZA_ENABLE_SEMBLE-${LIZA_ENABLE_SEMBLE:-}}" | tr '[:upper:]' '[:lower:]' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
if [ "$hook_name" = "post-commit" ] && [ "$semble_gate" != "1" ] && [ "$semble_gate" != "true" ]; then
	# Inspect root commits and every merge parent. Disabling rename detection
	# keeps code renamed to documentation visible as a code deletion.
	# --quiet does not reliably report merge differences. Check the path list;
	# only a successful empty result skips refresh, and errors fall through.
	if changed_paths="$(git diff-tree --root --no-commit-id --name-only -r -m --no-renames HEAD -- . \
		':(glob,exclude,icase)**/*.md' \
		':(glob,exclude,icase)**/*.rst' \
		':(glob,exclude,icase)**/*.adoc' \
		':(glob,exclude,icase)specs/plans/**/*-output.json' \
		':(glob,exclude,icase)specs/epics/**/*-output.json' \
		':(glob,exclude,icase)specs/arch-plan/**/*-output.json' 2>/dev/null)" && [ -z "$changed_paths" ]; then
		exit 0
	fi
fi

index_binary='/fixture/bin/agent-cli'
if [ -n "$index_binary" ] && [ ! -x "$index_binary" ]; then
	index_binary="$(command -v liza 2>/dev/null || true)"
fi
if [ -z "$index_binary" ]; then
	echo "liza-index-hook.sh: liza not found; skipping index refresh" >&2
	exit 0
fi
exec "$index_binary" index-refresh --trigger "$hook_name"
