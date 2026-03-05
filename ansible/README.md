# KubeQuest - Cluster Kubernetes avec kubeadm

Provisioning automatise d'un cluster Kubernetes from scratch avec kubeadm sur 4 VMs AWS.

## Quick Start

```bash
# Admin : deployer l'infra + provisionner le cluster
make all

# Tout le monde : recuperer le kubeconfig depuis SSM
make kubeconfig
export KUBECONFIG=~/.kube/config:~/.kube/config-kubequest
kubectl config use-context kubequest
kubectl get nodes
k9s
```

## Architecture

```
kube-1      : Control Plane (etcd, API server, scheduler, controller-manager) + worker
kube-2      : Worker
ingress     : Worker (label: node-role.kubernetes.io/ingress)
monitoring  : Worker (label: node-role.kubernetes.io/monitoring)
```

## kubeadm vs EKS : ce qu'EKS fait pour toi

| Composant | EKS (manage) | kubeadm (ici) |
|---|---|---|
| etcd | Gere par AWS, multi-AZ | Static pod sur kube-1, single node |
| API Server | Endpoint manage, HA | Static pod, on gere les certs |
| Scheduler / Controller Manager | Invisible, gere par AWS | Static pods qu'on peut inspecter |
| Kubelet | AMI optimisee, pre-configure | On installe et configure nous-memes |
| CNI | VPC CNI (integre aux ENI AWS) | Calico (overlay VXLAN) |
| Certificats | Geres automatiquement | kubeadm genere, rotation manuelle |
| Upgrades | `eksctl upgrade` | `kubeadm upgrade` noeud par noeud |
| Node groups | Managed/Fargate | On join manuellement chaque noeud |

## Prerequis systeme : pourquoi chaque etape

### Kernel modules (overlay, br_netfilter)
- **overlay** : necessaire pour le stockage overlay de containerd (couches d'images)
- **br_netfilter** : permet a iptables de voir le trafic qui traverse les bridges Linux (requis pour le routage des Services Kubernetes)

### Sysctl (bridge-nf-call-iptables, ip_forward)
- **bridge-nf-call-iptables** : les paquets traversant un bridge passent par iptables (kube-proxy en a besoin pour les ClusterIP/NodePort)
- **ip_forward** : permet au noeud de router des paquets entre interfaces (le noeud agit comme routeur pour les pods)

### Swap off
- kubelet refuse de demarrer si swap est active (par defaut)
- Kubernetes gere lui-meme la memoire via les requests/limits — le swap fausse les garanties de QoS

### containerd avec SystemdCgroup
- containerd est le CRI (Container Runtime Interface) utilise par kubelet
- `SystemdCgroup = true` : aligne la gestion des cgroups entre containerd et systemd (evite les conflits de ressources)

## Composants Control Plane (static pods)

Apres `kubeadm init`, les composants tournent comme **static pods** dans `/etc/kubernetes/manifests/` :

- **etcd** : base de donnees cle-valeur distribuee, stocke tout l'etat du cluster (pods, services, secrets, configmaps...)
- **kube-apiserver** : point d'entree unique du cluster, valide et persiste les objets dans etcd, sert l'API REST
- **kube-scheduler** : surveille les pods non assignes et choisit le noeud optimal (resources, affinites, taints)
- **kube-controller-manager** : boucles de controle (Deployment controller, ReplicaSet controller, Node controller...)

## Le processus `kubeadm init`

1. **Pre-flight checks** : verifie les prerequis (swap off, ports libres, cgroup driver...)
2. **Generation des certificats** : CA du cluster, certs pour API server, kubelet, etcd (dans `/etc/kubernetes/pki/`)
3. **Generation des kubeconfigs** : admin.conf, kubelet.conf, controller-manager.conf, scheduler.conf
4. **Static pod manifests** : ecrit les YAML dans `/etc/kubernetes/manifests/`, kubelet les demarre automatiquement
5. **Bootstrap token** : cree un token temporaire pour que les workers puissent rejoindre le cluster
6. **Addons** : installe CoreDNS et kube-proxy comme Deployments/DaemonSets

## Le processus `kubeadm join`

1. **Discovery** : le worker contacte l'API server avec le bootstrap token
2. **TLS Bootstrap** : le kubelet genere une CSR (Certificate Signing Request), l'API server l'approuve automatiquement
3. **Node registration** : le kubelet s'enregistre comme Node dans l'API, le scheduler peut commencer a y placer des pods

## CRI et containerd

**CRI** (Container Runtime Interface) est l'abstraction entre kubelet et le runtime de conteneurs.

- Docker n'est plus supporte directement depuis Kubernetes 1.24 (suppression de dockershim)
- containerd fait exactement ce que Docker faisait, sans la couche Docker CLI/daemon
- `crictl` est l'outil CLI pour interagir directement avec le CRI (equivalent de `docker ps` pour le debug)

## CNI et Calico

**CNI** (Container Network Interface) : plugin reseau qui donne une IP a chaque pod et permet la communication pod-to-pod.

Sans CNI, les nodes restent en `NotReady` — les pods ne peuvent pas communiquer.

**Calico** utilise un overlay VXLAN pour encapsuler le trafic pod-to-pod entre les noeuds. Chaque noeud recoit un bloc d'IPs (`/26` par defaut) depuis le CIDR `192.168.0.0/16`.

## Commandes utiles

```bash
# Cluster
kubectl get nodes -o wide           # voir tous les noeuds + IPs
kubectl get pods -A                  # tous les pods (kube-system, tigera...)
kubectl cluster-info                 # endpoints du cluster

# Debug
kubectl describe node <name>         # detail d'un noeud (conditions, capacite, pods)
kubectl logs -n kube-system <pod>    # logs d'un composant system
journalctl -u kubelet -f             # logs kubelet en temps reel (sur le noeud)
crictl ps                            # conteneurs geres par containerd
crictl logs <container-id>           # logs d'un conteneur via CRI

# Certificats
kubeadm certs check-expiration       # voir l'expiration des certs
openssl x509 -in /etc/kubernetes/pki/apiserver.crt -noout -dates

# Tokens
kubeadm token list                   # tokens actifs
kubeadm token create --print-join-command  # regenerer une commande join

# Static pods
ls /etc/kubernetes/manifests/        # manifests des composants control plane
```
