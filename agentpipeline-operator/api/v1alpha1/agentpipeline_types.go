/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PipelinePhase represents the current lifecycle phase of the pipeline.
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type PipelinePhase string

const (
	PhasePending   PipelinePhase = "Pending"
	PhaseRunning   PipelinePhase = "Running"
	PhaseCompleted PipelinePhase = "Completed"
	PhaseFailed    PipelinePhase = "Failed"
)

// StagePhase represents the current lifecycle phase of a single stage.
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Skipped
type StagePhase string

const (
	StagePending   StagePhase = "Pending"
	StageRunning   StagePhase = "Running"
	StageCompleted StagePhase = "Completed"
	StageFailed    StagePhase = "Failed"
	StageSkipped   StagePhase = "Skipped"
)

// TriggerType defines how the pipeline is initiated.
// +kubebuilder:validation:Enum=manual;alertmanager;event
type TriggerType string

const (
	TriggerManual       TriggerType = "manual"
	TriggerAlertmanager TriggerType = "alertmanager"
	TriggerEvent        TriggerType = "event"
)

// TriggerSpec defines how the pipeline is triggered.
type TriggerSpec struct {
	// type specifies the trigger mechanism. Only "manual" is supported in v1alpha1.
	// +kubebuilder:default=manual
	// +optional
	Type TriggerType `json:"type,omitempty"`

	// alertmanager contains configuration for the Alertmanager webhook trigger.
	// Only used when type is "alertmanager".
	// +optional
	Alertmanager *AlertmanagerConfig `json:"alertmanager,omitempty"`
}

// AlertmanagerConfig holds settings for the Alertmanager webhook trigger (Phase 2).
type AlertmanagerConfig struct {
	// webhookPort is the port the webhook server listens on.
	// +kubebuilder:default=8080
	// +optional
	WebhookPort int32 `json:"webhookPort,omitempty"`

	// matchLabels filters incoming alerts by label selectors.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// AgentReference points to a kagent Agent CR by name and namespace.
type AgentReference struct {
	// name is the name of the kagent Agent CR.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// namespace is the namespace of the kagent Agent CR.
	// Defaults to the pipeline's namespace if not specified.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// RetryPolicy defines retry behavior for failed stages.
type RetryPolicy struct {
	// maxRetries is the maximum number of retry attempts.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// backoff is the delay between retry attempts (e.g. "30s", "1m").
	// +kubebuilder:default="30s"
	// +optional
	Backoff *metav1.Duration `json:"backoff,omitempty"`
}

// StageConfig holds optional per-stage configuration.
type StageConfig struct {
	// autoMerge indicates whether gitops PRs should be auto-merged.
	// +optional
	AutoMerge string `json:"autoMerge,omitempty"`

	// targetRepo is the GitHub repository for gitops operations.
	// +optional
	TargetRepo string `json:"targetRepo,omitempty"`
}

// StageSpec defines a single stage in the pipeline.
type StageSpec struct {
	// name is the unique identifier for this stage within the pipeline.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// agentRef references the kagent Agent CR to invoke for this stage.
	AgentRef AgentReference `json:"agentRef"`

	// timeout is the maximum duration to wait for the agent to complete.
	// +kubebuilder:default="5m"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// dependsOn lists stage names that must complete before this stage can start.
	// If empty, the stage depends on the previous stage in the list (implicit ordering).
	// +optional
	DependsOn []string `json:"dependsOn,omitempty"`

	// prompt is a Go template string sent to the agent. Supports {{.PreviousOutput}},
	// {{.StageOutput "name"}}, and {{.Inputs}} for inter-stage data passing.
	// +optional
	Prompt string `json:"prompt,omitempty"`

	// inputs provides static key-value pairs available in prompt templates via {{.Inputs}}.
	// +optional
	Inputs map[string]string `json:"inputs,omitempty"`

	// config holds optional stage-specific configuration (e.g. autoMerge, targetRepo).
	// +optional
	Config *StageConfig `json:"config,omitempty"`

	// retryPolicy overrides the pipeline-level retry policy for this stage.
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

// AgentPipelineSpec defines the desired state of AgentPipeline.
type AgentPipelineSpec struct {
	// trigger defines how the pipeline is initiated.
	// +optional
	Trigger TriggerSpec `json:"trigger,omitempty"`

	// stages is the ordered list of agent stages to execute.
	// +kubebuilder:validation:MinItems=1
	Stages []StageSpec `json:"stages"`

	// retryPolicy defines the default retry behavior for failed stages.
	// Individual stages can override this.
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

// StageStatus captures the observed state of a single stage execution.
type StageStatus struct {
	// name is the stage identifier matching StageSpec.Name.
	Name string `json:"name"`

	// phase is the current lifecycle phase of this stage.
	Phase StagePhase `json:"phase"`

	// startedAt is the timestamp when this stage began execution.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// completedAt is the timestamp when this stage finished (success or failure).
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// retryCount is the number of retries attempted so far.
	// +optional
	RetryCount int32 `json:"retryCount,omitempty"`

	// output is the truncated agent response (last 1024 characters).
	// Full output is stored in the ConfigMap referenced by outputRef.
	// +optional
	Output string `json:"output,omitempty"`

	// outputRef is the name of the ConfigMap containing the full stage output.
	// +optional
	OutputRef string `json:"outputRef,omitempty"`

	// error contains the error message if the stage failed.
	// +optional
	Error string `json:"error,omitempty"`

	// taskID is the kagent A2A task identifier for tracking.
	// +optional
	TaskID string `json:"taskID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Stage",type=string,JSONPath=`.status.currentStage`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AgentPipeline orchestrates kagent AI agents through sequential stages.
// It watches the pipeline CR, invokes agents via the A2A protocol,
// and passes outputs between stages using ConfigMaps.
type AgentPipeline struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired pipeline configuration.
	// +required
	Spec AgentPipelineSpec `json:"spec"`

	// status reflects the observed state of the pipeline execution.
	// +optional
	Status AgentPipelineStatus `json:"status,omitzero"`
}

// AgentPipelineStatus defines the observed state of AgentPipeline.
type AgentPipelineStatus struct {
	// phase is the overall pipeline lifecycle phase.
	// +optional
	Phase PipelinePhase `json:"phase,omitempty"`

	// currentStage is the name of the stage currently being executed.
	// +optional
	CurrentStage string `json:"currentStage,omitempty"`

	// startedAt is the timestamp when the pipeline began execution.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// completedAt is the timestamp when the pipeline finished.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// stages contains the status of each stage in the pipeline.
	// +optional
	Stages []StageStatus `json:"stages,omitempty"`

	// conditions represent the latest available observations of the pipeline's state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// AgentPipelineList contains a list of AgentPipeline.
type AgentPipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []AgentPipeline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentPipeline{}, &AgentPipelineList{})
}
