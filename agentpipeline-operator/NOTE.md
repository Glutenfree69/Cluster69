# NOTE — Comprendre le fonctionnement de l'opérateur

> Aide-mémoire pour expliquer clairement comment fonctionne `agentpipeline-operator`,
> quelles ressources Kubernetes il utilise et pourquoi.

## En une phrase

L'opérateur **surveille** des objets `AgentPipeline` et **exécute leurs stages dans
l'ordre**, chaque stage invoquant un agent IA `kagent` via le protocole **A2A**, et la
sortie d'un stage alimente le prompt du suivant.

C'est une **boucle de réconciliation** (le pattern de base de tout opérateur K8s) : on
décrit un *état désiré* (le spec du CR), et le contrôleur travaille en continu pour faire
correspondre l'*état réel* à cet état désiré.

---

## Les ressources Kubernetes utilisées

| Ressource | Rôle dans l'opérateur | Qui la crée |
|---|---|---|
| **CRD** `AgentPipeline` (`aiops.cluster69.io/v1alpha1`) | Le type custom qui décrit un pipeline (ses stages, triggers, retries). Étend l'API Kubernetes. | `make install` (généré par kubebuilder) |
| **CR** `AgentPipeline` | Une instance concrète de pipeline. C'est l'objet **principal** que le contrôleur surveille. | L'utilisateur (`kubectl apply`) |
| **ConfigMap** | Stocke la **sortie complète** de chaque stage. Une par stage, nommée `<pipeline>-stage-<name>`. | Le contrôleur (`StageHandler.StoreOutput`) |
| **Event** | Trace lisible de ce qui se passe (`PipelineStarted`, `StageCompleted`, etc.), visible dans `describe` / k9s. | Le contrôleur (`Recorder.Event`) |
| **CR** `Agent` (`kagent.dev`) | Ressource **externe** (gérée par kagent) que chaque stage référence. L'opérateur a le droit de la lire (RBAC), pas de la modifier. | kagent |
| **ServiceAccount + Role/RoleBinding** | Identité et permissions du pod opérateur (RBAC généré depuis les markers `+kubebuilder:rbac`). | `make deploy` |

> Le sous-objet `status` du CR n'est **pas** une ressource séparée : c'est une
> *sous-ressource* du `AgentPipeline`, modifiée via `r.Status().Update()`. Le `spec`
> = désiré (écrit par l'humain), le `status` = observé (écrit par le contrôleur).

---

## 1. La ConfigMap : pourquoi ?

### Le problème
Un agent peut renvoyer une **grosse sortie** (diagnostic, patch, logs). Il faut la stocker
pour (a) la passer au stage suivant, (b) pouvoir la consulter ensuite.

### Pourquoi pas tout mettre dans le `status` du CR ?
- Un objet K8s vit dans **etcd**, limité à ~**1.5 Mo par objet**.
- Le `status` est relu/réécrit à **chaque reconcile** → s'il est gros, chaque cycle coûte cher.
- etcd est un store de **métadonnées**, pas un blob store.

### La solution : séparation pointeur / contenu
- **Contenu complet** → une ConfigMap dédiée, nom déterministe `<pipeline>-stage-<name>`.
- **Status du CR** → garde seulement :
  - `output` : les **1024 derniers** caractères (aperçu ; les *derniers* car la conclusion d'un agent est souvent à la fin).
  - `outputRef` : le **nom** de la ConfigMap (le pointeur).

### Les bénéfices
- **Natif K8s** : visible dans k9s, soumis au RBAC, versionné.
- **Idempotent** : nom déterministe + `CreateOrUpdate` → relancer un stage écrase proprement, pas de doublon.
- **Lien inter-stages** : `BuildPipelineContext` relit les ConfigMaps des stages déjà
  terminés pour reconstruire `{{.PreviousOutput}}` et `{{.StageOutput "x"}}` dans les templates de prompt.
- **Résilience / idempotence** : la ConfigMap est un *fait durable* ("cet agent a tourné,
  voici sa sortie"). Avant d'invoquer un agent, le contrôleur vérifie si la ConfigMap
  existe déjà — si oui, l'agent a déjà tourné mais l'écriture du status a échoué : on
  **récupère depuis la ConfigMap au lieu de relancer l'agent** (qui coûterait cher ou
  aurait des effets de bord, ex. ouvrir une PR GitHub).

---

## 2. Les finalizers : pourquoi ?

### Le concept
`kubectl delete` ne supprime **pas** tout de suite l'objet. K8s regarde `metadata.finalizers` :
- **Liste vide** → suppression réelle (l'objet disparaît d'etcd).
- **Liste non vide** → K8s pose seulement un `metadata.deletionTimestamp` et **bloque**
  la suppression. L'objet reste en `Terminating` tant que tous les finalizers ne sont pas retirés.

Un finalizer = un **verrou de nettoyage** : "ne supprime pas tant que je n'ai pas fait mon ménage".

### Dans cet opérateur
- **Ajout** : au premier reconcile (`handlePending`), on ajoute `aiops.cluster69.io/pipeline-cleanup`.
- **Déclenchement** : au début de `Reconcile`, si `deletionTimestamp` est posé → `handleDeletion`.
- **Libération** : `handleDeletion` retire le finalizer → K8s supprime alors réellement le CR.

### Finalizer vs OwnerReference (important)
Les ConfigMaps sont nettoyées **automatiquement** par le *garbage collector* de K8s, parce
qu'on leur pose une **OwnerReference** vers le pipeline (`SetControllerReference`). Quand le
parent meurt, les enfants meurent.

Du coup, **pourquoi garder le finalizer ?** Comme point d'ancrage pour le nettoyage que K8s
ne peut PAS faire seul — typiquement des **ressources externes au cluster** : annuler une
tâche A2A en cours, fermer une PR GitHub, libérer un lock externe. Aujourd'hui `handleDeletion`
ne fait que retirer le finalizer (placeholder), mais l'infrastructure est prête.

> **Piège** : si l'opérateur est désinstallé/cassé alors qu'il a posé un finalizer, plus
> personne ne le retire → l'objet reste bloqué en `Terminating`. Il faut alors le forcer à
> la main (`kubectl patch ... -p '{"metadata":{"finalizers":[]}}' --type=merge`).

---

## 3. Comment le reconcile est déclenché

**On n'appelle jamais `Reconcile` soi-même.** C'est `controller-runtime` qui l'appelle,
selon un modèle **event-driven** + **level-triggered**.

### Le wiring (qui écoute quoi)
Dans `SetupWithManager` :
```go
ctrl.NewControllerManagedBy(mgr).
    For(&AgentPipeline{}).   // ressource principale surveillée
    Owns(&corev1.ConfigMap{}). // ressources possédées (enfants)
    Complete(r)
```
Le manager ouvre un **watch** (connexion long-poll vers l'API server) sur ces types.
L'API server **pousse** un event à chaque create/update/delete.

### Les sources de déclenchement
| Source | Exemple concret |
|---|---|
| **`For` — le CR** | `kubectl apply` du pipeline, ou modif de son spec. |
| **Update du `status`** | Chaque `r.Status().Update()` re-déclenche un reconcile → c'est ça qui fait **avancer la machine à états** pas à pas. |
| **`Owns` — les enfants** | Une ConfigMap fille modifiée/supprimée → K8s remonte au parent via l'OwnerReference et reconcile le pipeline. |
| **Requeue explicite** | `ctrl.Result{Requeue: true}` ou `{RequeueAfter: 10s}`. |
| **Resync périodique** | Même sans event, re-reconcile de tout l'inventaire (~10h par défaut) pour corriger une dérive. |
| **Redémarrage de l'opérateur** | Tout l'inventaire existant est rejoué au démarrage. |

### Ce que retourne `Reconcile` (le requeue)
```go
ctrl.Result{}, nil                       // fini, ne rien reprogrammer (états terminaux)
ctrl.Result{Requeue: true}, nil          // re-reconcile tout de suite (avancer d'un cran)
ctrl.Result{RequeueAfter: 10*time.Second}, nil // re-reconcile plus tard (attente, backoff)
ctrl.Result{}, err                       // erreur → requeue auto avec backoff exponentiel
```

### Principe clé : level-triggered, pas edge-triggered
`Reconcile` ne reçoit **pas** "ce qui a changé", juste "l'objet X mérite un coup d'œil".
À chaque appel il doit :
1. **Lire l'état désiré frais** (`r.Get` au début).
2. **Comparer avec l'état réel** (status, ConfigMaps existantes).
3. **Faire le pas nécessaire** pour rapprocher les deux.

→ D'où la grosse machine à états basée sur `status.phase` (et non sur l'event reçu).
→ Conséquence : `Reconcile` **doit être idempotent** (l'appeler 2× ne casse rien). C'est
exactement pourquoi la garde ConfigMap existe.

---

## La machine à états (vue d'ensemble)

```
phase="" / Pending ──► handlePending ──► Running ──► handleRunning ──► Completed / Failed (terminal)
```

- **handlePending** : ajoute le finalizer (requeue), valide les stages, init le status,
  passe en `Running`.
- **handleRunning** (appelé en boucle, **un stage par passe**) :
  1. `FindCurrentStage` → premier stage non terminé.
  2. `AreDependenciesMet` → sinon requeue 10s.
  3. Selon la phase du stage : `Pending` → lancer ; `Failed` → retry si quota, sinon échouer le pipeline.
  4. Plus de stage → `completePipeline`.
- **startStage** : garde anti-doublon (ConfigMap) → build context → render prompt →
  `RunAgent` (synchrone) → store output + status.

> ⚠️ L'invocation de l'agent est **synchrone** dans la boucle : un stage à timeout 5 min
> occupe un worker du contrôleur pendant 5 min. Simple, au prix du parallélisme.

---

## Les 3 mécaniques qui s'emboîtent

```
apply CR ──(watch For)──► Reconcile #1
   └─ handlePending: AddFinalizer ──(Update)──► Reconcile #2
        └─ init status, phase=Running ──(Status().Update)──► Reconcile #3
             └─ startStage → RunAgent → StoreOutput
                  ├─ ConfigMap créée (OwnerRef) ──(watch Owns)──► reconcile possible
                  └─ status Completed ──(Status().Update)──► Reconcile #4
                       └─ ... stage suivant ... ──► Completed (terminal)

delete CR ──► deletionTimestamp ──(watch)──► Reconcile
   └─ handleDeletion: RemoveFinalizer ──► K8s GC le CR + ConfigMaps (OwnerRef)
```

- **ConfigMap** = mémoire durable (passage inter-stages + idempotence).
- **Finalizer** = verrou qui garantit le ménage avant la disparition.
- **Reconcile** = boucle réveillée par les watches, avance la state machine via les requeues.

---

## Le pitch en 30 secondes (pour expliquer à l'oral)

> « C'est un opérateur Kubernetes : j'ai créé un nouveau type de ressource, `AgentPipeline`,
> via une CRD. Quand quelqu'un en crée une, mon contrôleur est réveillé par un *watch* et
> exécute une **boucle de réconciliation** : il lit l'état désiré, regarde où il en est, et
> fait avancer le pipeline d'un stage. Chaque stage appelle un agent IA via le protocole A2A,
> et je stocke sa sortie dans une **ConfigMap** (parce que le status d'un objet K8s est trop
> limité pour de gros blobs) — cette ConfigMap sert aussi à passer le résultat au stage
> suivant et à éviter de relancer un agent déjà exécuté. J'utilise un **finalizer** pour
> garantir un nettoyage propre à la suppression, et les ConfigMaps sont auto-supprimées via
> leur **OwnerReference**. Tout l'avancement se fait en réécrivant le `status`, ce qui
> re-déclenche la boucle jusqu'à l'état terminal `Completed` ou `Failed`. »
