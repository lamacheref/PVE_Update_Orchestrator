# 🧾 CHANGELOG — PVE Update Orchestrator

> Format [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/). Versions `M.m.f` ([VERSION](VERSION)), tags `vM.m.f`.
> Les entrées de release sont générées par la CI (`chore(release)`), complétées à la main si besoin.

## [Unreleased]
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
