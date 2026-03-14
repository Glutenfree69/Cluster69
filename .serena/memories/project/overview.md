# KubeQuest - Project Overview

## Purpose
Cluster Kubernetes self-managed (kubeadm) sur AWS. Workflow GitOps complet avec ArgoCD App of Apps. Provisioning full automatisé via `make all` (Terraform + Ansible).

## Project Structure
- `terraform/` — Infrastructure AWS (VPC, EC2, SSM, Security Groups)
- `ansible/` — Provisioning cluster kubeadm (6 roles: common, control_plane, worker, calico, helm, argocd)
- `apps/` — Applications ArgoCD (App of Apps pattern)
  - `root.yaml` — App of Apps root, surveille ce dossier
  - `ingress-nginx.yaml` — Nginx Ingress Controller (Helm chart)
- `.github/workflows/lint.yml` — CI lint/validation (yamllint, terraform, ansible-lint, kubeconform)
- `.pre-commit-config.yaml` — Pre-commit hooks (mêmes checks qu'en CI, en local)
- `.yamllint.yml` — Config yamllint

## Infrastructure (terraform/)
4 VMs EC2 dans eu-west-3 (Paris) :

| Node | Rôle | Type | Stockage | SG |
|---|---|---|---|---|
| kube-1 | Control plane + worker | t3.medium | 20 GB gp3 | k8s_nodes |
| kube-2 | Worker | t3.small | 20 GB gp3 | k8s_nodes |
| ingress | Ingress controller (Nginx) | t3.small | 20 GB gp3 | public_nodes |
| monitoring | Prometheus/Grafana/Loki | t3.medium | 30 GB gp3 | public_nodes |

Coût estimé : ~$97/mois

## Stack

| Composant | Outil |
|---|---|
| Infra | Terraform (backend S3 bucket `logs69`) |
| Provisioning | Ansible (kubeadm + containerd) |
| K8s distro | kubeadm v1.32 |
| CNI | Calico (VXLAN always — obligatoire sur AWS, source/dest check) |
| Ingress | Nginx Ingress Controller (DaemonSet, hostNetwork, via ArgoCD) |
| GitOps | ArgoCD (App of Apps, bootstrappé automatiquement par Ansible) |
| CI | GitHub Actions (lint/validation uniquement, pas de CD) |
| Pre-commit | yamllint, terraform fmt/validate, ansible-lint, detect-private-key |

## Réseau — 3 plans
1. **Nodes** : 10.10.1.0/24 (VPC subnet, interface ens5)
2. **Pods** : 192.168.0.0/16 (overlay Calico VXLAN, /26 par node)
3. **Services** : 10.96.0.0/12 (virtuel, iptables kube-proxy)

VXLAN obligatoire sur AWS car les instances droppent les paquets avec IP source != IP instance (source/dest check). IPIP impossible car les SG AWS ne supportent que TCP/UDP/ICMP (pas protocole IP 4).

## Key Decisions
- VXLAN always (pas VXLANCrossSubnet) — tous les nodes dans le même subnet
- Pas de collections Ansible externes (community.general, ansible.posix remplacées par ansible.builtin)
- ArgoCD sur le control plane (tolerations + nodeSelector), Redis inclus
- Nginx Ingress en hostNetwork (pas de LoadBalancer/NodePort)
- App of Apps root.yaml : fichier unique dans apps/, référencé par Ansible via playbook_dir (pas de copie)
- CD géré par ArgoCD (git push → sync auto), CI par GitHub Actions (lint uniquement)

## Flux
```
make all → terraform apply + ansible-playbook
  → Infra AWS créée
  → Cluster kubeadm initialisé
  → Calico VXLAN configuré
  → Helm + ArgoCD installés
  → App of Apps bootstrappée (kubectl apply root.yaml)
  → ArgoCD sync ingress-nginx automatiquement
```
