# --- Public IPs ---

output "kube_1_public_ip" {
  description = "Public IP of kube-1 (control plane)"
  value       = aws_instance.kube_1.public_ip
}

output "kube_2_public_ip" {
  description = "Public IP of kube-2 (worker)"
  value       = aws_instance.kube_2.public_ip
}

output "ingress_public_ip" {
  description = "Public IP of ingress node"
  value       = aws_instance.ingress.public_ip
}

output "monitoring_public_ip" {
  description = "Public IP of monitoring node"
  value       = aws_instance.monitoring.public_ip
}

# --- Private IPs ---

output "kube_1_private_ip" {
  description = "Private IP of kube-1 (control plane)"
  value       = aws_instance.kube_1.private_ip
}

output "kube_2_private_ip" {
  description = "Private IP of kube-2 (worker)"
  value       = aws_instance.kube_2.private_ip
}

output "ingress_private_ip" {
  description = "Private IP of ingress node"
  value       = aws_instance.ingress.private_ip
}

output "monitoring_private_ip" {
  description = "Private IP of monitoring node"
  value       = aws_instance.monitoring.private_ip
}

# --- AMI ---

output "ami_id" {
  description = "Ubuntu AMI ID used for all instances"
  value       = data.aws_ami.ubuntu.id
}

output "ami_name" {
  description = "Ubuntu AMI name"
  value       = data.aws_ami.ubuntu.name
}

# --- SSH Commands ---

output "ssh_commands" {
  description = "SSH connection commands for all nodes"
  value = {
    kube_1     = "ssh ubuntu@${aws_instance.kube_1.public_ip}"
    kube_2     = "ssh ubuntu@${aws_instance.kube_2.public_ip}"
    ingress    = "ssh ubuntu@${aws_instance.ingress.public_ip}"
    monitoring = "ssh ubuntu@${aws_instance.monitoring.public_ip}"
  }
}

# --- Kubeadm ---

output "kubeadm_init_endpoint" {
  description = "Private IP to use for kubeadm init --apiserver-advertise-address"
  value       = aws_instance.kube_1.private_ip
}

# --- Ansible Inventory ---

resource "local_file" "ansible_inventory" {
  filename        = "${path.module}/../ansible/inventory/hosts.ini"
  file_permission = "0644"
  content         = <<-INI
    [control_plane]
    kube-1 ansible_host=${aws_instance.kube_1.public_ip} private_ip=${aws_instance.kube_1.private_ip} public_ip=${aws_instance.kube_1.public_ip}

    [workers]
    kube-2     ansible_host=${aws_instance.kube_2.public_ip}      private_ip=${aws_instance.kube_2.private_ip}      public_ip=${aws_instance.kube_2.public_ip}
    ingress    ansible_host=${aws_instance.ingress.public_ip}    private_ip=${aws_instance.ingress.private_ip}    public_ip=${aws_instance.ingress.public_ip}
    monitoring ansible_host=${aws_instance.monitoring.public_ip} private_ip=${aws_instance.monitoring.private_ip} public_ip=${aws_instance.monitoring.public_ip}

    [k8s_cluster:children]
    control_plane
    workers
  INI
}
