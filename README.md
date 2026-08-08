# Cluster69

Cluster Kubernetes self-managed (kubeadm) sur AWS, provisionné from scratch avec Terraform + Ansible. GitOps via ArgoCD (App of Apps).

## Architecture

```mermaid
flowchart TB
    subgraph DEV[\"Poste local\"]
        TF[\"Terraform\"]
        AN[\"Ansible\"]
        KC[\"kubectl / k9s\"]
    end

    subgraph AWS[\"AWS eu-west-3\"]
        subgraph VPC[\"VPC 10.10.0.0/16\"]
            subgraph SUBNET[\"Subnet public 10.10.1.0/24 — eu-west-3a\"]
                K1[\"kube-1\\nt3.medium · 4 GB\\nControl Plane + Worker\"]
                K2[\"kube-2\\nt3.small · 2 GB\\nWorker\"]
                IG[\"ingress\\nt3.small · 2 GB\\nIngress Controller\"]
                MO[\"monitoring\\nt3.medium · 4 GB\\nPrometheus · Grafana · Loki · kagent\"]
            end
            IGW[\"Internet Gateway\"]
        end
        SSM[\"SSM Parameter Store\\n/cluster69/kubeconfig\"]
        S3[\"S3 — Terraform state\"]
        BED[\"Amazon Bedrock\\nClaude Haiku 4.5\"]
    end

    TF -- \"terraform apply\" --> VPC
    TF -- \"tfstate\" --> S3
    TF -- \"inventory\" --> AN
    AN -- \"SSH\" --> K1
    AN -- \"SSH\" --> K2
    AN -- \"SSH\" --> IG
    AN -- \"SSH\" --> MO
    AN -- \"kubeconfig\" --> SSM
    KC -- \"kubectl (6443)\" --> K1

    K1 -- \"kubeadm join\" --> K2
    K1 -- \"kubeadm join\" --> IG
    K1 -- \"kubeadm join\" --> MO

    MO -. \"InvokeModel\" .-> BED

    IGW --- SUBNET

    style K1 fill:#4a90d9,color:#fff
    style K2 fill:#7ab648,color:#fff
    style IG fill:#e6a023,color:#fff
    style MO fill:#d94a7a,color:#fff
    style BED fill:#8b5cf6,color:#fff
```

| Node | Role | Instance | RAM | Disque | SG |
|---|---|---|---|---|---|
| kube-1 | Control plane + worker | t3.medium | 4 GB | 20 GB | k8s_nodes |
| kube-2 | Worker | t3.small | 2 GB | 20 GB | k8s_nodes |
| ingress | Ingress controller (Nginx) | t3.small | 2 GB | 20 GB | public_nodes |
| monitoring | Prometheus / Grafana / Loki / kagent | t3.medium | 4 GB | 30 GB | public_nodes |

**Cout estime : ~$97/mois**

## Stack applicative

Deploye automatiquement par ArgoCD via le dossier `apps/` (App of Apps pattern).

| App | Chart Helm | Namespace | Role |
|---|---|---|---|
| ingress-nginx | `ingress-nginx` 4.15.0 | ingress-nginx | Reverse proxy, hostNetwork sur node ingress |
| podinfo | `podinfo` 6.11.1 | podinfo | App de demo |
| local-path-provisioner | `local-path-provisioner` 0.0.32 | local-path-storage | StorageClass pour PVC (hostPath) |
| metrics-server | `metrics-server` 3.13.0 | kube-system | `kubectl top`, HPA |
| kube-prometheus-stack | `kube-prometheus-stack` 82.10.5 | monitoring | Prometheus, Grafana, Alertmanager, node-exporter, kube-state-metrics |
| loki | `loki` 6.55.0 | monitoring | Agregation de logs (SingleBinary) |
| alloy | `alloy` 1.6.2 | monitoring | Collecte de logs (DaemonSet, successeur de Promtail) |
| kagent | `kagent` 0.7.23 | kagent | Agents IA autonomes (diagnostic, advisor, gitops-proposer) via Amazon Bedrock |

**Acces web** (via ingress-nginx) :

| Path | Service |
|---|---|
| `/` | podinfo |
| `/grafana` | Grafana (admin/admin) |
| `/prometheus` | Prometheus UI |

## kagent (agents IA autonomes)

kagent remplace K8sGPT avec une approche multi-agents. Chaque agent a acces au **cluster** (etat reel via kubectl/prometheus) ET au **repo GitHub** (etat desire/code). Cette double visibilite elimine les faux positifs et permet des recommandations contextualisees.

```
                    ┌─────────────────────┐
                    │   Amazon Bedrock     │
                    │  (Claude Haiku 4.5)  │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │   kagent Controller  │
                    │   (namespace: kagent)│
                    │   node: monitoring   │
                    └──────────┬──────────┘
           ┌───────────────────┼───────────────────┐
           │                   │                   │
┌──────────▼────────┐ ┌───────▼────────┐ ┌────────▼─────────┐
│  diagnostic       │ │ advisor        │ │ gitops-proposer  │
│                   │ │                │ │                  │
│ kubectl (ro)      │ │ kubectl (ro)   │ │ kubectl (ro)     │
│ prometheus        │ │ prometheus     │ │ GitHub (rw)      │
│ GitHub (ro)       │ │ GitHub (ro)    │ │                  │
│                   │ │                │ │ Cree des PRs     │
│ Scan cluster +    │ │ Recommandations│ │ sur le repo      │
│ filtre faux pos.  │ │ contextualisees│ │                  │
└───────────────────┘ └────────────────┘ └──────────────────┘
```

### 3 agents

| Agent | Role | Outils |
|---|---|---|
| `diagnostic` | Scan la sante du cluster, filtre les faux positifs en croisant avec les manifests du repo | kubectl, prometheus, GitHub (lecture) |
| `advisor` | Analyse metriques/logs/secu, propose des ameliorations adaptees au cluster | kubectl, prometheus, GitHub (lecture) |
| `gitops-proposer` | Transforme les recommandations en PRs sur le repo GitHub | kubectl, GitHub (lecture + ecriture) |

### Monitoring tools (Grafana MCP)

Les agents accedent a Prometheus et Loki **via Grafana** (MCP server `grafana-mcp`), pas directement. Le flux est :

```
Agent → kagent-grafana-mcp → Grafana API → Datasources (Prometheus, Loki)
```

Cela concerne les outils : `query_prometheus`, `list_prometheus_metric_names`, `query_loki_logs`, `list_loki_label_names`, `search_dashboards`, etc.

**Configuration requise** (`apps/kagent.yaml`) :

```yaml
grafana-mcp:
  grafana:
    url: "http://kube-prometheus-stack-grafana.monitoring.svc.cluster.local:80/grafana"
    secretRef: "grafana-mcp-token"
  args:
    - --allowed-hosts
    - "kagent-grafana-mcp.kagent:8000"
```

> L'URL par defaut du chart kagent (`grafana.kagent:3000/api`) est incorrecte — Grafana est dans le namespace `monitoring`, pas `kagent`. On met le sous-chemin `/grafana` (Grafana est en `serve_from_sub_path: true`, root_url `/grafana/`) **sans** `/api` : mcp-grafana ajoute `/api` lui-meme (`makeBasePath`), donc mettre `/grafana/api` donnerait `/grafana/api/api/...` → `404`.

**Deux pieges a regler pour eviter le `403 Forbidden` au reconcile (`kagent-grafana-mcp`) :**

1. **`--allowed-hosts`** (la vraie cause du 403) : mcp-grafana valide le header `Host` de chaque requete (protection anti-DNS-rebinding). Par defaut il n'autorise que le loopback ; le controller l'appelle via `kagent-grafana-mcp.kagent:8000` et se fait rejeter **avant tout log**. On ajoute ce Host dans `grafana-mcp.args`. (Alternative permissive, service ClusterIP interne seulement : `--allowed-hosts` `*`.)

2. **`serviceAccountToken`** (auth vers Grafana) : mcp-grafana s'authentifie a Grafana avec un **service account token** lu depuis le Secret `grafana-mcp-token` (clé `GRAFANA_SERVICE_ACCOUNT_TOKEN`), reference via `secretRef`. L'acces anonyme ne suffit pas. Voir *Setup post-deploy* pour creer le token + le Secret.

Les outils `k8s_*` et `prometheus_*` de `kagent-tool-server` sont independants et ne passent pas par Grafana.

### Setup post-deploy

```bash
# 1. Recuperer les credentials IAM
terraform -chdir=terraform output -raw kagent_access_key_id
terraform -chdir=terraform output -raw kagent_secret_access_key

# 2. Creer les secrets K8s
kubectl create namespace kagent

kubectl create secret generic aws-credentials -n kagent \
  --from-literal=AWS_ACCESS_KEY_ID=$(terraform -chdir=terraform output -raw kagent_access_key_id) \
  --from-literal=AWS_SECRET_ACCESS_KEY=$(terraform -chdir=terraform output -raw kagent_secret_access_key)

kubectl create secret generic github-token -n kagent \
  --from-literal=GITHUB_TOKEN="Bearer <GITHUB_PAT>" \
  --dry-run=client -o yaml | kubectl apply -f -

# Secret Grafana MCP : service account token pour l'auth du grafana-mcp server
# → creer d'abord le token dans Grafana :
#   Administration → Users and access → Service accounts → New (role Admin) → Add token
kubectl create secret generic grafana-mcp-token -n kagent \
  --from-literal=GRAFANA_SERVICE_ACCOUNT_TOKEN="glsa_xxxxxxxxxxxxxxxx" \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Verifier le deploiement
kubectl get pods -n kagent
kubectl get agents -n kagent
```

**GitHub PAT** : Token classique avec permissions `repo` + `copilot`. Le prefixe `Bearer ` dans le secret est requis par le MCP server GitHub Copilot (`api.githubcopilot.com/mcp/`).

**Grafana service account token** : cree dans Grafana (*Administration → Users and access → Service accounts*, role **Admin**). Ne pas mettre de prefixe `Bearer ` ici — le MCP server l'ajoute lui-meme. Sans ce token, le controller kagent echoue au reconcile avec `initialize: Forbidden` sur `kagent-grafana-mcp`.

## Quick Start

```bash
# Deployer l'infra + provisionner le cluster
make all

# Recuperer le kubeconfig
make kubeconfig
export KUBECONFIG=~/.kube/config:~/.kube/config-cluster69
kubectl config use-context cluster69
```

## Makefile

| Commande | Action |
|---|---|
| `make all` | Infra + cluster |
| `make infra` | `terraform apply` |
| `make cluster` | `ansible-playbook` |
| `make kubeconfig` | Fetch kubeconfig depuis SSM |
| `make argocd-password` | Mot de passe admin ArgoCD |
| `make destroy` | `terraform destroy` |

## Arborescence

```
Cluster69/
  Makefile
  apps/                          # ArgoCD Applications (App of Apps)
  agents/                        # Agent CRDs kagent (deployes via ArgoCD)
    model-config.yaml            # ModelConfig Bedrock (Claude Haiku 4.5)
    github-mcp-server.yaml       # MCPServer pour acces GitHub
    diagnostic.yaml              # Agent : diagnostic cluster
    advisor.yaml                 # Agent : recommandations
    gitops-proposer.yaml         # Agent : PRs GitHub
  terraform/                     # Infra AWS (VPC, EC2, SG, SSM, IAM Bedrock)
  ansible/
    playbook.yml                 # 6 plays : common → control_plane → worker → calico → helm → argocd
    roles/
      common/                    # OS setup, containerd, kubeadm/kubelet/kubectl
      control_plane/             # kubeadm init
      worker/                    # kubeadm join
      calico/                    # CNI Calico VXLAN + labels/taints + kubeconfig SSM
      helm/                      # Helm CLI
      argocd/                    # ArgoCD + bootstrap App of Apps
```

## Choix techniques

- **CNI Calico VXLAN** : encapsulation obligatoire sur AWS (source/dest check sur les ENI)
- **kubeadm** (vs EKS) : approche educative, controle total sur le control plane
- **ArgoCD App of Apps** : tout deploiement = un fichier YAML dans `apps/`, sync automatique
- **Monitoring sur node dedie** : taint `NoSchedule` pour isoler les workloads monitoring (budget RAM 4 GB)
- **Ingress path-based** : un seul point d'entree (port 80 du node ingress) pour toutes les apps
- **kagent + Bedrock** : agents IA autonomes avec acces cluster + repo, Claude Haiku 4.5 en pay-per-token

## CI/CD

**GitHub Actions** : 4 jobs paralleles sur chaque push/PR — yamllint, terraform fmt+validate, ansible-lint, kubeconform.

**Pre-commit** : memes checks en local avant chaque commit.

## Prerequis

- Terraform >= 1.14
- Ansible
- AWS CLI configure (EC2, VPC, SSM, S3, IAM)
- Cle SSH `~/.ssh/id_ed25519`
- Bucket S3 `logs69` existant

🚀 ✨
