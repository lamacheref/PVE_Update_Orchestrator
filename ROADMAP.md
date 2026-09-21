# 🗺️ ROADMAP — PVE Update Orchestrator

> Jalons + versions. `M` = promotion manuelle 🔴, `m`/`f` = bumps auto CI 🤖.
> Suivi fin des tâches : [TODO.md](TODO.md). Spec : [PROJET.md](PROJET.md).

| 📦 Jalon | 🎯 Contenu | 🏷️ Version cible | ✅ Critère de sortie |
|---|---|---|---|
| 🌱 **Lot 0 — Socle** | Config, `bootstrap-ssh`, `inventory sync` 7 nodes, CI verte | `v0.2.0` (features) | `inventory sync` réel OK, `build/vet/test` verts |
| 🚀 **Lot 1 — MVP** | Runner, health, update node/guest, Discord, `serve` lecture seule | `v0.3.0` | Canary Janus + 1 guest `--dry-run` puis réel 🐤 |
| 💪 **Lot 2 — Durcissement** | Reboot/resume, kernel-clean natif, backup/rollback PBS, systemd | `v0.4.0`+ | Rollback réel sur VM poubelle 🗑️, run ~30 min ☕ |
| 🎨 **Lot 3 — Docker + UI** | Module docker, actions `retry/rollback`, runbook | `v0.5.0`+ | Revue d'un run Discord complet |

> ⚠️ `v0.1.0` consommée par le bump auto du socle : jalons décalés en conséquence.

## 🔴 Promotion v1.0.0 — geste utilisateur uniquement

Quand le run hebdo est stable en conditions réelles :

```bash
CONFIRM_MAJOR=yes scripts/bump.sh major   # 0.x.y → 1.0.0
git add VERSION && git commit -m "chore(release): v1.0.0"
git tag v1.0.0 && git push all main --tags
```

La CI ne fera **jamais** ce bump : `scripts/bump.sh` refuse `major` sans `CONFIRM_MAJOR=yes`, et le job `version` n'appelle que le mode `auto` (→ `m` ou `f`).

## 🔮 Après v1.0.0 (idées, non engagé)

- 📊 Export Prometheus/Grafana + Loki
- 🔁 SSE temps réel sur le dashboard (V2)
- 🪸 Politiques Ceph/HA avancées, fenêtres de reboot par tag
