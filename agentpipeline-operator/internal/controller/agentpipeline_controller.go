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
	"context"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	aiopsv1alpha1 "github.com/Glutenfree69/agentpipeline-operator/api/v1alpha1"
	"github.com/Glutenfree69/agentpipeline-operator/internal/runner"
)

const (
	finalizerName = "aiops.cluster69.io/pipeline-cleanup"
	requeueDelay  = 10 * time.Second
)

// AgentPipelineReconciler reconciles AgentPipeline objects.
// It orchestrates kagent agents through sequential stages, invoking each agent
// via the A2A protocol and passing outputs between stages via ConfigMaps.
type AgentPipelineReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Runner   runner.AgentRunner
	Handler  *StageHandler
}

// +kubebuilder:rbac:groups=aiops.cluster69.io,resources=agentpipelines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aiops.cluster69.io,resources=agentpipelines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aiops.cluster69.io,resources=agentpipelines/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agents,verbs=get;list;watch

// Reconcile implements the main state machine for AgentPipeline resources.
func (r *AgentPipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch the AgentPipeline CR.
	pipeline := &aiopsv1alpha1.AgentPipeline{}
	if err := r.Get(ctx, req.NamespacedName, pipeline); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	slog.Info("reconciling pipeline",
		"name", pipeline.Name,
		"namespace", pipeline.Namespace,
		"phase", pipeline.Status.Phase,
	)

	// 2. Handle deletion with finalizer.
	if !pipeline.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, pipeline)
	}

	// 3. State machine based on current phase.
	switch pipeline.Status.Phase {
	case "", aiopsv1alpha1.PhasePending:
		return r.handlePending(ctx, pipeline)
	case aiopsv1alpha1.PhaseRunning:
		return r.handleRunning(ctx, pipeline)
	case aiopsv1alpha1.PhaseCompleted, aiopsv1alpha1.PhaseFailed:
		// Terminal states — nothing to do.
		log.V(1).Info("pipeline in terminal state", "phase", pipeline.Status.Phase)
		return ctrl.Result{}, nil
	default:
		log.Error(nil, "unknown pipeline phase", "phase", pipeline.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// handleDeletion cleans up resources and removes the finalizer.
func (r *AgentPipelineReconciler) handleDeletion(ctx context.Context, pipeline *aiopsv1alpha1.AgentPipeline) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(pipeline, finalizerName) {
		return ctrl.Result{}, nil
	}

	slog.Info("cleaning up pipeline resources", "name", pipeline.Name)

	// ConfigMaps are cleaned up automatically via OwnerReferences.
	// Remove the finalizer to allow deletion to proceed.
	controllerutil.RemoveFinalizer(pipeline, finalizerName)
	if err := r.Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// handlePending transitions the pipeline from Pending to Running.
func (r *AgentPipelineReconciler) handlePending(ctx context.Context, pipeline *aiopsv1alpha1.AgentPipeline) (ctrl.Result, error) {
	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(pipeline, finalizerName) {
		controllerutil.AddFinalizer(pipeline, finalizerName)
		if err := r.Update(ctx, pipeline); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate stages.
	if len(pipeline.Spec.Stages) == 0 {
		return r.failPipeline(ctx, pipeline, "no stages defined")
	}

	// Warn if non-manual trigger is specified (not yet supported).
	if pipeline.Spec.Trigger.Type != "" && pipeline.Spec.Trigger.Type != aiopsv1alpha1.TriggerManual {
		slog.Warn("trigger type not yet supported, treating as manual",
			"type", pipeline.Spec.Trigger.Type,
			"pipeline", pipeline.Name,
		)
	}

	// Initialize status.
	now := metav1.Now()
	pipeline.Status.Phase = aiopsv1alpha1.PhaseRunning
	pipeline.Status.StartedAt = &now
	pipeline.Status.CurrentStage = pipeline.Spec.Stages[0].Name
	InitStageStatuses(pipeline)

	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "PipelineStarted",
		Message:            "Pipeline execution started",
		ObservedGeneration: pipeline.Generation,
	})

	if err := r.Status().Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status to Running: %w", err)
	}

	r.Recorder.Event(pipeline, corev1.EventTypeNormal, "PipelineStarted",
		fmt.Sprintf("Pipeline started with %d stages", len(pipeline.Spec.Stages)))

	return ctrl.Result{Requeue: true}, nil
}

// handleRunning processes the current stage of a running pipeline.
func (r *AgentPipelineReconciler) handleRunning(ctx context.Context, pipeline *aiopsv1alpha1.AgentPipeline) (ctrl.Result, error) {
	// Find the current stage to process.
	stageSpec, stageStatus := FindCurrentStage(pipeline)
	if stageSpec == nil {
		// All stages completed — mark pipeline as completed.
		return r.completePipeline(ctx, pipeline)
	}

	// Check dependencies.
	if !AreDependenciesMet(pipeline, stageSpec) {
		slog.Info("waiting for dependencies",
			"stage", stageSpec.Name,
			"dependsOn", stageSpec.DependsOn,
		)
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	switch stageStatus.Phase {
	case aiopsv1alpha1.StagePending:
		return r.startStage(ctx, pipeline, stageSpec, stageStatus)
	case aiopsv1alpha1.StageFailed:
		// Check if we should retry.
		retryPolicy := GetEffectiveRetryPolicy(pipeline, stageSpec)
		if stageStatus.RetryCount < retryPolicy.MaxRetries {
			return r.retryStage(ctx, pipeline, stageSpec, stageStatus, retryPolicy)
		}
		// No more retries — fail the pipeline.
		return r.failPipeline(ctx, pipeline,
			fmt.Sprintf("stage %q failed after %d retries: %s", stageSpec.Name, stageStatus.RetryCount, stageStatus.Error))
	default:
		slog.Warn("stage in unexpected state",
			"stage", stageSpec.Name,
			"phase", stageStatus.Phase,
		)
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}
}

// startStage invokes the kagent agent for the given stage and updates status
// in a single write to avoid ResourceVersion conflicts.
func (r *AgentPipelineReconciler) startStage(
	ctx context.Context,
	pipeline *aiopsv1alpha1.AgentPipeline,
	stageSpec *aiopsv1alpha1.StageSpec,
	stageStatus *aiopsv1alpha1.StageStatus,
) (ctrl.Result, error) {
	slog.Info("starting stage", "stage", stageSpec.Name, "agent", stageSpec.AgentRef.Name)

	now := metav1.Now()
	stageStatus.Phase = aiopsv1alpha1.StageRunning
	stageStatus.StartedAt = &now
	pipeline.Status.CurrentStage = stageSpec.Name

	r.Recorder.Event(pipeline, corev1.EventTypeNormal, "StageStarted",
		fmt.Sprintf("Stage %q started (agent: %s)", stageSpec.Name, stageSpec.AgentRef.Name))

	// Build prompt context from previous stages' outputs.
	pipelineCtx, err := r.Handler.BuildPipelineContext(ctx, pipeline, stageSpec)
	if err != nil {
		return r.failStage(ctx, pipeline, stageStatus, fmt.Sprintf("building pipeline context: %v", err))
	}

	// Render the prompt template.
	prompt, err := r.Handler.RenderPrompt(stageSpec, pipelineCtx)
	if err != nil {
		return r.failStage(ctx, pipeline, stageStatus, fmt.Sprintf("rendering prompt: %v", err))
	}

	// Resolve agent namespace.
	agentNamespace := stageSpec.AgentRef.Namespace
	if agentNamespace == "" {
		agentNamespace = pipeline.Namespace
	}

	// Resolve timeout.
	timeout := 5 * time.Minute
	if stageSpec.Timeout != nil {
		timeout = stageSpec.Timeout.Duration
	}

	// Invoke the agent via A2A (synchronous, blocks until completion).
	result, err := r.Runner.RunAgent(ctx, runner.RunRequest{
		AgentName: stageSpec.AgentRef.Name,
		Namespace: agentNamespace,
		Prompt:    prompt,
		Timeout:   timeout,
	})
	if err != nil {
		return r.failStage(ctx, pipeline, stageStatus, fmt.Sprintf("invoking agent: %v", err))
	}

	// Process the result — status is written once here (not before RunAgent).
	switch result.Status {
	case runner.RunStatusCompleted:
		return r.completeStage(ctx, pipeline, stageSpec, stageStatus, result)
	case runner.RunStatusFailed:
		return r.failStage(ctx, pipeline, stageStatus, result.Error)
	case runner.RunStatusTimedOut:
		return r.failStage(ctx, pipeline, stageStatus, "agent execution timed out")
	default:
		return r.failStage(ctx, pipeline, stageStatus, fmt.Sprintf("unexpected run status: %s", result.Status))
	}
}

// completeStage marks a stage as completed and stores its output.
func (r *AgentPipelineReconciler) completeStage(
	ctx context.Context,
	pipeline *aiopsv1alpha1.AgentPipeline,
	stageSpec *aiopsv1alpha1.StageSpec,
	stageStatus *aiopsv1alpha1.StageStatus,
	result *runner.RunResult,
) (ctrl.Result, error) {
	// Store output in ConfigMap.
	configMapName, truncatedOutput, err := r.Handler.StoreOutput(ctx, pipeline, stageSpec.Name, result.Output)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("storing output for stage %q: %w", stageSpec.Name, err)
	}

	// Update stage status.
	now := metav1.Now()
	stageStatus.Phase = aiopsv1alpha1.StageCompleted
	stageStatus.CompletedAt = &now
	stageStatus.Output = truncatedOutput
	stageStatus.OutputRef = configMapName
	stageStatus.TaskID = result.TaskID

	if err := r.Status().Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating stage %q to Completed: %w", stageSpec.Name, err)
	}

	r.Recorder.Event(pipeline, corev1.EventTypeNormal, "StageCompleted",
		fmt.Sprintf("Stage %q completed in %s", stageSpec.Name, result.Duration.Round(time.Second)))

	slog.Info("stage completed",
		"stage", stageSpec.Name,
		"duration", result.Duration.Round(time.Second),
		"outputLen", len(result.Output),
	)

	// Requeue to process the next stage.
	return ctrl.Result{Requeue: true}, nil
}

// failStage marks a stage as failed.
func (r *AgentPipelineReconciler) failStage(
	ctx context.Context,
	pipeline *aiopsv1alpha1.AgentPipeline,
	stageStatus *aiopsv1alpha1.StageStatus,
	errMsg string,
) (ctrl.Result, error) {
	now := metav1.Now()
	stageStatus.Phase = aiopsv1alpha1.StageFailed
	stageStatus.CompletedAt = &now
	stageStatus.Error = errMsg

	if err := r.Status().Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating stage %q to Failed: %w", stageStatus.Name, err)
	}

	r.Recorder.Event(pipeline, corev1.EventTypeWarning, "StageFailed",
		fmt.Sprintf("Stage %q failed: %s", stageStatus.Name, errMsg))

	slog.Error("stage failed", "stage", stageStatus.Name, "error", errMsg)

	// Requeue to let handleRunning decide whether to retry or fail the pipeline.
	return ctrl.Result{Requeue: true}, nil
}

// retryStage resets a failed stage for a retry attempt.
func (r *AgentPipelineReconciler) retryStage(
	ctx context.Context,
	pipeline *aiopsv1alpha1.AgentPipeline,
	stageSpec *aiopsv1alpha1.StageSpec,
	stageStatus *aiopsv1alpha1.StageStatus,
	retryPolicy *aiopsv1alpha1.RetryPolicy,
) (ctrl.Result, error) {
	stageStatus.RetryCount++
	stageStatus.Phase = aiopsv1alpha1.StagePending
	stageStatus.Error = ""
	stageStatus.CompletedAt = nil

	if err := r.Status().Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating stage %q for retry: %w", stageSpec.Name, err)
	}

	r.Recorder.Event(pipeline, corev1.EventTypeNormal, "StageRetry",
		fmt.Sprintf("Retrying stage %q (attempt %d/%d)", stageSpec.Name, stageStatus.RetryCount, retryPolicy.MaxRetries))

	slog.Info("retrying stage",
		"stage", stageSpec.Name,
		"attempt", stageStatus.RetryCount,
		"maxRetries", retryPolicy.MaxRetries,
	)

	// Apply backoff delay before requeue.
	backoff := 30 * time.Second
	if retryPolicy.Backoff != nil {
		backoff = retryPolicy.Backoff.Duration
	}

	return ctrl.Result{RequeueAfter: backoff}, nil
}

// completePipeline marks the pipeline as completed.
func (r *AgentPipelineReconciler) completePipeline(ctx context.Context, pipeline *aiopsv1alpha1.AgentPipeline) (ctrl.Result, error) {
	now := metav1.Now()
	pipeline.Status.Phase = aiopsv1alpha1.PhaseCompleted
	pipeline.Status.CompletedAt = &now
	pipeline.Status.CurrentStage = ""

	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "PipelineCompleted",
		Message:            "All stages completed successfully",
		ObservedGeneration: pipeline.Generation,
	})

	if err := r.Status().Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating pipeline to Completed: %w", err)
	}

	r.Recorder.Event(pipeline, corev1.EventTypeNormal, "PipelineCompleted",
		fmt.Sprintf("Pipeline completed all %d stages", len(pipeline.Spec.Stages)))

	slog.Info("pipeline completed",
		"name", pipeline.Name,
		"stages", len(pipeline.Spec.Stages),
	)

	return ctrl.Result{}, nil
}

// failPipeline marks the pipeline as failed.
func (r *AgentPipelineReconciler) failPipeline(ctx context.Context, pipeline *aiopsv1alpha1.AgentPipeline, reason string) (ctrl.Result, error) {
	now := metav1.Now()
	pipeline.Status.Phase = aiopsv1alpha1.PhaseFailed
	pipeline.Status.CompletedAt = &now

	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "PipelineFailed",
		Message:            reason,
		ObservedGeneration: pipeline.Generation,
	})

	if err := r.Status().Update(ctx, pipeline); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating pipeline to Failed: %w", err)
	}

	r.Recorder.Event(pipeline, corev1.EventTypeWarning, "PipelineFailed", reason)

	slog.Error("pipeline failed", "name", pipeline.Name, "reason", reason)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentPipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiopsv1alpha1.AgentPipeline{}).
		Owns(&corev1.ConfigMap{}). // Watch owned ConfigMaps for changes.
		Named("agentpipeline").
		Complete(r)
}
