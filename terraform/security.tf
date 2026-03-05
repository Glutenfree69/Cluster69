# --- SSH Key Pair ---

resource "aws_key_pair" "deployer" {
  key_name   = "${var.project_name}-key"
  public_key = file(var.ssh_public_key_path)
}

# --- Security Group: k8s_nodes (kube-1, kube-2) ---

resource "aws_security_group" "k8s_nodes" {
  name        = "${var.project_name}-k8s-nodes"
  description = "Security group for internal Kubernetes nodes (control plane + workers)"
  vpc_id      = aws_vpc.main.id

  # SSH from admin IP only
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }

  # Kubernetes API server (from VPC)
  ingress {
    description = "Kubernetes API server"
    from_port   = 6443
    to_port     = 6443
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  # Kubernetes API server (from admin — for k9s/kubectl access)
  ingress {
    description = "Kubernetes API server (admin)"
    from_port   = 6443
    to_port     = 6443
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }

  # etcd
  ingress {
    description = "etcd"
    from_port   = 2379
    to_port     = 2380
    protocol    = "tcp"
    self        = true
  }

  # kubelet API
  ingress {
    description = "kubelet API"
    from_port   = 10250
    to_port     = 10250
    protocol    = "tcp"
    self        = true
  }

  # NodePort range
  ingress {
    description = "NodePort services"
    from_port   = 30000
    to_port     = 32767
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # VXLAN overlay (Calico/Flannel)
  ingress {
    description = "VXLAN overlay"
    from_port   = 8472
    to_port     = 8472
    protocol    = "udp"
    self        = true
  }

  # All intra-VPC traffic (covers cross-SG communication between k8s_nodes and public_nodes)
  ingress {
    description = "Intra-VPC traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [var.vpc_cidr]
  }

  # Pod network CIDR
  ingress {
    description = "Pod network"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [var.pod_network_cidr]
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name                                        = "${var.project_name}-k8s-nodes"
    "kubernetes.io/cluster/${var.project_name}" = "owned"
  }
}

# --- Security Group: public_nodes (ingress, monitoring) ---

resource "aws_security_group" "public_nodes" {
  name        = "${var.project_name}-public-nodes"
  description = "Security group for public-facing nodes (ingress, monitoring)"
  vpc_id      = aws_vpc.main.id

  # SSH from admin IP only
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }

  # HTTP
  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTPS
  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # NodePort range
  ingress {
    description = "NodePort services"
    from_port   = 30000
    to_port     = 32767
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Grafana (admin only)
  ingress {
    description = "Grafana"
    from_port   = 3000
    to_port     = 3000
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }

  # Prometheus (admin only)
  ingress {
    description = "Prometheus"
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }

  # Kubernetes API server (from VPC)
  ingress {
    description = "Kubernetes API server"
    from_port   = 6443
    to_port     = 6443
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  # All intra-VPC traffic (covers cross-SG communication between k8s_nodes and public_nodes)
  ingress {
    description = "Intra-VPC traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [var.vpc_cidr]
  }

  # Pod network CIDR
  ingress {
    description = "Pod network"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [var.pod_network_cidr]
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name                                        = "${var.project_name}-public-nodes"
    "kubernetes.io/cluster/${var.project_name}" = "owned"
  }
}
