#!/usr/bin/env bash

# release helper: tags each module and bumps its dependents' go.mod requires, in
# dependency order. each module is tagged before the next one is tidied, because
# steep and cmd/reel require the reel tag that was just published.
set -euo pipefail

# go mod tidy and go build must follow each go.mod, not go.work
export GOWORK=off

# tidy runs immediately after a tag push, before the module proxy has the tag
export GOPRIVATE="github.com/lrstanley/steep"

REEL_MODULE="github.com/lrstanley/steep/reel"

if ! command -v svu >/dev/null 2>&1; then
	echo "svu is not installed"
	exit 1
fi

if [ "$(git rev-parse --show-toplevel)" != "$PWD" ]; then
	echo "must be run from the root of the repository"
	exit 1
fi

CURRENT="$(svu current --tag.pattern "v*")"
NEXT="${1:-}"
if [ -z "$NEXT" ]; then
	NEXT="$(svu patch --tag.pattern "v*")"
fi

if [ "$CURRENT" = "$NEXT" ]; then
	echo "${CURRENT} is already the latest version"
fi

echo "${CURRENT} -> ${NEXT}"
echo "press enter to continue"
read -r

tag_and_push() {
	local tag_name="$1"

	if ! git tag -l | grep -q "^${tag_name}$"; then
		echo "tagging ${tag_name}"
		git tag -m "${tag_name}" "${tag_name}"
	fi

	if ! git ls-remote --tags origin | grep -q "/${tag_name}$"; then
		echo "pushing ${tag_name}"
		git push origin "${tag_name}"
	fi
}

# bump_reel requires the reel just tagged from dir, and checks that dir still
# builds against it, as a consumer would without a workspace.
bump_reel() {
	local dir="$1"

	echo "bump ${REEL_MODULE} in ${dir}/go.mod"
	pushd "$dir" >/dev/null
	go mod edit -require="${REEL_MODULE}@${NEXT}"
	go mod tidy
	go build ./...
	popd >/dev/null

	git add "${dir}/go.mod" "${dir}/go.sum"
	if git diff --cached --quiet; then
		echo "no go.mod changes for ${dir}"
		return 0
	fi

	echo "committing ${dir}"
	git commit -S -s -m "chore(release): bump reel in ${dir} to ${NEXT}"
	git push origin HEAD
}

(cd reel && go build ./...)
tag_and_push "reel/${NEXT}"

bump_reel .
tag_and_push "$NEXT"

bump_reel cmd/reel
tag_and_push "cmd/reel/${NEXT}"
