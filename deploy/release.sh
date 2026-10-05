#!/usr/bin/env bash
# Proposes the next app version from the conventional commits since the last v* tag, then (after you confirm)
# tags main with it and pushes the tag, which starts the prod Android release (.github/workflows/android-release.yml).
# feat -> minor, fix/perf -> patch, `type!:` or "BREAKING CHANGE" -> major; anything else alone -> patch.
# --pre alpha tags a closed-testing build (v1.2.3-alpha.4, numbered per version); without it, the last
# pre-release's version is released as is (v1.2.3), even from the same commit.
# Usage: deploy/release.sh [--version x.y.z] [--pre alpha] [--dry-run]
#   (or: make release [VERSION=x.y.z] [PRE=alpha] [DRY_RUN=1])
set -euo pipefail

override=""
pre=""
dry_run=false
while (($# > 0)); do
	case "$1" in
	--version)
		override="${2:-}"
		shift 2
		;;
	--pre)
		pre="${2:-}"
		shift 2
		;;
	--dry-run)
		dry_run=true
		shift
		;;
	*)
		echo "usage: $0 [--version x.y.z] [--pre alpha] [--dry-run]" >&2
		exit 2
		;;
	esac
done

semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
if [[ -n $override && ! $override =~ $semver ]]; then
	echo "--version must look like 1.2.3, got $override" >&2
	exit 2
fi
if [[ -n $pre && $pre != alpha ]]; then
	echo "--pre must be alpha (Play's closed testing track), got $pre" >&2
	exit 2
fi

# Succeeds when version $1 is above $2. Versions are x.y.z or x.y.z-alpha.n; a pre-release is below its release.
is_above() {
	local a b i
	IFS=. read -r -a a <<<"${1%%-*}"
	IFS=. read -r -a b <<<"${2%%-*}"
	for i in 0 1 2; do
		if ((a[i] != b[i])); then
			((a[i] > b[i]))
			return
		fi
	done
	if [[ $1 == "$2" || $1 == *-* && $2 != *-* ]]; then
		return 1
	fi
	if [[ $2 == *-* && $1 != *-* ]]; then
		return 0
	fi
	((${1##*.} > ${2##*.}))
}

# The next alpha number of version $1 (x.y.z), from $tags.
next_alpha() {
	local n
	n="$(grep -F -- "v$1-alpha." <<<"$tags" | head -n 1 || true)"
	n="${n##*.}"
	echo "$((${n:-0} + 1))"
}

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
for problem in ${problems[@]+"${problems[@]}"}; do
	if $dry_run; then echo "warning: $problem" >&2; else echo "$problem" >&2; fi
done
if ((${#problems[@]} > 0)) && ! $dry_run; then
	exit 1
fi

# Newest first; versionsort.suffix puts v1.2.3-alpha.4 below v1.2.3.
tags="$(git -c versionsort.suffix=- tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname |
	grep -E '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-alpha\.[1-9][0-9]*)?$' || true)"
last="${tags%%$'\n'*}"
last_release="$(grep -v -- - <<<"$tags" | head -n 1 || true)"
range="${last:+$last..}HEAD"
if [[ $(git rev-list --count "$range") == 0 && ! ($last == *-* && -z $pre) ]]; then
	echo "nothing to release: no commits since $last" >&2
	exit 1
fi

# The bump counts every commit since the last release, alphas included.
current="${last_release#v}"
current="${current:-0.0.0}"
IFS=. read -r major minor patch <<<"$current"
bump=patch
bump_range="${last_release:+$last_release..}HEAD"
subjects="$(git log --format='%s' "$bump_range")"
if git log --format='%B' "$bump_range" | grep -qE '^BREAKING[ -]CHANGE:' || grep -qE '^[a-z]+(\([^)]*\))?!:' <<<"$subjects"; then
	bump=major
elif grep -qE '^feat(\([^)]*\))?:' <<<"$subjects"; then
	bump=minor
fi
case "$bump" in
major) proposed="$((major + 1)).0.0" ;;
minor) proposed="$major.$((minor + 1)).0" ;;
patch) proposed="$major.$minor.$((patch + 1))" ;;
esac
# An unreleased alpha keeps its version unless the commits since need a bigger bump.
pending="${last#v}"
pending="${pending%%-*}"
if [[ $last == *-* ]] && is_above "$pending" "$proposed"; then
	proposed="$pending"
fi
version="${override:-$proposed}"
if [[ -n $pre ]]; then
	proposed="$proposed-alpha.$(next_alpha "$proposed")"
	version="$version-alpha.$(next_alpha "$version")"
fi
if [[ -n $tags ]] && grep -qx "v$version" <<<"$tags"; then
	echo "v$version already exists" >&2
	exit 1
fi
if [[ -n $last ]] && ! is_above "$version" "${last#v}"; then
	echo "v$version is not above the last tag ($last)" >&2
	exit 1
fi
track=production
[[ -n $pre ]] && track="closed testing (alpha)"

echo "Last release: ${last_release:-none}; last tag: ${last:-none}"
echo "Commits since ${last:-the start}:"
git log --format='  %h %s' "$range"
echo
echo "Proposed: v$proposed ($bump since ${last_release:-the start}, from conventional commits)"
[[ -n $override ]] && echo "Using:    v$version (--version)"
echo "Play track: $track"
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
echo "Pushed v$version. The Android release to Play's $track track waits for your approval in GitHub Actions (production environment)."
