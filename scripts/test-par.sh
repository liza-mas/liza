#!/usr/bin/env bash
# test-par.sh runs the same tests as `go test ./...`, but splits the slowest
# packages into several `go test -run` processes so that their serial tests
# overlap. Separate processes isolate per-process state that t.Parallel cannot
# share: t.Setenv, working-directory changes, and the oracle A9 check that no
# other scenario owns a child of the test process (internal/oracle/a9/orchreap_test.go).
#
# Extra arguments are passed to every go test invocation (e.g. -short, -count=1).
#
# Ceiling: shard counts are fixed and tests are dealt round-robin by name, so a
# shard can still be dominated by one long test. Revisit the counts when
# `make test-par` stops beating `make test` by a clear margin.
set -euo pipefail

# mapfile needs bash 4, and expanding an empty array under set -u needs 4.4.
# macOS ships bash 3.2; install a newer one (e.g. Homebrew) first on PATH.
if ((BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4))); then
	echo "test-par: needs bash 4.4 or later, found $BASH_VERSION" >&2
	exit 2
fi

# package=shard count, sized from `go test -json ./...` on an 8-core host
# (2026-11): a9 341s, ops 277s, agent 272s, cmd/liza 199s, regen 153s,
# a4 146s, integration 151s, commands 109s.
# a9 runs as a single process: split 4 ways, its signal/reap tests failed
# about 1 run in 7 for a cause not yet found (TECH_DEBT.md, "Oracle A9
# reap/scan helpers fail intermittently under `make test-par`").
SPLIT=(
	./internal/oracle/a9=1
	./internal/agent=4
	./internal/ops=3
	./cmd/liza=3
	./internal/oracle/a4=2
	./internal/integration=2
	./internal/commands=2
)

# Build-selection flags change which packages, files and tests exist, so
# discovery below gets them too. Ceiling: only -tags and -race are recognised;
# another selection flag (-mod, -overlay, ...) would need adding here.
build=()
args=("$@")
for ((a = 0; a < ${#args[@]}; a++)); do
	case ${args[a]} in
	-tags=* | --tags=* | -race | --race) build+=("${args[a]}") ;;
	-tags | --tags) build+=("${args[a]}" "${args[a + 1]:-}") && a=$((a + 1)) ;;
	esac
done

logs=$(mktemp -d "${TMPDIR:-/tmp}/test-par.XXXXXX")
trap 'rm -rf "$logs"' EXIT

pids=()
labels=()

launch() { # label, go test args...
	local label=$1
	shift
	go test "$@" >"$logs/$label.log" 2>&1 &
	pids+=("$!")
	labels+=("$label")
}

# Discovery output is captured by plain assignments, not process
# substitutions, so that set -e stops the script when go list or go test -list fails.
split_pkgs=()
for entry in "${SPLIT[@]}"; do
	pkg=${entry%=*}
	n=${entry#*=}
	listed=$(go list "${build[@]}" "$pkg")
	split_pkgs+=("$listed")
	# -list prints top-level Test/Example/Fuzz names; anything else is noise.
	listed=$(go test -list . "${build[@]}" "$pkg")
	mapfile -t names < <(grep -E '^(Test|Example|Fuzz)[[:alnum:]_]*$' <<<"$listed" || true)
	if [ "${#names[@]}" -eq 0 ]; then
		echo "test-par: no tests listed for $pkg" >&2
		exit 1
	fi
	for ((i = 0; i < n; i++)); do
		pattern=""
		for ((j = i; j < ${#names[@]}; j += n)); do
			pattern+="${pattern:+|}${names[j]}"
		done
		label=${pkg#./}
		[ -n "$pattern" ] && launch "${label//\//_}.$((i + 1))" "$@" -run "^($pattern)\$" "$pkg"
	done
done

listed=$(go list "${build[@]}" ./...)
mapfile -t rest < <(grep -vxF -f <(printf '%s\n' "${split_pkgs[@]}") <<<"$listed" || true)
# With no package arguments, go test would run the current directory instead.
if [ "${#rest[@]}" -gt 0 ]; then
	launch rest "$@" "${rest[@]}"
fi

failed=0
for k in "${!pids[@]}"; do
	if wait "${pids[k]}"; then
		# Slowest package time in the process, from go test's "ok <pkg> <N>s" lines.
		echo "ok   ${labels[k]} $(awk '$1 == "ok" { t = $3 + 0; if (t > max) max = t } END { printf "%.0fs", max }' "$logs/${labels[k]}.log")"
	else
		failed=1
		echo "FAIL ${labels[k]}"
		sed 's/^/    /' "$logs/${labels[k]}.log"
	fi
done
exit "$failed"
