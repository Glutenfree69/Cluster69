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

package controller

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aiopsv1alpha1 "github.com/Glutenfree69/agentpipeline-operator/api/v1alpha1"
)

const (
	// maxStatusOutputLen is the maximum number of characters stored in status.stages[].output.
	maxStatusOutputLen = 1024

	// configMapDataKey is the key used to store stage output in ConfigMaps.
	configMapDataKey = "output"
)

// PipelineContext is the data object passed to Go templates when rendering prompts.
type PipelineContext struct {
	// PreviousOutput contains the full output of the immediately preceding stage.
	PreviousOutput string

	// Inputs contains the static key-value pairs defined in the stage spec.
	Inputs map[string]string

	// stageOutputs maps stage names to their full output text (for StageOutput).
	stageOutputs map[string]string
}

// StageOutput returns the full output of a named stage.
// Usage in templates: {{.StageOutput "diagnostic"}}
func (pc PipelineContext) StageOutput(stageName string) string {
	if pc.stageOutputs == nil {
		return ""
	}
	return pc.stageOutputs[stageName]
}

// StageHandler encapsulates stage lifecycle operations: prompt rendering,
// output storage, and stage status management.
type StageHandler struct {
	client client.Client
}

// NewStageHandler creates a new StageHandler.
func NewStageHandler(c client.Client) *StageHandler {
	return &StageHandler{client: c}
}

// RenderPrompt renders the stage's prompt template with the pipeline context.
// If the stage has no prompt template, a default prompt is generated that
// includes the previous stage output (if any).
func (h *StageHandler) RenderPrompt(stage *aiopsv1alpha1.StageSpec, pipelineCtx *PipelineContext) (string, error) {
	promptTmpl := stage.Prompt
	if promptTmpl == "" {
		// Default: pass previous output directly if available.
		if pipelineCtx.PreviousOutput != "" {
			return pipelineCtx.PreviousOutput, nil
		}
		return fmt.Sprintf("Execute agent %s", stage.AgentRef.Name), nil
	}

	tmpl, err := template.New("prompt").Parse(promptTmpl)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template for stage %q: %w", stage.Name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, pipelineCtx); err != nil {
		return "", fmt.Errorf("executing prompt template for stage %q: %w", stage.Name, err)
	}

	return buf.String(), nil
}

// BuildPipelineContext constructs the template context by reading outputs from
// completed stages' ConfigMaps.
func (h *StageHandler) BuildPipelineContext(
	ctx context.Context,
	pipeline *aiopsv1alpha1.AgentPipeline,
	currentStage *aiopsv1alpha1.StageSpec,
) (*PipelineContext, error) {
	pc := &PipelineContext{
		Inputs:       currentStage.Inputs,
		stageOutputs: make(map[string]string),
	}

	// Collect outputs from all completed stages.
	for i, stageStatus := range pipeline.Status.Stages {
		if stageStatus.Phase != aiopsv1alpha1.StageCompleted || stageStatus.OutputRef == "" {
			continue
		}

		output, err := h.readConfigMapOutput(ctx, pipeline.Namespace, stageStatus.OutputRef)
		if err != nil {
			return nil, fmt.Errorf("reading output for stage %q: %w", stageStatus.Name, err)
		}

		pc.stageOutputs[stageStatus.Name] = output

		// The previous stage is the one just before the current stage in the spec.
		if i < len(pipeline.Spec.Stages)-1 && pipeline.Spec.Stages[i+1].Name == currentStage.Name {
			pc.PreviousOutput = output
		}
	}

	return pc, nil
}

// StoreOutput saves the agent output to a ConfigMap and returns the truncated
// output for the status field.
func (h *StageHandler) StoreOutput(
	ctx context.Context,
	pipeline *aiopsv1alpha1.AgentPipeline,
	stageName string,
	output string,
) (configMapName string, truncatedOutput string, err error) {
	configMapName = outputConfigMapName(pipeline.Name, stageName)
	truncatedOutput = truncateOutput(output)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: pipeline.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentpipeline-operator",
				"aiops.cluster69.io/pipeline":  pipeline.Name,
				"aiops.cluster69.io/stage":     stageName,
			},
		},
		Data: map[string]string{
			configMapDataKey: output,
		},
	}

	// Set OwnerReference so the ConfigMap is GC'd with the pipeline.
	if err := controllerutil.SetControllerReference(pipeline, cm, h.client.Scheme()); err != nil {
		return "", "", fmt.Errorf("setting owner reference on ConfigMap %q: %w", configMapName, err)
	}

	// CreateOrUpdate to handle re-runs (retries).
	_, err = controllerutil.CreateOrUpdate(ctx, h.client, cm, func() error {
		cm.Data = map[string]string{
			configMapDataKey: output,
		}
		return nil
	})
	if err != nil {
		return "", "", fmt.Errorf("creating/updating ConfigMap %q: %w", configMapName, err)
	}

	return configMapName, truncatedOutput, nil
}

// FindCurrentStage returns the first stage that is not yet completed or skipped.
func FindCurrentStage(pipeline *aiopsv1alpha1.AgentPipeline) (*aiopsv1alpha1.StageSpec, *aiopsv1alpha1.StageStatus) {
	for i, stageStatus := range pipeline.Status.Stages {
		if stageStatus.Phase != aiopsv1alpha1.StageCompleted && stageStatus.Phase != aiopsv1alpha1.StageSkipped {
			return &pipeline.Spec.Stages[i], &pipeline.Status.Stages[i]
		}
	}
	return nil, nil
}

// AreDependenciesMet checks that all stages listed in dependsOn are completed.
func AreDependenciesMet(pipeline *aiopsv1alpha1.AgentPipeline, stage *aiopsv1alpha1.StageSpec) bool {
	if len(stage.DependsOn) == 0 {
		return true
	}

	completedStages := make(map[string]bool)
	for _, ss := range pipeline.Status.Stages {
		if ss.Phase == aiopsv1alpha1.StageCompleted {
			completedStages[ss.Name] = true
		}
	}

	for _, dep := range stage.DependsOn {
		if !completedStages[dep] {
			return false
		}
	}
	return true
}

// InitStageStatuses initializes the status.stages slice from the spec.
func InitStageStatuses(pipeline *aiopsv1alpha1.AgentPipeline) {
	pipeline.Status.Stages = make([]aiopsv1alpha1.StageStatus, len(pipeline.Spec.Stages))
	for i, stage := range pipeline.Spec.Stages {
		pipeline.Status.Stages[i] = aiopsv1alpha1.StageStatus{
			Name:  stage.Name,
			Phase: aiopsv1alpha1.StagePending,
		}
	}
}

// GetEffectiveRetryPolicy returns the retry policy for a stage, falling back
// to the pipeline-level policy if the stage doesn't define one.
func GetEffectiveRetryPolicy(pipeline *aiopsv1alpha1.AgentPipeline, stage *aiopsv1alpha1.StageSpec) *aiopsv1alpha1.RetryPolicy {
	if stage.RetryPolicy != nil {
		return stage.RetryPolicy
	}
	if pipeline.Spec.RetryPolicy != nil {
		return pipeline.Spec.RetryPolicy
	}
	return &aiopsv1alpha1.RetryPolicy{MaxRetries: 0}
}

// readConfigMapOutput reads the output data from a stage output ConfigMap.
func (h *StageHandler) readConfigMapOutput(ctx context.Context, namespace, name string) (string, error) {
	cm := &corev1.ConfigMap{}
	if err := h.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cm); err != nil {
		return "", err
	}
	return cm.Data[configMapDataKey], nil
}

// outputConfigMapName generates a deterministic ConfigMap name for a stage.
func outputConfigMapName(pipelineName, stageName string) string {
	return fmt.Sprintf("%s-stage-%s", pipelineName, stageName)
}

// truncateOutput returns the last maxStatusOutputLen characters of the output.
func truncateOutput(output string) string {
	if len(output) <= maxStatusOutputLen {
		return output
	}
	return "..." + output[len(output)-maxStatusOutputLen+3:]
}
