#!/bin/bash
# Display a structure view of the .liza/state.yaml blackboard
# Usage: watch -n 2 'scripts/console.sh'

# Deprecation notice
echo ""
echo "WARNING: console.sh is deprecated and will be removed in a future release."
echo "  Use 'liza tui' for an interactive TUI dashboard with the same information plus:"
echo "  - Color-coded status indicators"
echo "  - Keyboard commands (spawn, pause, resume, add task, checkpoint)"
echo "  - Inline anomaly monitoring"
echo ""

# Display GOAL, SPRINT, SYSTEM as three columns
liza status 2>&1 | awk '
/^=== TASKS/   { exit }
/^=== GOAL/    { sec=1; next }
/^=== SPRINT/  { sec=2; next }
/^=== SYSTEM/  { sec=3; next }
/^===/         { next }
sec && /^$/ && !got[sec] { next }  # skip leading blank lines
sec { got[sec]=1; lines[sec]=lines[sec] (lines[sec]?"\n":"") $0 }
END {
  # Split each section into arrays, trim trailing blank lines
  n1=split(lines[1], a1, "\n"); while(n1>0 && a1[n1]=="") n1--
  n2=split(lines[2], a2, "\n"); while(n2>0 && a2[n2]=="") n2--
  n3=split(lines[3], a3, "\n"); while(n3>0 && a3[n3]=="") n3--
  max=n1; if(n2>max) max=n2; if(n3>max) max=n3

  w1=38; w2=34; w3=20
  printf "%-*s \xe2\x94\x82 %-*s \xe2\x94\x82 %s\n", w1,"GOAL", w2,"SPRINT", "SYSTEM"
  for(i=1;i<=max;i++) {
    c1=(i<=n1?a1[i]:""); c2=(i<=n2?a2[i]:""); c3=(i<=n3?a3[i]:"")
    # Truncate long values to fit columns
    if(length(c1)>w1) c1=substr(c1,1,w1-3)"..."
    if(length(c2)>w2) c2=substr(c2,1,w2-3)"..."
    printf "%-*s \xe2\x94\x82 %-*s \xe2\x94\x82 %s\n", w1,c1, w2,c2, c3
  }
}'
echo ; echo ====== Tasks ======
liza get tasks --format table
echo ; echo ====== Agents ======
liza get agents --format table
echo ; echo ====== Metrics ======
liza get metrics 2>&1 | awk '
{ a[NR]=$0 }
END {
  w1=38; w2=34
  max=4; if(5>max) max=5
  for(i=1;i<=max;i++) {
    c1=(i<=4?a[i]:""); c2=(i>=1&&i<=5?a[i+4]:""); c3=a[i+9]
    if(!c3) c3=""
    if(c1=="" && c2=="" && c3=="") continue
    if(length(c1)>w1) c1=substr(c1,1,w1-3)"..."
    if(length(c2)>w2) c2=substr(c2,1,w2-3)"..."
    printf "%-*s \xe2\x94\x82 %-*s \xe2\x94\x82 %s\n", w1,c1, w2,c2, c3
  }
}'
echo ; echo ====== Anomalies ======
liza get anomalies
echo ; echo ====== Logs ======
yq '.[] | [.timestamp, .agent, .action, (.task // "-"), (.detail // "-")] | @tsv' .liza/log.yaml |
  awk -F'\t' '{ts=$1; sub(/T/," ",ts); sub(/\.[0-9]+Z$/,"",ts); sub(/Z$/,"",ts); d=substr($5,1,80); printf "%s | %-15s | %-20s | %-25s | %s\n", ts, $2, $3, $4, d}'
