#!/usr/bin/env bash
# Checks everything a change can break, printing only what fails:
# gofmt, go vet (Linux and Windows), go test, and the phone app's Kotlin compile.
#
#   scripts/check.sh          all checks
#   scripts/check.sh go       only the Go ones
#   scripts/check.sh app      only the Kotlin compile
#
# A macOS vet isn't run: Wails needs cgo for darwin, which can't cross-compile here.
set -uo pipefail
cd "$(dirname "$0")/.."

what=${1:-all}
failed=0

step() { # step NAME COMMAND...: runs COMMAND quietly, shows its output only on failure
	local name=$1 out
	shift
	if out=$("$@" 2>&1); then
		echo "ok    $name"
	else
		echo "FAIL  $name"
		echo "$out" | sed 's/^/      /'
		failed=1
	fi
}

gofmt_clean() {
	local files
	files=$(gofmt -l cmd/hearsay)
	[ -z "$files" ] || { echo "not gofmt'ed: $files"; return 1; }
}

if [ "$what" = all ] || [ "$what" = go ]; then
	step "gofmt" gofmt_clean
	step "go vet (linux)" go vet ./cmd/hearsay
	step "go vet (windows)" env GOOS=windows GOARCH=amd64 go vet ./cmd/hearsay
	step "go test" go test ./cmd/hearsay
fi
if [ "$what" = all ] || [ "$what" = app ]; then
	# Warnings (lines starting with "w:") aren't failures.
	step "kotlin compile" bash -c './gradlew -q --offline :app:compileDebugKotlin 2>&1 | grep -v "^w:"; exit ${PIPESTATUS[0]}'
fi
exit $failed
