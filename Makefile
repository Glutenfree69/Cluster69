AWS_REGION ?= eu-west-3
KUBECONFIG_PATH = $(HOME)/.kube/config-kubequest
SSM_PARAM = /kubequest/kubeconfig

.PHONY: infra cluster kubeconfig all destroy

all: infra cluster ## Deploy infra + provision cluster

infra: ## Deploy AWS infrastructure (generates Ansible inventory)
	cd terraform && terraform init -upgrade && terraform apply -auto-approve

cluster: ## Provision Kubernetes cluster via Ansible
	cd ansible && ansible-playbook playbook.yml

kubeconfig: ## Fetch kubeconfig from SSM to ~/.kube/config-kubequest
	@mkdir -p $(HOME)/.kube
	@aws ssm get-parameter \
		--name "$(SSM_PARAM)" \
		--with-decryption \
		--query 'Parameter.Value' \
		--output text \
		--region $(AWS_REGION) \
		> $(KUBECONFIG_PATH)
	@chmod 600 $(KUBECONFIG_PATH)
	@echo "Kubeconfig saved to $(KUBECONFIG_PATH)"
	@echo ""
	@echo "To use it:"
	@echo "  export KUBECONFIG=~/.kube/config:$(KUBECONFIG_PATH)"
	@echo "  kubectl config use-context kubequest"

argocd-password: ## Get ArgoCD admin password
	@kubectl --kubeconfig $(KUBECONFIG_PATH) get secret argocd-initial-admin-secret \
		-n argocd -o jsonpath='{.data.password}' | base64 -d && echo

destroy: ## Destroy all AWS infrastructure
	cd terraform && terraform destroy

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
