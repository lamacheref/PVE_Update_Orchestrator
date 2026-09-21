#!/usr/bin/env bash
# Bump du fichier VERSION (format M.m.f).
#
#   scripts/bump.sh major   # 🔴 UTILISATEUR UNIQUEMENT (CONFIRM_MAJOR=yes requis, jamais en CI)
#   scripts/bump.sh minor   # 🟡 ajout de fonctionnalité
#   scripts/bump.sh fix     # 🟢 tout le reste (défaut)
#   scripts/bump.sh auto    # 🤖 CI : minor si commits "feat:" depuis le dernier tag, sinon fix
#
# Affiche la nouvelle version sur stdout. Ne committe ni ne tagge.
set -euo pipefail

VERSION_FILE="${VERSION_FILE:-VERSION}"

usage() {
	echo "Usage: $0 [major|minor|fix|auto]" >&2
	exit 2
}

cmd="${1:-auto}"
case "$cmd" in
	major | minor | fix | auto) ;;
	*) usage ;;
esac

if [ ! -f "$VERSION_FILE" ]; then
	echo "erreur : $VERSION_FILE introuvable" >&2
	exit 1
fi

current="$(tr -d '[:space:]' <"$VERSION_FILE")"
if ! [[ "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "erreur : $VERSION_FILE contient \"$current\", format M.m.f attendu" >&2
	exit 1
fi

IFS=. read -r M m f <<<"$current"

if [ "$cmd" = "auto" ]; then
	base="$(git describe --tags --abbrev=0 2>/dev/null || true)"
	if [ -z "$base" ]; then
		range="HEAD"
	else
		range="$base..HEAD"
	fi
	if git log --format=%s "$range" 2>/dev/null | grep -qiE '^feat(\(|:)'; then
		cmd="minor"
	else
		cmd="fix"
	fi
fi

case "$cmd" in
	major)
		if [ "${CONFIRM_MAJOR:-}" != "yes" ]; then
			echo "erreur : bump major réservé à l'utilisateur." >&2
			echo "Relancez avec CONFIRM_MAJOR=yes scripts/bump.sh major" >&2
			exit 1
		fi
		M=$((M + 1))
		m=0
		f=0
		;;
	minor)
		m=$((m + 1))
		f=0
		;;
	fix)
		f=$((f + 1))
		;;
esac

new="$M.$m.$f"
printf '%s\n' "$new" >"$VERSION_FILE"
printf '%s\n' "$new"
