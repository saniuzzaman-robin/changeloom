#!/usr/bin/env bash
# Proposes the next app version from the conventional commits since the last v* tag, then (after you confirm)
# tags main with it and pushes the tag, which starts the prod Android release (.github/workflows/android-release.yml).
# feat -> minor, fix/perf -> patch, `type!:` or "BREAKING CHANGE" -> major; anything else alone -> patch.
# Usage: deploy/release.sh [--version x.y.z] [--dry-run]   (or: make release [VERSION=x.y.z] [DRY_RUN=1])
set -euo pipefail

override=""
dry_run=false
while (($# > 0)); do
	case "$1" in
	--version)
		override="${2:-}"
		shift 2
		;;
	--dry-run)
		dry_run=true
		shift
		;;
	*)
		echo "usage: $0 [--version x.y.z] [--dry-run]" >&2
		exit 2
		;;
	esac
done

semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
if [[ -n $override && ! $override =~ $semver ]]; then
	echo "--version must look like 1.2.3, got $override" >&2
	exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

# The tag goes on the pushed main, so the release builds exactly what is on GitHub. A dry run only warns.
git fetch --quiet --tags origin main
problems=()
branch="$(git rev-parse --abbrev-ref HEAD)"
[[ $branch == main ]] || problems+=("releases are tagged from main; you are on $branch")
[[ -z $(git status --porcelain --untracked-files=no) ]] || problems+=("commit or stash your changes first: the tag would not include them")
[[ $(git rev-parse HEAD) == $(git rev-parse origin/main) ]] ||
	problems+=("main differs from origin/main: push or pull first, so the tag points at what GitHub builds")
for problem in "${problems[@]}"; do
	if $dry_run; then echo "warning: $problem" >&2; else echo "$problem" >&2; fi
done
if ((${#problems[@]} > 0)) && ! $dry_run; then
	exit 1
fi

tags="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname)"
last="${tags%%$'\n'*}"
if [[ -n $last ]]; then
	range="$last..HEAD"
	current="${last#v}"
else
	range="HEAD"
	current="0.0.0"
fi
if [[ $(git rev-list --count "$range") == 0 ]]; then
	echo "nothing to release: no commits since $last" >&2
	exit 1
fi

IFS=. read -r major minor patch <<<"$current"
bump=patch
subjects="$(git log --format='%s' "$range")"
if git log --format='%B' "$range" | grep -qE '^BREAKING[ -]CHANGE:' || grep -qE '^[a-z]+(\([^)]*\))?!:' <<<"$subjects"; then
	bump=major
elif grep -qE '^feat(\([^)]*\))?:' <<<"$subjects"; then
	bump=minor
fi
case "$bump" in
major) proposed="$((major + 1)).0.0" ;;
minor) proposed="$major.$((minor + 1)).0" ;;
patch) proposed="$major.$minor.$((patch + 1))" ;;
esac
version="${override:-$proposed}"
if [[ -n $tags ]] && grep -qx "v$version" <<<"$tags"; then
	echo "v$version already exists" >&2
	exit 1
fi
highest="$(printf '%s\n%s\n' "$current" "$version" | sort -V | tail -n 1)"
if [[ $version == "$current" || $highest != "$version" ]]; then
	echo "v$version is not above the last release (${last:-v0.0.0})" >&2
	exit 1
fi

echo "Last release: ${last:-none}"
echo "Commits in this release:"
git log --format='  %h %s' "$range"
echo
echo "Proposed: v$proposed ($bump, from conventional commits)"
[[ -n $override ]] && echo "Using:    v$version (--version)"
if $dry_run; then
	echo "Dry run: nothing tagged."
	exit 0
fi

read -r -p "Tag $(git rev-parse --short HEAD) as v$version and push the tag? [y/N] " answer
if [[ $answer != y && $answer != Y ]]; then
	echo "Cancelled: nothing tagged."
	exit 1
fi
git tag -a "v$version" -m "Changeloom $version"
git push origin "v$version"
echo "Pushed v$version. The prod Android release waits for your approval in GitHub Actions (production environment)."
