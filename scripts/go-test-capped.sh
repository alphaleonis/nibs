#!/usr/bin/env bash
# Run `go test` inside a memory-capped systemd --user scope so a runaway test
# (unbounded allocation) is OOM-killed inside its own cgroup instead of triggering
# a GLOBAL OOM that takes down every shell sharing init.scope (as happened on
# 2026-07-06: an uncapped nib.test billion-laughs probe hit ~12 GB RSS and killed
# the whole WSL VM). See nibs-mv0i.
#
# Falls back to a plain, UNCAPPED `go test` (with a warning) where systemd --user
# scopes aren't available (e.g. CI without a user systemd manager).
#
# Override the cap with GO_TEST_MEM_MAX (default 4G). All arguments are forwarded
# to `go test`, e.g.  scripts/go-test-capped.sh ./...  or  ... -run TestFoo ./pkg
set -euo pipefail

LIMIT="${GO_TEST_MEM_MAX:-4G}"

if systemd-run --user --scope --quiet true >/dev/null 2>&1; then
	exec systemd-run --user --scope --quiet \
		-p MemoryMax="$LIMIT" -p MemorySwapMax=0 -p MemoryAccounting=yes \
		go test "$@"
fi

echo "warning: systemd --user scope unavailable; running 'go test' UNCAPPED" >&2
exec go test "$@"
