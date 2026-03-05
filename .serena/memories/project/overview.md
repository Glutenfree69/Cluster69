# KubeQuest - Project Overview

## Purpose
Cluster Kubernetes self-managed (kubeadm) sur AWS pour héberger une app Laravel PHP (dans `php_dogshit/`) avec MySQL et Traefik. Workflow GitOps complet avec Helm + ArgoCD.

## Project Structure
- `php_dogshit/` — Application Laravel PHP (compteur API)
- `terraform/` — Infrastructure AWS (IaC)
- `ansible/` — Provisioning cluster kubeadm (à créer)
- `helm/` — Charts Helm applicatifs (à créer)
- `argocd/` — App of Apps manifests (à créer)
- `.github/workflows/` — CI/CD GitHub Actions (à créer)

## Infrastructure (terraform/)
4 VMs EC2 dans eu-west-3 (Paris) :

| Node | Rôle | Type | Stockage |
|---|---|---|---|
| kube-1 | Control plane + worker | t3.medium | 20 GB gp3 |
| kube-2 | Worker | t3.small | 20 GB gp3 |
| ingress | Ingress controller (Traefik) | t3.small | 20 GB gp3 |
| monitoring | Prometheus/Grafana/Loki | t3.medium | 30 GB gp3 |

Coût estimé : ~$97/mois

## Stack GitOps choisie

| Composant | Outil |
|---|---|
| Infra | Terraform (existant) |
| Provisioning | Ansible (kubeadm + containerd + bootstrap) |
| K8s distro | kubeadm (upstream) |
| CNI | Calico |
| Ingress | Traefik (Helm chart officiel) |
| GitOps | ArgoCD (pattern App of Apps) |
| Monitoring | kube-prometheus-stack (Helm) |
| App | Helm chart custom |
| Registry | GHCR (GitHub Container Registry) |
| CI/CD | GitHub Actions → commit tag dans values.yaml → ArgoCD sync |
| Repo | Monorepo |

## Flux destroy/recreate
```
terraform apply → ansible-playbook → ArgoCD app-of-apps → cluster opérationnel
terraform destroy → tout supprimé, 0€
```

## Terraform Files
- `provider.tf` — Provider AWS
- `terraform.tf` — Backend S3 (bucket `logs69`)
- `network.tf` — VPC, subnet, IGW, routes
- `variables.tf` — Variables avec defaults (region eu-west-3, SSH key ed25519)
- `security.tf` — Key pair SSH, 2 SGs (k8s_nodes + public_nodes), cross-SG rules
- `instances.tf` — 4 EC2 instances avec user_data (kernel modules, sysctl, swap off)
- `outputs.tf` — IPs publiques/privées, AMI, commandes SSH, endpoint kubeadm
- `locals.tf` — User data script (kernel modules, sysctl, swap)
- `data.tf` — AMI Ubuntu 22.04 lookup

## Key Decisions
- VPC custom (10.10.0.0/16) avec 1 subnet public, 1 seul AZ
- Pas de NAT Gateway (trop cher), toutes les instances ont une IP publique
- Pas d'Elastic IP (IPs changent au stop/start, OK pour projet école)
- gp3 (20% moins cher que gp2)
- CPU credits "standard" (éviter surprises facturation)
- 2 Security Groups : k8s_nodes (interne) et public_nodes (DMZ)
- SSH restreint à my_ip_cidr (variable requise, IPv4 uniquement)
- State Terraform stocké sur S3 bucket "logs69"
- AWS EC2 n'accepte PAS les `/` dans les clés de tags (ex: kubernetes.io/... interdit)
- Pod network CIDR : 192.168.0.0/16 (défaut Calico)

## User Preferences
- SSH key: ed25519 (pas RSA)
- Langue: français
- Style: direct, pas de bullshit
- Très à l'aise avec K8s/Helm/ArgoCD (fait tourner des charges prod sur EKS)
- Veut apprendre kubeadm spécifiquement (jamais utilisé)
- Veut de la documentation explicative sur le fonctionnement de kubeadm
- Étudiant, budget serré, tout doit être gratuit/pas cher
