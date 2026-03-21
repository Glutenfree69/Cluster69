# AgentPipeline Operator — Guide complet

Ce guide explique en detail le fonctionnement de l'operateur, la structure du code Go,
les concepts Kubernetes utilises, et comment tester/deployer le projet.
Il est ecrit pour quelqu'un qui decouvre Go.

---

## Table des matieres

1. [Vue d'ensemble](#1-vue-densemble)
2. [Concepts Go essentiels](#2-concepts-go-essentiels)
3. [Concepts Kubernetes Operator](#3-concepts-kubernetes-operator)
4. [Architecture du projet](#4-architecture-du-projet)
5. [Le CRD : AgentPipeline](#5-le-crd--agentpipeline)
6. [Le runner A2A : communiquer avec kagent](#6-le-runner-a2a--communiquer-avec-kagent)
7. [Le stage handler : passer les donnees entre etapes](#7-le-stage-handler--passer-les-donnees-entre-etapes)
8. [Le controller : la boucle de reconciliation](#8-le-controller--la-boucle-de-reconciliation)
9. [Le point d'entree : cmd/main.go](#9-le-point-dentree--cmdmaingo)
10. [Les tests](#10-les-tests)
11. [Commandes Make](#11-commandes-make)
12. [Deploiement sur le cluster](#12-deploiement-sur-le-cluster)
13. [Tester manuellement](#13-tester-manuellement)
14. [Glossaire](#14-glossaire)

---

## 1. Vue d'ensemble

### Le probleme

Tu as 3 agents kagent dans ton cluster :
- **diagnostic** : scanne le cluster et detecte les problemes
- **advisor** : analyse les problemes et propose des corrections
- **gitops-proposer** : cree des PRs GitHub avec les corrections

Aujourd'hui, tu dois les appeler manuellement un par un via l'UI kagent,
copier-coller la sortie de l'un pour la donner a l'autre.

### La solution

L'operateur AgentPipeline automatise cette chaine :

```
kubectl apply -f pipeline.yaml
         |
         v
  [Controller detecte le nouveau CR]
         |
         v
  Stage 1: diagnostic     -->  "OOMKill sur pod-xyz"
         |
         v (output passe automatiquement)
  Stage 2: advisor         -->  "Augmenter memory limit a 512Mi"
         |
         v (output passe automatiquement)
  Stage 3: gitops-proposer -->  PR creee sur GitHub
         |
         v
  Pipeline Completed !
```

Tu crees un YAML, tu l'appliques, et l'operateur fait tout le reste.

### Comment ca marche concretement

1. Tu ecris un YAML `AgentPipeline` qui definit les etapes (stages)
2. Tu l'appliques avec `kubectl apply`
3. L'operateur detecte le nouveau CR et commence la reconciliation
4. Pour chaque stage, il appelle l'agent kagent via HTTP (protocole A2A)
5. Il stocke la sortie de chaque agent dans un ConfigMap Kubernetes
6. Il passe cette sortie a l'agent suivant via des templates Go
7. A la fin, le pipeline est marque "Completed"

---

## 2. Concepts Go essentiels

### Packages

En Go, le code est organise en **packages**. Chaque dossier = un package.
Le nom du package est declare en haut de chaque fichier :

```go
package runner    // tous les fichiers dans internal/runner/
package controller // tous les fichiers dans internal/controller/
package main      // le point d'entree du programme (cmd/main.go)
```

Un package `main` avec une fonction `main()` = un programme executable.
Les autres packages sont des bibliotheques importees par d'autres.

### Structs

Une struct est l'equivalent d'une classe (sans heritage). C'est un regroupement de champs :

```go
type RunRequest struct {
    AgentName string        // champ public (majuscule)
    Namespace string
    Prompt    string
    Timeout   time.Duration
}
```

- **Majuscule** = public (exporte, visible depuis d'autres packages)
- **minuscule** = prive (visible uniquement dans le package)

### Interfaces

Une interface definit un contrat — un ensemble de methodes que quelqu'un doit implementer :

```go
type AgentRunner interface {
    RunAgent(ctx context.Context, req RunRequest) (*RunResult, error)
    CheckHealth(ctx context.Context) error
}
```

En Go, on n'ecrit PAS `implements`. Si une struct a les bonnes methodes,
elle implemente automatiquement l'interface. C'est le "duck typing" :
"si ca marche comme un canard, c'est un canard".

```go
// A2ARunner a les methodes RunAgent() et CheckHealth()
// donc A2ARunner implemente automatiquement AgentRunner
type A2ARunner struct { ... }
func (r *A2ARunner) RunAgent(...) (*RunResult, error) { ... }
func (r *A2ARunner) CheckHealth(...) error { ... }

// MockRunner aussi !
type MockRunner struct { ... }
func (m *MockRunner) RunAgent(...) (*RunResult, error) { ... }
func (m *MockRunner) CheckHealth(...) error { ... }
```

### Methodes (receivers)

En Go, on attache une methode a une struct avec un "receiver" :

```go
func (r *A2ARunner) RunAgent(ctx context.Context, req RunRequest) (*RunResult, error) {
    // r est l'equivalent de "self" ou "this"
    url := r.baseURL + "/api/a2a/..."
}
```

Le `(r *A2ARunner)` avant le nom de la methode dit :
"cette methode appartient a A2ARunner, et je l'appelle `r`".

### Pointeurs

`*RunResult` = un pointeur vers un RunResult.
`&RunResult{...}` = creer un RunResult et retourner son adresse.

```go
// Retourner un pointeur (le * dans le type de retour)
return &RunResult{
    Status: RunStatusCompleted,
    Output: "hello",
}, nil

// nil = "rien" (pas d'erreur, ou pas de valeur)
```

Pourquoi des pointeurs ? Pour eviter de copier de grosses structs en memoire,
et pour pouvoir retourner `nil` (= "pas de resultat").

### Gestion d'erreurs

Go n'a pas d'exceptions. Les fonctions retournent une erreur en dernier :

```go
result, err := r.Runner.RunAgent(ctx, req)
if err != nil {
    // quelque chose a plante, on gere l'erreur
    return ctrl.Result{}, fmt.Errorf("invoking agent: %w", err)
}
// tout va bien, on continue avec result
```

`fmt.Errorf("message: %w", err)` cree une nouvelle erreur qui "enveloppe" l'erreur
originale (le `%w`), ce qui permet de garder la trace complete.

### Context

`context.Context` est present partout en Go. C'est un objet qui porte :
- un **timeout** (annulation automatique apres X secondes)
- un **signal d'annulation** (quelqu'un a appuye sur Ctrl+C)
- des **valeurs** (metadata transmise entre fonctions)

```go
// Creer un contexte avec un timeout de 5 minutes
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
defer cancel() // toujours appeler cancel quand on a fini
```

### JSON tags

Les tags `json:"..."` sur les champs de struct controlent comment Go
serialise/deserialise le JSON et le YAML Kubernetes :

```go
type StageSpec struct {
    Name      string  `json:"name"`              // champ obligatoire
    Namespace string  `json:"namespace,omitempty"` // omis si vide
    Timeout   *metav1.Duration `json:"timeout,omitempty"` // pointeur = optionnel
}
```

- `omitempty` : le champ n'apparait pas dans le YAML s'il est vide/nil
- Le nom entre guillemets est le nom dans le YAML (pas le nom Go)

### Kubebuilder markers

Les commentaires `// +kubebuilder:...` ne sont PAS des commentaires normaux.
Ce sont des **markers** que l'outil `controller-gen` lit pour generer du code :

```go
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
// -> genere une validation OpenAPI : seules ces 4 valeurs sont acceptees

// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// -> ajoute une colonne "Phase" quand on fait kubectl get agentpipelines

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;create;delete
// -> genere une ClusterRole RBAC autorisant l'acces aux ConfigMaps

// +kubebuilder:subresource:status
// -> active le sous-resource /status (mis a jour separement du spec)
```

---

## 3. Concepts Kubernetes Operator

### Qu'est-ce qu'un Operator ?

Un operator est un programme qui tourne dans le cluster et etend Kubernetes
avec de la logique metier. Il suit le pattern **controller** :

1. **Observer** : surveiller les ressources (via l'API Kubernetes)
2. **Analyser** : comparer l'etat desire (spec) avec l'etat actuel (status)
3. **Agir** : faire les actions necessaires pour rapprocher les deux

### CRD (Custom Resource Definition)

Un CRD definit un nouveau type de ressource Kubernetes.
Notre CRD `AgentPipeline` permet d'ecrire :

```yaml
apiVersion: aiops.cluster69.io/v1alpha1
kind: AgentPipeline
```

Le CRD est genere automatiquement depuis les structs Go par `make manifests`.
Il est installe dans le cluster avec `kubectl apply -f config/crd/bases/`.

### Controller et Reconciliation

Le controller est une boucle infinie :

```
while true:
    event = attendre_evenement()  # un CR a ete cree/modifie/supprime
    reconcile(event)              # rapprocher l'etat actuel du desire
```

La fonction `Reconcile()` est appelee a chaque evenement.
Elle doit etre **idempotente** : l'appeler 2 fois doit donner le meme resultat.

### Spec vs Status

```yaml
spec:     # CE QUE L'UTILISATEUR VEUT (l'etat desire)
  stages:
    - name: diagnose
      agentRef: ...

status:   # CE QUI SE PASSE VRAIMENT (l'etat observe)
  phase: Running
  currentStage: diagnose
```

- **spec** : ecrit par l'utilisateur, lu par le controller
- **status** : ecrit par le controller, lu par l'utilisateur
- Le status est un **subresource** (mis a jour avec `r.Status().Update()`)

### Finalizers

Un finalizer empeche Kubernetes de supprimer une ressource avant qu'on ait
fait le menage. Le flow :

1. User fait `kubectl delete agentpipeline mon-pipeline`
2. Kubernetes met un `deletionTimestamp` mais ne supprime PAS
3. Notre controller voit le timestamp, nettoie les ressources
4. Notre controller retire le finalizer
5. Kubernetes supprime enfin la ressource

### OwnerReferences

Quand on cree un ConfigMap pour stocker la sortie d'un stage, on met une
OwnerReference vers le pipeline. Resultat :
- Si le pipeline est supprime, Kubernetes supprime automatiquement les ConfigMaps
- C'est le garbage collector de Kubernetes

### Events

Les Events sont des messages visibles avec `kubectl describe agentpipeline` :

```
Events:
  Type    Reason            Message
  ----    ------            -------
  Normal  PipelineStarted   Pipeline started with 3 stages
  Normal  StageCompleted    Stage "diagnose" completed in 45s
  Normal  PipelineCompleted Pipeline completed all 3 stages
```

On les emet avec `r.Recorder.Event(pipeline, type, reason, message)`.

---

## 4. Architecture du projet

```
agentpipeline-operator/
|
|-- api/v1alpha1/                    # TYPES (le "quoi")
|   |-- agentpipeline_types.go       #   Structs Go = schema du CRD
|   |-- groupversion_info.go         #   Enregistrement du groupe/version
|   `-- zz_generated.deepcopy.go     #   Auto-genere par controller-gen
|
|-- internal/                        # CODE INTERNE (le "comment")
|   |-- runner/                      #   Communication avec kagent
|   |   |-- interface.go             #     Interface AgentRunner
|   |   |-- types.go                 #     RunRequest, RunResult
|   |   |-- a2a_runner.go            #     Implementation reelle (HTTP/SSE)
|   |   |-- a2a_runner_test.go       #     Tests du runner
|   |   `-- mock_runner.go           #     Faux runner pour les tests
|   |
|   `-- controller/                  #   Logique de reconciliation
|       |-- agentpipeline_controller.go      # Machine a etats principale
|       |-- agentpipeline_controller_test.go # Tests du controller
|       |-- stage_handler.go                 # Templates, ConfigMaps, avancement
|       |-- stage_handler_test.go            # Tests du handler
|       `-- suite_test.go                    # Setup envtest (Ginkgo)
|
|-- cmd/
|   `-- main.go                      # POINT D'ENTREE (assemblage des pieces)
|
|-- config/                          # MANIFESTES KUBERNETES (generes)
|   |-- crd/bases/                   #   Le CRD YAML genere
|   |-- rbac/                        #   ClusterRole, ServiceAccount
|   |-- manager/                     #   Deployment du controller
|   |-- default/                     #   Kustomize overlay (tout ensemble)
|   `-- samples/                     #   Exemple de pipeline
|
|-- Dockerfile                       # Image Docker multi-stage
|-- Makefile                         # Commandes de build/test
`-- go.mod                           # Dependances Go
```

### Comment les pieces s'assemblent

```
cmd/main.go  (cree le Manager, injecte le Runner dans le Controller)
     |
     v
AgentPipelineReconciler  (recoit les events, orchestre la logique)
     |
     |--- Runner (AgentRunner interface)
     |     |-- A2ARunner  (en production : appel HTTP a kagent)
     |     `-- MockRunner (en test : reponses pre-configurees)
     |
     `--- StageHandler (operations sur les stages)
           |-- RenderPrompt()      : interprete les templates Go
           |-- BuildPipelineContext() : lit les ConfigMaps des stages precedents
           |-- StoreOutput()       : cree un ConfigMap avec la sortie
           |-- FindCurrentStage()  : trouve le prochain stage a executer
           `-- AreDependenciesMet(): verifie les dependances
```

---

## 5. Le CRD : AgentPipeline

### Fichier : `api/v1alpha1/agentpipeline_types.go`

Ce fichier definit **toute la structure** du CRD. C'est la "source de verite"
depuis laquelle Kubebuilder genere le schema OpenAPI et les validations.

### Les types enum (constantes)

```go
type PipelinePhase string
const (
    PhasePending   PipelinePhase = "Pending"    // pipeline cree, pas encore demarre
    PhaseRunning   PipelinePhase = "Running"    // en cours d'execution
    PhaseCompleted PipelinePhase = "Completed"  // tous les stages termines
    PhaseFailed    PipelinePhase = "Failed"     // un stage a echoue definitivement
)
```

Go n'a pas de vrai `enum` comme Python ou Java. On utilise `type X string` + `const`.
Le marker `+kubebuilder:validation:Enum=...` genere la validation cote Kubernetes.

### Le Spec (ce que l'utilisateur definit)

```go
type AgentPipelineSpec struct {
    Trigger     TriggerSpec   // comment le pipeline demarre (manual/alertmanager)
    Stages      []StageSpec   // la liste ordonnee des etapes
    RetryPolicy *RetryPolicy  // politique de retry par defaut
}
```

Le `*` devant `RetryPolicy` (pointeur) signifie "optionnel" :
- `nil` = pas defini (on utilise les valeurs par defaut)
- `&RetryPolicy{MaxRetries: 2}` = defini

### Un stage en detail

```go
type StageSpec struct {
    Name        string            // ex: "diagnose"
    AgentRef    AgentReference    // quel agent appeler (name + namespace)
    Timeout     *metav1.Duration  // max 5m par defaut
    DependsOn   []string          // quels stages doivent finir avant
    Prompt      string            // template Go avec {{.PreviousOutput}}
    Inputs      map[string]string // cles/valeurs statiques
    Config      *StageConfig      // config specifique (autoMerge, targetRepo)
    RetryPolicy *RetryPolicy      // override la policy du pipeline
}
```

### Le Status (ce que le controller ecrit)

```go
type AgentPipelineStatus struct {
    Phase        PipelinePhase      // Pending/Running/Completed/Failed
    CurrentStage string             // nom du stage en cours
    StartedAt    *metav1.Time       // quand le pipeline a demarre
    CompletedAt  *metav1.Time       // quand il a fini
    Stages       []StageStatus      // status de chaque stage
    Conditions   []metav1.Condition // conditions standard K8s
}
```

Chaque stage a son propre status :

```go
type StageStatus struct {
    Name        string     // "diagnose"
    Phase       StagePhase // Pending/Running/Completed/Failed
    StartedAt   *metav1.Time
    CompletedAt *metav1.Time
    RetryCount  int32      // nombre de retries effectues
    Output      string     // sortie tronquee (max 1024 chars)
    OutputRef   string     // nom du ConfigMap avec la sortie complete
    Error       string     // message d'erreur si Failed
    TaskID      string     // ID de la tache kagent A2A
}
```

### Exemple YAML complet

```yaml
apiVersion: aiops.cluster69.io/v1alpha1
kind: AgentPipeline
metadata:
  name: incident-response
  namespace: kagent
spec:
  trigger:
    type: manual              # seul type supporte pour l'instant
  retryPolicy:
    maxRetries: 1             # 1 retry en cas d'echec
    backoff: 30s              # attendre 30s avant de retenter
  stages:
    - name: diagnose
      agentRef:
        name: diagnostic      # nom du CR kagent Agent
        namespace: kagent
      timeout: 5m
      prompt: |
        Effectue un diagnostic complet du cluster.

    - name: advise
      agentRef:
        name: advisor
        namespace: kagent
      timeout: 5m
      dependsOn: ["diagnose"]  # attend que diagnose finisse
      prompt: |
        Voici le diagnostic :
        {{.PreviousOutput}}
        Propose des corrections.

    - name: propose-fix
      agentRef:
        name: gitops-proposer
        namespace: kagent
      timeout: 5m
      dependsOn: ["advise"]
      prompt: |
        Voici les recommandations :
        {{.PreviousOutput}}
        Cree une PR pour la plus prioritaire.
```

---

## 6. Le runner A2A : communiquer avec kagent

### Decouverte cle : les agents kagent ne sont PAS crees par l'operateur

Les Agent CRs kagent (`diagnostic`, `advisor`, `gitops-proposer`) sont des
**declarations statiques** deja deployees dans le cluster. Le controller kagent
les transforme en services accessibles via HTTP.

Pour invoquer un agent, on fait un appel HTTP au controller kagent :

```
POST http://kagent-controller.kagent.svc.cluster.local:8083/api/a2a/kagent/diagnostic/task
Content-Type: application/json
Accept: text/event-stream

{
  "app_name": "diagnostic",
  "user_id": "admin@kagent.dev",
  "message": "Effectue un diagnostic complet du cluster."
}
```

La reponse est un **flux SSE** (Server-Sent Events) :

```
event: task-status
data: {"id":"task-123","status":{"state":"working","message":{"parts":[{"text":"analyzing..."}]}}}

event: task-status
data: {"id":"task-123","status":{"state":"completed","message":{"parts":[{"text":"OOMKill on pod-xyz"}]}}}
```

### Fichier : `internal/runner/interface.go`

```go
type AgentRunner interface {
    RunAgent(ctx context.Context, req RunRequest) (*RunResult, error)
    CheckHealth(ctx context.Context) error
}
```

C'est un **contrat**. Le controller ne sait pas s'il parle au vrai kagent
ou a un mock. Il appelle juste `r.Runner.RunAgent(...)`.

**Pourquoi une interface ?**
- En test : on injecte un `MockRunner` qui repond instantanement
- En production : on injecte un `A2ARunner` qui fait le vrai appel HTTP
- On peut changer l'implementation sans toucher au controller

### Fichier : `internal/runner/a2a_runner.go`

Le `A2ARunner` fait 3 choses :

1. **Construire la requete HTTP** (POST JSON)
2. **Envoyer la requete** avec un timeout via `context.WithTimeout`
3. **Parser le flux SSE** ligne par ligne avec `bufio.Scanner`

La methode `parseSSEStream` lit le flux SSE :
- Chaque event est delimite par une ligne vide
- `event: task-status` = type d'event
- `data: {...}` = payload JSON
- On extrait le `state` : `working` (on continue), `completed` (on a fini), `failed` (erreur)

```go
// Parsing SSE simplifie :
for scanner.Scan() {
    line := scanner.Text()
    if line == "" {
        // fin d'un event, on le traite
        processEvent(&currentEvent, &result)
    }
    if strings.HasPrefix(line, "data:") {
        currentEvent.Data = strings.TrimPrefix(line, "data:")
    }
}
```

### Fichier : `internal/runner/mock_runner.go`

Le mock est simple : on pre-configure une reponse par nom d'agent.

```go
mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{
    "diagnostic": {Output: "OOMKill on pod-xyz", Status: runner.RunStatusCompleted},
    "advisor":    {Output: "Increase memory to 512Mi", Status: runner.RunStatusCompleted},
})
```

Quand le controller appelle `RunAgent("diagnostic", ...)`, le mock retourne
immediatement la reponse pre-configuree. Il enregistre aussi tous les appels
dans `mockRunner.Calls` pour qu'on puisse verifier dans les tests.

---

## 7. Le stage handler : passer les donnees entre etapes

### Fichier : `internal/controller/stage_handler.go`

Le stage handler gere le "plomberie" entre les stages.

### Templates Go

La syntaxe `{{.PreviousOutput}}` dans les prompts est du **Go templating**.
C'est le meme systeme que les templates Helm (qui utilisent aussi `text/template`).

```go
// Le contexte passe au template
type PipelineContext struct {
    PreviousOutput string            // sortie du stage precedent
    Inputs         map[string]string // cles/valeurs statiques
    stageOutputs   map[string]string // sorties de tous les stages (prive)
}
```

Variables disponibles dans les prompts :
- `{{.PreviousOutput}}` : sortie du stage juste avant
- `{{.StageOutput "diagnose"}}` : sortie d'un stage specifique
- `{{index .Inputs "repo"}}` : valeur d'un input statique

### Stockage des sorties dans ConfigMaps

Les sorties d'agents peuvent etre longues (plusieurs Ko). Les mettre dans le
status du CR poserait des problemes de taille (le status total d'un CR K8s
est limite a ~1 Mo).

Solution : on stocke la sortie complete dans un **ConfigMap** :

```
ConfigMap: incident-response-stage-diagnose
  namespace: kagent
  data:
    output: "OOMKill on pod frontend-xyz, recommend memory limit increase..."
  ownerReferences:
    - kind: AgentPipeline
      name: incident-response    # GC automatique a la suppression
```

Dans le status, on ne garde que les **1024 derniers caracteres** (tronques)
et le nom du ConfigMap (`outputRef`) pour reference.

### Fonctions utilitaires

```go
// Trouve le prochain stage a executer (le premier qui n'est pas Completed)
FindCurrentStage(pipeline) -> (stageSpec, stageStatus)

// Verifie que tous les stages dans dependsOn sont termines
AreDependenciesMet(pipeline, stage) -> bool

// Initialise le status de chaque stage a "Pending"
InitStageStatuses(pipeline)

// Retourne la retry policy applicable (stage > pipeline > defaut)
GetEffectiveRetryPolicy(pipeline, stage) -> *RetryPolicy
```

---

## 8. Le controller : la boucle de reconciliation

### Fichier : `internal/controller/agentpipeline_controller.go`

C'est le coeur de l'operateur. La machine a etats :

```
                   CR cree
                     |
                     v
              +----------+
              | Pending  |  (ajouter finalizer, initialiser status)
              +----+-----+
                   |
                   v
              +----------+
              | Running  |  (executer les stages un par un)
              +----+-----+
                  / \
                 /   \
                v     v
        +-----------+  +--------+
        | Completed |  | Failed |
        +-----------+  +--------+
```

### Le reconciler struct

```go
type AgentPipelineReconciler struct {
    client.Client              // client Kubernetes (Get, List, Update, etc.)
    Scheme   *runtime.Scheme   // schema des types connus
    Recorder record.EventRecorder // pour emettre des Events K8s
    Runner   runner.AgentRunner   // interface pour appeler kagent
    Handler  *StageHandler        // operations sur les stages
}
```

### La methode Reconcile()

C'est la methode appelee a chaque evenement. Elle suit ce flow :

```go
func (r *AgentPipelineReconciler) Reconcile(ctx, req) (Result, error) {
    // 1. Lire le CR depuis le cache K8s
    pipeline := &AgentPipeline{}
    r.Get(ctx, req.NamespacedName, pipeline)

    // 2. Si supprime -> nettoyer et retirer le finalizer
    if pipeline.DeletionTimestamp != nil {
        return r.handleDeletion(ctx, pipeline)
    }

    // 3. Selon la phase actuelle, agir differemment
    switch pipeline.Status.Phase {
    case Pending:  return r.handlePending(ctx, pipeline)
    case Running:  return r.handleRunning(ctx, pipeline)
    case Completed, Failed: return Result{}, nil  // rien a faire
    }
}
```

### handlePending : demarrer le pipeline

1. Ajouter le **finalizer** (empeche la suppression avant le cleanup)
2. Valider les stages (au moins 1 stage)
3. Initialiser le status (phase=Running, stages=[Pending, Pending, ...])
4. Emettre un Event "PipelineStarted"

### handleRunning : executer les stages

1. Trouver le stage courant (`FindCurrentStage`)
2. Verifier les dependances (`AreDependenciesMet`)
3. Si le stage est `Pending` : appeler `startStage`
4. Si le stage est `Failed` : verifier la retry policy
   - Si retries restants : repasser en Pending avec backoff
   - Sinon : echouer le pipeline

### startStage : invoquer un agent

1. Construire le contexte de template (`BuildPipelineContext`)
2. Rendre le prompt (`RenderPrompt`)
3. Appeler l'agent via A2A (`Runner.RunAgent`) — **bloquant**
4. Selon le resultat :
   - `Completed` : stocker l'output dans un ConfigMap, passer au stage suivant
   - `Failed/TimedOut` : marquer le stage comme echoue

**Point important** : `RunAgent` est synchrone. Le goroutine du controller
est bloquee pendant que l'agent travaille (1-5 minutes typiquement).
C'est OK car on traite tres peu de pipelines en parallele.

### Les retours de Reconcile()

```go
return ctrl.Result{Requeue: true}, nil
// -> rappeler Reconcile() immediatement

return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
// -> rappeler dans 10 secondes

return ctrl.Result{}, nil
// -> ne pas rappeler (etat terminal)

return ctrl.Result{}, err
// -> erreur, controller-runtime va reessayer avec backoff exponentiel
```

### SetupWithManager : enregistrer le controller

```go
func (r *AgentPipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&AgentPipeline{}).           // surveiller les AgentPipeline CRs
        Owns(&corev1.ConfigMap{}).       // surveiller les ConfigMaps qu'on possede
        Named("agentpipeline").
        Complete(r)
}
```

- `For(...)` : event sur un AgentPipeline → reconcile ce pipeline
- `Owns(...)` : event sur un ConfigMap possede → reconcile le pipeline parent

---

## 9. Le point d'entree : cmd/main.go

### Ce que fait main()

```go
func main() {
    // 1. Parser les flags (--leader-elect, --metrics-bind-address, etc.)
    flag.Parse()

    // 2. Creer le Manager (le chef d'orchestre controller-runtime)
    mgr := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        Scheme:           scheme,
        LeaderElection:   enableLeaderElection,
        LeaderElectionID: "7791af13.cluster69.io",
        // ...
    })

    // 3. Creer le Runner A2A (lit les env vars KAGENT_A2A_URL et KAGENT_USER_ID)
    agentRunner := runner.NewA2ARunner(os.Getenv("KAGENT_A2A_URL"), os.Getenv("KAGENT_USER_ID"))

    // 4. Creer et enregistrer le controller
    reconciler := &controller.AgentPipelineReconciler{
        Client:   mgr.GetClient(),
        Scheme:   mgr.GetScheme(),
        Recorder: mgr.GetEventRecorderFor("agentpipeline-controller"),
        Runner:   agentRunner,     // injection de dependance !
        Handler:  controller.NewStageHandler(mgr.GetClient()),
    }
    reconciler.SetupWithManager(mgr)

    // 5. Demarrer le manager (boucle infinie)
    mgr.Start(ctrl.SetupSignalHandler())
}
```

### Schema et enregistrement des types

```go
func init() {
    // Enregistrer les types K8s standard (Pod, ConfigMap, etc.)
    clientgoscheme.AddToScheme(scheme)
    // Enregistrer nos types custom (AgentPipeline)
    aiopsv1alpha1.AddToScheme(scheme)
}
```

Le `scheme` est comme un registre : il dit a controller-runtime comment
deserialiser les objets Kubernetes en structs Go.

### Variables d'environnement

| Variable | Defaut | Description |
|----------|--------|-------------|
| `KAGENT_A2A_URL` | `http://kagent-controller.kagent.svc.cluster.local:8083` | URL du service kagent |
| `KAGENT_USER_ID` | `admin@kagent.dev` | User ID pour l'API A2A |

---

## 10. Les tests

### Framework : Ginkgo + Gomega + envtest

- **Ginkgo** : framework de test BDD (Describe/Context/It)
- **Gomega** : assertions (Expect/To/Equal)
- **envtest** : lance un vrai API server K8s en local pour les tests

### Runner tests (`internal/runner/a2a_runner_test.go`)

Tests standard Go (`testing.T`). Utilisent `httptest.NewServer` pour simuler
le serveur kagent SSE :

```go
func TestA2ARunnerSuccess(t *testing.T) {
    // Creer un faux serveur HTTP qui retourne un flux SSE
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Ecrire des events SSE
        fmt.Fprint(w, "event: task-status\n")
        fmt.Fprint(w, `data: {"id":"task-123","status":{"state":"completed",...}}\n\n`)
    }))

    runner := NewA2ARunner(server.URL, "test@test.com")
    result, err := runner.RunAgent(ctx, RunRequest{...})

    // Verifier le resultat
    if result.Status != RunStatusCompleted { t.Error("...") }
}
```

7 tests couvrent : succes, echec, timeout, erreur HTTP, multi-part, health check.

### Controller tests (`internal/controller/agentpipeline_controller_test.go`)

Tests Ginkgo avec envtest (vrai API server K8s) :

```go
var _ = Describe("AgentPipeline Controller", func() {
    It("should complete successfully", func() {
        // 1. Creer un pipeline
        pipeline := newPipeline("test", stages)
        Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

        // 2. Creer un mock runner
        mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{
            "diagnostic": {Output: "ok", Status: runner.RunStatusCompleted},
        })
        reconciler := newTestReconciler(mockRunner)

        // 3. Boucler sur Reconcile() jusqu'a completion
        for i := 0; i < 10; i++ {
            reconciler.Reconcile(ctx, reconcile.Request{...})
            // Verifier si termine
        }

        // 4. Verifier l'etat final
        Expect(final.Status.Phase).To(Equal(PhaseCompleted))
    })
})
```

### Stage handler tests (`internal/controller/stage_handler_test.go`)

Tests unitaires purs (pas besoin d'API server) pour :
- Rendering de templates avec `{{.PreviousOutput}}`
- Rendering avec `{{.StageOutput "name"}}`
- Rendering avec `{{index .Inputs "key"}}`
- Truncation des sorties longues
- Verification des dependances
- Resolution de la retry policy

### Lancer les tests

```bash
cd agentpipeline-operator

# Tous les tests (telecharge envtest automatiquement)
make test

# Seulement les tests du runner (rapide, pas besoin d'envtest)
go test ./internal/runner/ -v

# Seulement les tests du controller (avec envtest)
go test ./internal/controller/ -v

# Avec couverture detaillee
go test ./internal/... -coverprofile=cover.out
go tool cover -html=cover.out  # ouvre un rapport HTML
```

---

## 11. Commandes Make

```bash
# --- GENERATION DE CODE ---
make manifests    # Genere le CRD YAML + RBAC depuis les markers Go
make generate     # Genere les methodes DeepCopy sur les structs

# --- BUILD ---
make build        # Compile le binaire dans bin/manager
make run          # Compile et lance localement (avec ton kubeconfig)

# --- TEST ---
make test         # Tous les tests + couverture
make lint         # Linting avec golangci-lint

# --- DOCKER ---
make docker-build IMG=mon-registry/agentpipeline-operator:v0.1.0
make docker-push  IMG=mon-registry/agentpipeline-operator:v0.1.0

# --- DEPLOIEMENT ---
make install      # Installe le CRD dans le cluster
make uninstall    # Supprime le CRD du cluster
make deploy       # Deploie le controller dans le cluster
make undeploy     # Supprime le controller du cluster

# --- UTILITAIRES ---
make fmt          # Formate le code Go
make vet          # Analyse statique
```

### Les commandes les plus utiles au quotidien

```bash
# Apres avoir modifie les types dans agentpipeline_types.go :
make manifests generate

# Pour verifier que tout compile :
make build

# Pour lancer les tests :
make test

# Pour construire et pousser l'image Docker :
make docker-build docker-push IMG=<registry>/<image>:<tag>
```

---

## 12. Deploiement sur le cluster

### Prerequis

1. Le CRD kagent doit etre installe (deja fait via `apps/kagent-crds.yaml`)
2. Les agents doivent etre deployes (deja fait via `apps/kagent-agents.yaml`)
3. Le controller kagent doit tourner (deja fait via `apps/kagent.yaml`)

### Etape 1 : construire l'image Docker

```bash
cd agentpipeline-operator

# Construire l'image (remplacer par ton registry Docker)
make docker-build IMG=ghcr.io/glutenfree69/agentpipeline-operator:v0.1.0

# Pousser vers le registry
make docker-push IMG=ghcr.io/glutenfree69/agentpipeline-operator:v0.1.0
```

Le Dockerfile fait un build multi-stage :
1. **Stage builder** : compile le Go dans une image golang
2. **Stage runtime** : copie le binaire dans une image distroless (ultra-legere)

### Etape 2 : deployer dans le cluster

Option A — avec make :
```bash
make deploy IMG=ghcr.io/glutenfree69/agentpipeline-operator:v0.1.0
```

Option B — via ArgoCD (recommande pour ton setup GitOps) :

Creer `apps/agentpipeline-operator.yaml` :
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: agentpipeline-operator
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/Glutenfree69/Cluster69
    path: agentpipeline-operator/config/default
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: kagent
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

### Etape 3 : configurer l'URL kagent

Le controller a besoin de savoir ou trouver kagent.
Dans le Deployment (`config/manager/manager.yaml`), ajouter les env vars :

```yaml
env:
  - name: KAGENT_A2A_URL
    value: "http://kagent-controller.kagent.svc.cluster.local:8083"
  - name: KAGENT_USER_ID
    value: "admin@kagent.dev"
```

---

## 13. Tester manuellement

### Verifier que le CRD est installe

```bash
kubectl get crd agentpipelines.aiops.cluster69.io
```

### Creer un pipeline

```bash
kubectl apply -f config/samples/aiops_v1alpha1_agentpipeline.yaml
```

### Suivre l'execution

```bash
# Vue d'ensemble (les printcolumns)
kubectl get agentpipelines -n kagent -w
# NAME                PHASE     STAGE       AGE
# incident-response   Running   diagnose    5s
# incident-response   Running   advise      48s
# incident-response   Completed             2m10s

# Details complets
kubectl describe agentpipeline incident-response -n kagent

# Voir les events
kubectl get events -n kagent --field-selector involvedObject.name=incident-response

# Lire la sortie d'un stage
kubectl get configmap incident-response-stage-diagnose -n kagent -o jsonpath='{.data.output}'

# Voir les logs du controller
kubectl logs -n kagent -l control-plane=controller-manager -f
```

### Supprimer un pipeline

```bash
kubectl delete agentpipeline incident-response -n kagent
# Les ConfigMaps sont automatiquement supprimes (OwnerReferences)
```

### Debugger

Si un pipeline reste bloque :

```bash
# Voir le status detaille
kubectl get agentpipeline incident-response -n kagent -o yaml

# Verifier que kagent est accessible
kubectl exec -n kagent deploy/kagent-controller -- \
  wget -qO- http://localhost:8083/api/a2a

# Verifier les logs du controller
kubectl logs -n kagent -l control-plane=controller-manager --tail=50
```

---

## 14. Glossaire

| Terme | Definition |
|-------|-----------|
| **CRD** | Custom Resource Definition — etend l'API Kubernetes avec un nouveau type |
| **CR** | Custom Resource — une instance d'un CRD (ex: un AgentPipeline specifique) |
| **Controller** | Programme qui surveille des CRs et agit pour atteindre l'etat desire |
| **Operator** | Un controller + un CRD = un pattern pour automatiser des taches complexes |
| **Reconcile** | La fonction appelee quand un CR change. Doit etre idempotente |
| **Finalizer** | Mecanisme pour bloquer la suppression d'un CR jusqu'au cleanup |
| **OwnerReference** | Lien parent-enfant entre ressources K8s (GC automatique) |
| **Status subresource** | Partie du CR mise a jour separement du spec |
| **Kubebuilder** | Framework pour generer le squelette d'un operator Go |
| **controller-runtime** | Bibliotheque Go qui fournit le Manager, les caches, les watchers |
| **envtest** | Mini API server K8s pour les tests unitaires |
| **A2A** | Agent-to-Agent — protocole HTTP de kagent pour invoquer des agents |
| **SSE** | Server-Sent Events — flux de donnees HTTP unidirectionnel |
| **ConfigMap** | Ressource K8s pour stocker des donnees cle-valeur (on y met les outputs) |
| **Scheme** | Registre Go qui mappe les GVK (Group/Version/Kind) aux types Go |
| **GVK** | Group/Version/Kind — identifiant unique d'un type K8s (ex: aiops.cluster69.io/v1alpha1/AgentPipeline) |
| **RBAC** | Role-Based Access Control — permissions du service account du controller |
| **Leader election** | Mecanisme pour qu'un seul replica du controller soit actif a la fois |
| **Kustomize** | Outil pour composer et personnaliser des manifestes K8s (utilise par Kubebuilder) |
| **struct** | Type Go equivalent a une classe (sans heritage) |
| **interface** | Contrat Go — ensemble de methodes qu'un type doit implementer |
| **pointeur** | Adresse memoire (`*Type` = pointeur, `&value` = prendre l'adresse) |
| **goroutine** | Thread leger Go (le controller-runtime en utilise pour chaque reconcile) |
| **receiver** | Le `(r *MyStruct)` avant une methode — equivalent de `self`/`this` |
