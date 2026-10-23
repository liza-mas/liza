#!/bin/sh
set -eu
# The literal is synthetic input belonging to this committed fixture.
test "${ORACLE_FIXTURE:-absent}" = oracle-reusable-fixture-8472
printf 'fixture=%s\n' "$ORACLE_FIXTURE"
printf 'oracle-live-marker\n'
exit 7
