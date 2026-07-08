# stress/ — démo de charge podinfo

Petite boîte à outils pour stresser le cluster en live (autoscaling, résilience, charge).

> ⚠️ **Pas de risque pour la CB.** Les nodes sont **statiques** (aucun autoscaler de nodes).
> Le seul scaling ici = le **HPA sur les pods podinfo**, borné à **20 replicas** (`apps/podinfo.yaml`).
> Une fois les nodes pleins, les pods en trop passent juste en `Pending`.

## Lancer

```bash
./demo.sh <commande>
```

`k6` est récupéré automatiquement via `nix` (rien à installer). La cible (IP ingress)
est résolue depuis Terraform puis l'inventaire Ansible. Sinon : `export TARGET_IP=1.2.3.4`.

## Les commandes

| Commande | Ce qu'elle fait |
|---|---|
| `smoke` | Vérifie juste que podinfo répond (affiche version + hostname du pod). |
| `watch` | Boucle `curl` : montre **quels pods** servent le trafic (preuve visuelle du scale). |
| `autoscale` | Montée en charge **progressive** sur `/api/info` → CPU ↑ → le **HPA scale 2 → N pods**. |
| `spike` | **Pic brutal** (5 → ~100 users en 5s) → latence qui grimpe puis HPA qui rattrape. |
| `latency` | Sature via `/delay/1` (podinfo dort 1s) → montre la saturation par la latence. |
| `chaos` | Envoie des `500`/`503` → fait monter le taux d'erreur (logs Loki). |
| `survivor` | **Chaos Monkey** : tue des pods (`/panic`) en boucle **en mesurant la dispo en direct**. |

## Régler l'intensité (variables d'env)

```bash
VUS=60 ./demo.sh autoscale         # + d'utilisateurs virtuels
VUS=150 ./demo.sh spike            # pic plus violent
DELAY=2 ./demo.sh latency          # podinfo dort 2s/requête
CHAOS_SECONDS=60 ./demo.sh chaos
KILL_EVERY=10 DURATION=90 ./demo.sh survivor   # tue 1 pod / 10s pendant 90s
```

## Déroulé conseillé (3 terminaux)

1. **k9s** sur le namespace `podinfo` (+ vue `:hpa`) — les pods/RESTARTS en direct.
2. `./demo.sh watch` — la charge qui se répartit.
3. `./demo.sh autoscale` (puis `spike`, `survivor`…) — on lance la charge.

**Astuce survivor** : lance `./demo.sh spike` d'abord (→ ~6 pods) puis `KILL_EVERY=10 ./demo.sh survivor`
pour le beau résultat (l'appli encaisse). Baisse `KILL_EVERY` pour déclencher volontairement
le **CrashLoopBackOff** (bon moment pédagogique).

## Où regarder dans Grafana

- **autoscale / spike** → dashboard `k8s-views-pods` (CPU + nombre de pods qui monte).
- **chaos / latency** → ⚠️ pas de métrique HTTP scrapée pour l'instant : visible seulement
  en **logs** (Explore → Loki, `{namespace="podinfo"}`). Pour de vrais graphes RPS/erreurs/p95,
  il faudrait activer le ServiceMonitor podinfo (pas encore fait).
