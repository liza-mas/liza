#!/bin/sh
# PAIRING-INDEX-SCRIPT: managed
set -eu
repo_root='<REPO>'
run_ai=0
if [ "${1:-}" = "ai" ]; then
	run_ai=1
	shift
fi
staging_root="$(cd "$repo_root" && cd "$(git rev-parse --git-common-dir)" && pwd -P)/liza-index-staging"
mkdir -p "$staging_root"
staging_dir="$(mktemp -d "$staging_root/run.XXXXXX")"
cleanup_staging() { rm -rf "$staging_dir"; }
trap cleanup_staging EXIT
trap 'exit 1' HUP INT TERM
needs_go_scip=0
if [ ! -f '<REPO>/go.scip' ]; then
	needs_go_scip=1
elif find '<REPO>' -type f -name '*.go' -not -path '<REPO>/.git/*' -not -path '<REPO>/.worktrees/*' -newer '<REPO>/go.scip' -print -quit | grep -q .; then
	needs_go_scip=1
fi
missing_go_scip=0
if [ "$needs_go_scip" -eq 1 ] && ! command -v scip-search >/dev/null 2>&1; then
	echo "liza-index.sh: scip-search not found; skipping go SCIP refresh" >&2
	missing_go_scip=1
fi
if [ "$needs_go_scip" -eq 1 ] && ! command -v scip-go >/dev/null 2>&1; then
	echo "liza-index.sh: scip-go not found; skipping go SCIP refresh" >&2
	missing_go_scip=1
fi
if [ "$needs_go_scip" -eq 1 ] && [ "$missing_go_scip" -eq 0 ]; then
	tmp_go_scip="$staging_dir/go-scip"
	mkdir "$tmp_go_scip"
	cd '<REPO>'
		aggregate_go_scip() {
			set -- scip-search aggregate-index --project-root <REPO>
			indexed_go_roots=0
			if scip-go index --module-root <REPO> --output "$tmp_go_scip/go-0.scip" 3>&-; then
				set -- "$@" --root . --index "$tmp_go_scip/go-0.scip"
				indexed_go_roots=$((indexed_go_roots + 1))
			else
				echo "liza-index.sh: failed to index go SCIP root .; skipping it" >&2
			fi
			if [ "$indexed_go_roots" -gt 0 ]; then
				"$@" --out "$staging_dir/go.scip" 3>&-
				mv -f "$staging_dir/go.scip" <REPO>/go.scip
			elif [ -f '<REPO>/go.scip' ]; then
				echo "liza-index.sh: no go SCIP roots indexed; retaining existing index" >&2
			else
				echo "liza-index.sh: no go SCIP roots indexed; no index produced" >&2
			fi
		}
		aggregate_go_scip
fi
if ! command -v stacklit >/dev/null 2>&1; then
	echo "liza-index.sh: stacklit not found; skipping Stacklit refresh" >&2
else
	cd "$repo_root"
	code=0
	stacklit_refresh=1
	if [ -f stacklit.json ]; then
		stacklit diff -i stacklit.json >/dev/null 3>&- || code=$?
		case "$code" in
			0) [ "$run_ai" = "1" ] || stacklit_refresh=0 ;;
			1) ;;
			*) echo "stacklit diff failed"; exit "$code" ;;
		esac
	fi
	if [ "$stacklit_refresh" = "1" ]; then
		echo "Stacklit Indexing..."
		insights_before=""
		if [ -f stacklit-insights.json ]; then
			insights_before="$(cksum stacklit-insights.json | awk '{print $1 ":" $2}')"
		fi
		stacklit generate-json -o "$staging_dir/stacklit.json" --parse-workers 3 3>&-
		mv -f "$staging_dir/stacklit.json" stacklit.json
		if [ -f stacklit-insights.json ]; then cp stacklit-insights.json "$staging_dir/stacklit-insights.json"; fi
		stacklit init-insights -i stacklit.json -o "$staging_dir/stacklit-insights.json" 3>&-
		mv -f "$staging_dir/stacklit-insights.json" stacklit-insights.json
		if [ "$run_ai" = "1" ]; then
			echo "Adding AI summary..."
			if [ -f stacklit-insights.json ]; then cp stacklit-insights.json "$staging_dir/stacklit-insights.json"; fi
			stacklit ai-summary -o "$staging_dir/stacklit-insights.json" 3>&-
			mv -f "$staging_dir/stacklit-insights.json" stacklit-insights.json
		fi
		insights_after=""
		if [ -f stacklit-insights.json ]; then
			insights_after="$(cksum stacklit-insights.json | awk '{print $1 ":" $2}')"
		fi
		if [ "$insights_before" != "$insights_after" ]; then
			stacklit generate-json -o "$staging_dir/stacklit.json" --parse-workers 3 3>&-
			mv -f "$staging_dir/stacklit.json" stacklit.json
		fi
		echo "Wrote stacklit.json"
	fi
fi
cd '<REPO>'
needs_functional_clusters=0
if [ ! -f 'functional-clusters.json' ]; then
	needs_functional_clusters=1
elif [ -f 'stacklit.json' ] && [ 'stacklit.json' -nt 'functional-clusters.json' ]; then
	needs_functional_clusters=1
elif [ -f '<REPO>/go.scip' ] && [ '<REPO>/go.scip' -nt 'functional-clusters.json' ]; then
	needs_functional_clusters=1
fi
missing_functional_clusters=0
if [ "$needs_functional_clusters" -eq 1 ] && ! command -v functional-clusters >/dev/null 2>&1; then
	echo "liza-index.sh: functional-clusters not found; skipping Functional Clusters refresh" >&2
	missing_functional_clusters=1
fi
if [ "$needs_functional_clusters" -eq 1 ] && ! command -v stacklit >/dev/null 2>&1; then
	echo "liza-index.sh: stacklit not found; skipping Functional Clusters refresh" >&2
	missing_functional_clusters=1
fi
if [ "$needs_functional_clusters" -eq 1 ] && ! command -v scip-search >/dev/null 2>&1; then
	echo "liza-index.sh: scip-search not found; skipping Functional Clusters refresh" >&2
	missing_functional_clusters=1
fi
if [ "$needs_functional_clusters" -eq 1 ] && [ ! -f 'stacklit.json' ]; then
	echo "liza-index.sh: stacklit.json missing; skipping Functional Clusters refresh" >&2
	missing_functional_clusters=1
fi
if [ "$needs_functional_clusters" -eq 1 ] && [ ! -f '<REPO>/go.scip' ]; then
	echo "liza-index.sh: go SCIP index missing; skipping Functional Clusters refresh" >&2
	missing_functional_clusters=1
fi
if [ "$needs_functional_clusters" -eq 1 ] && [ "$missing_functional_clusters" -eq 0 ]; then
	tmp_functional_clusters="$staging_dir/functional-clusters"
	mkdir "$tmp_functional_clusters"
	echo "Functional Clusters Indexing..."
	stacklit export-architecture -i 'stacklit.json' -o "$tmp_functional_clusters/stacklit-architecture.json" 3>&-
	scip-search graph-export --index '<REPO>/go.scip' -o "$tmp_functional_clusters/go-scip-graph.json" 3>&-
	functional-clusters build --scip-graph "$tmp_functional_clusters/go-scip-graph.json" --stacklit-architecture "$tmp_functional_clusters/stacklit-architecture.json" -o "$staging_dir/functional-clusters.json" 3>&-
	mv -f "$staging_dir/functional-clusters.json" 'functional-clusters.json'
	echo "Wrote functional-clusters.json"
fi
