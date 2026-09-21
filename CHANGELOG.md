# 🧾 CHANGELOG — PVE Update Orchestrator

> Format [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/). Versions `M.m.f` ([VERSION](VERSION)), tags `vM.m.f`.
> Les entrées de release sont générées par la CI (`chore(release)`), complétées à la main si besoin.

## [Unreleased]
## [0.5.0] - 2026-09-21

- merge: bump CI (f255a50)
- feat: anti-double-backup + PBS verifie + timeouts longs/illimites (ff90f53)

## [0.4.7] - 2026-09-21

- fix: topgrade --allow-root adaptatif (v9 vs v17+) (deef809)

## [0.4.6] - 2026-09-21

- merge: bump CI (a592399)
- fix: kernels proxmox-kernel-* PVE9 + grep sans match non fatal (9cfdd22)

## [0.4.5] - 2026-09-21

- merge: bump CI (2898276)
- docs: assainissement flotte checkmk + datacenter UTF-8 + nas automount (81e1235)

## [0.4.4] - 2026-09-21

- merge: bump CI (ba289cd)
- fix: grep -c exit 1 + defaut stockage PBS reel (pbs) (2d47628)

## [0.4.3] - 2026-09-21

- merge: bump CI (60e0e46)
- fix: reboot si kernel installe non boote + recapture uname post-reboot (4ed7d46)

## [0.4.2] - 2026-09-21

- fix: timeouts longs non bloquants + progression live + topgrade non-interactif (6b9b657)

## [0.4.1] - 2026-09-21

- merge: bump CI (915af12)
- docs: dry-run 7 nodes 29 OK valide (61b2073)

## [0.4.0] - 2026-09-21

- merge: bump CI (2fa5c03)
- feat: Lot 1 runner + health + update + discord + serve dashboard (0da433a)

## [0.3.0] - 2026-09-21

- feat: autonomie cluster discover + reconcile/rotate/revoke (c79156f)

## [0.2.7] - 2026-09-21

- merge: bump CI (627e673)
- docs: run reel Janus OK + webhook rotation refusee (risque assume) (f9de5d0)

## [0.2.6] - 2026-09-21

- docs: releases verifiees github+gitea (8e3222b)

## [0.2.5] - 2026-09-21

- fix: CI chaque plateforme publie chez elle (URL generee, plus de cross-push) (a34a795)

## [0.2.4] - 2026-09-21

- merge: bump CI (bb0c55b)
- fix: CI env REGISTRY_* uniquement + URL externe generee (6938a9d)

## [0.2.3] - 2026-09-21

- fix: CI echec explicite si secrets vides + diag reponse Gitea (8b7fe6b)

## [0.2.2] - 2026-09-21

- fix: CI sans artifact actions (GHES) : build+publish dans un seul job (28d8bc2)

## [0.2.1] - 2026-09-21

- fix: CI gitea via REGISTRY_USER/TOKEN + URL derivee + SSH-only acte (1006840)

## [0.2.0] - 2026-09-21

- feat: Lot 0 config + pool SSH + inventory sync + bootstrap-ssh (be532d5)

## [0.1.1] - 2026-09-21

- fix: CI release seulement sur github (garde anti-double chore) (e9aa4ad)
- fix: mermaid §2 sans emojis (getAttribute renderer) (d40d556)

## [0.1.0] - 2026-09-21

- feat: docs README/TODO/CHANGELOG/ROADMAP + versionning M.m.f + CI github-vers-gitea (57df2bb)
- docs: PROJET.md logiciel Go + fix mermaid + remotes gitea/github synchro (6f9a6fe)


## [0.0.1] - 2026-09-21

### Ajouté
- Socle Go compilable (`main.go`, `internal/version` + tests)
- Versionning M.m.f : `VERSION`, `scripts/bump.sh` (M manuel confirmé, m/f auto)
- CI GitHub : bump auto, build linux amd64/arm64, release + artefacts vers Gitea
- Docs : README, TODO, CHANGELOG, ROADMAP, PROJET.md (spec)
