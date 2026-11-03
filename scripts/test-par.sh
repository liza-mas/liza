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

split_pkgs=()
for entry in "${SPLIT[@]}"; do
	pkg=${entry%=*}
	n=${entry#*=}
	split_pkgs+=("$(go list "$pkg")")
	# -list prints top-level Test/Example/Fuzz names; anything else is noise.
	mapfile -t names < <(go test -list . "$pkg" | grep -E '^(Test|Example|Fuzz)[[:alnum:]_]*$' || true)
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

mapfile -t rest < <(go list ./... | grep -vxF -f <(printf '%s\n' "${split_pkgs[@]}"))
launch rest "$@" "${rest[@]}"

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
