# KubeQuest - Infrastructure AWS

## Vue d'ensemble

Terraform déploie l'infrastructure AWS nécessaire pour héberger un cluster Kubernetes self-managed (kubeadm) destiné à une application Laravel PHP bien degeulasse.

## Architecture

| Node | Rôle | Type | RAM | Stockage |
|---|---|---|---|---|
| kube-1 | Control plane + worker | t3.medium | 4 GB | 20 GB gp3 |
| kube-2 | Worker | t3.small | 2 GB | 20 GB gp3 |
| ingress | Ingress controller (Traefik) | t3.small | 2 GB | 20 GB gp3 |
| monitoring | Prometheus / Grafana / Loki | t3.medium | 4 GB | 30 GB gp3 |

**Coût estimé : ~$97/mois**

## Réseau

- 1 VPC custom (`10.10.0.0/16`) avec 1 subnet public dans `eu-west-3a`
- Internet Gateway pour l'accès internet des instances
- Toutes les instances ont une IP publique (pas de NAT Gateway c'est trop chère)
- 2 Security Groups :
  - **k8s_nodes** (kube-1, kube-2) : SSH restreint à l'admin, ports k8s internes
  - **public_nodes** (ingress, monitoring) : SSH restreint, HTTP/HTTPS ouvert, Grafana/Prometheus restreints à l'admin

## Utilisation

```bash
cd terraform/
# Renseigner my_ip_cidr avec votre IP publique (curl -4 -s ifconfig.me)
terraform init
terraform plan
terraform apply
terraform output   # IPs et commandes SSH
```

## Fichiers

| Fichier | Contenu |
|---|---|
| `main.tf` | Provider AWS, backend S3, VPC, subnet, IGW, routes |
| `variables.tf` | Variables avec valeurs par défaut |
| `security.tf` | Key pair SSH, Security Groups, règles cross-SG |
| `instances.tf` | 4 instances EC2 avec user_data (kernel modules, sysctl, swap) |
| `outputs.tf` | IPs, AMI, commandes SSH |
| `terraform.tfvars.example` | Template de configuration |
