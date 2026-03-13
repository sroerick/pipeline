#!/bin/ksh

set -eu

PATH="/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin"

BRANCH="${BRANCH:-prod}"
REF="refs/heads/${BRANCH}"
BARE_REPO="${BARE_REPO:-$(pwd)}"
WORKTREE="${WORKTREE:-/srv/builds/current}"
PAKKUN_BIN="${PAKKUN_BIN:-/usr/local/bin/pakkun}"
CI_PIPELINE="${CI_PIPELINE:-ci}"
RELEASE_PIPELINE="${RELEASE_PIPELINE:-release}"
RELEASE_ARTIFACT="${RELEASE_ARTIFACT:-build/release.tar.gz}"
PROMOTE_CMD="${PROMOTE_CMD:-}"

zero_rev="0000000000000000000000000000000000000000"
lockdir="${WORKTREE}.deploy.lock"
target_rev=""

log() {
  print -ru2 -- "$*"
}

die() {
  log "deploy failed: $*"
  exit 1
}

while read -r oldrev newrev refname; do
  [ "${refname}" = "${REF}" ] || continue
  [ "${newrev}" = "${zero_rev}" ] && exit 0
  target_rev="${newrev}"
done

[ -n "${target_rev}" ] || exit 0

mkdir -p "$(dirname "${WORKTREE}")"
mkdir "${lockdir}" 2>/dev/null || die "another deploy is already running for ${WORKTREE}"
trap 'rmdir "${lockdir}"' EXIT HUP INT TERM

if [ ! -d "${WORKTREE}/.git" ]; then
  git clone --branch "${BRANCH}" "${BARE_REPO}" "${WORKTREE}"
fi

git -C "${WORKTREE}" fetch "${BARE_REPO}" "${BRANCH}"
if git -C "${WORKTREE}" show-ref --verify --quiet "refs/heads/${BRANCH}"; then
  git -C "${WORKTREE}" checkout -f "${BRANCH}"
else
  git -C "${WORKTREE}" checkout -B "${BRANCH}" "${target_rev}"
fi
git -C "${WORKTREE}" reset --hard "${target_rev}"
git -C "${WORKTREE}" clean -fdx -e .pipe

if [ ! -f "${WORKTREE}/.pipe/config.yaml" ]; then
  (
    cd "${WORKTREE}"
    "${PAKKUN_BIN}" init
  )
fi

(
  cd "${WORKTREE}"
  "${PAKKUN_BIN}" run "${CI_PIPELINE}"
  "${PAKKUN_BIN}" run "${RELEASE_PIPELINE}"
)

artifact_path="${WORKTREE}/${RELEASE_ARTIFACT}"
[ -f "${artifact_path}" ] || die "missing release artifact at ${artifact_path}"

if [ -n "${PROMOTE_CMD}" ]; then
  "${PROMOTE_CMD}" "${artifact_path}" "${target_rev}"
fi

log "deploy complete for ${REF} at ${target_rev}"
