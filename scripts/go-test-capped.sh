#!/usr/bin/env bash
# Run `go test` under the shared memory cap (scripts/run-capped.sh), with the
# strict policy: on WSL, an unavailable cap is a refusal rather than a silently
# uncapped run. The go lane is the one with the runaway history — twice a bare
# `go test` has torn down the whole WSL VM (nibs-mv0i, nibs-mlss) — so it is the
# lane that would rather fail than run unprotected.
#
# All arguments are forwarded to `go test`, e.g.
#   scripts/go-test-capped.sh ./...
#   scripts/go-test-capped.sh -run TestFoo ./pkg
#
# The ceiling and the opt-out are run-capped.sh's: NIBS_CAP_MEM_MAX (default 4G)
# and NIBS_UNCAPPED=1. Measured headroom, for whoever is tempted to raise it: the
# -race lane peaks at ~222 MB, 5% of the default (nibs-0kip).
set -euo pipefail

# Resolve the sibling script with bash's own parameter expansion rather than
# dirname, for the same reason run-capped.sh reads osrelease without grep: this
# must not break on a machine missing a coreutil. An unqualified $0 (invoked as
# `bash go-test-capped.sh` from scripts/) leaves the name untouched, which means
# the directory is the current one.
here=${BASH_SOURCE[0]%/*}
if [[ $here == "${BASH_SOURCE[0]}" ]]; then
	here=.
fi

# --host-os is run-capped.sh's, not this script's: peel it off the front and hand
# it straight back, so a caller passes it once and does not have to know which of
# the two scripts consumes it.
hostos=()
if [[ ${1:-} == --host-os ]]; then
	hostos=(--host-os "${2:-}")
	shift 2
fi

exec bash "$here/run-capped.sh" ${hostos[@]+"${hostos[@]}"} --refuse-on-wsl go test "$@"
