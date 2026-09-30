#!/usr/bin/env bash
# Print the next semver tag for HEAD.
# GNACT_TAG=1 also creates the annotated tag and pushes it to origin.
set -euo pipefail

exact=$(git tag --points-at HEAD | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { print; exit }')
if [[ -n "${exact}" ]]; then
	printf '%s\n' "$exact"
	exit 0
fi

latest=$(
	git tag --list |
		awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ {
			split(substr($0, 2), p, ".")
			printf "%d %d %d %s\n", p[1], p[2], p[3], $0
		}' |
		sort -n -k1,1 -k2,2 -k3,3 |
		awk 'END { print $4 }'
)

if [[ -n "${latest}" ]]; then
	subjects=$(git log --format='%s%n%b' "${latest}..HEAD")
else
	subjects=$(git log --format='%s%n%b')
fi

level=patch
if printf '%s\n' "${subjects}" | grep -Eq '^BREAKING CHANGE|^[a-zA-Z]+(\([^)]+\))?!:'; then
	level=major
elif printf '%s\n' "${subjects}" | grep -Eq '^feat(\([^)]+\))?:'; then
	level=minor
fi

if [[ -z "${latest}" ]]; then
	next=v0.1.0
else
	IFS=. read -r major minor patch <<<"${latest#v}"
	case "${level}" in
	major)
		if ((major >= 1)); then
			printf 'refusing automatic v2: the module path would have to change\n' >&2
			exit 1
		fi
		minor=$((minor + 1))
		patch=0
		;;
	minor)
		minor=$((minor + 1))
		patch=0
		;;
	*)
		patch=$((patch + 1))
		;;
	esac
	next="v${major}.${minor}.${patch}"
fi

if [[ "${GNACT_TAG:-}" == "1" ]]; then
	git tag -a "${next}" -m "release ${next}"
	git push origin "refs/tags/${next}"
fi
printf '%s\n' "${next}"
