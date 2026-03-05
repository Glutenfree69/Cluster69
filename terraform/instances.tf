# --- kube-1: Control Plane + Worker ---

resource "aws_instance" "kube_1" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.control_plane_instance_type
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.k8s_nodes.id]

  credit_specification {
    cpu_credits = "standard"
  }

  root_block_device {
    volume_size = var.root_volume_size_gb
    volume_type = var.root_volume_type
  }

  metadata_options {
    http_endpoint          = "enabled"
    http_tokens            = "required"
    instance_metadata_tags = "enabled"
  }

  tags = {
    Name = "${var.project_name}-kube-1"
    Role = "control-plane"
  }
}

# --- kube-2: Worker ---

resource "aws_instance" "kube_2" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.worker_instance_type
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.k8s_nodes.id]

  credit_specification {
    cpu_credits = "standard"
  }

  root_block_device {
    volume_size = var.root_volume_size_gb
    volume_type = var.root_volume_type
  }

  metadata_options {
    http_endpoint          = "enabled"
    http_tokens            = "required"
    instance_metadata_tags = "enabled"
  }

  tags = {
    Name = "${var.project_name}-kube-2"
    Role = "worker"
  }
}

# --- ingress: Ingress Controller ---

resource "aws_instance" "ingress" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.ingress_instance_type
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.public_nodes.id]

  credit_specification {
    cpu_credits = "standard"
  }

  root_block_device {
    volume_size = var.root_volume_size_gb
    volume_type = var.root_volume_type
  }

  metadata_options {
    http_endpoint          = "enabled"
    http_tokens            = "required"
    instance_metadata_tags = "enabled"
  }

  tags = {
    Name = "${var.project_name}-ingress"
    Role = "ingress"
  }
}

# --- monitoring: Prometheus/Grafana/Loki ---

resource "aws_instance" "monitoring" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.monitoring_instance_type
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.public_nodes.id]

  credit_specification {
    cpu_credits = "standard"
  }

  root_block_device {
    volume_size = 30
    volume_type = var.root_volume_type
  }

  metadata_options {
    http_endpoint          = "enabled"
    http_tokens            = "required"
    instance_metadata_tags = "enabled"
  }

  tags = {
    Name = "${var.project_name}-monitoring"
    Role = "monitoring"
  }
}
