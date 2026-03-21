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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiopsv1alpha1 "github.com/Glutenfree69/agentpipeline-operator/api/v1alpha1"
	"github.com/Glutenfree69/agentpipeline-operator/internal/runner"
)

func newTestReconciler(mockRunner runner.AgentRunner) *AgentPipelineReconciler {
	return &AgentPipelineReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(100),
		Runner:   mockRunner,
		Handler:  NewStageHandler(k8sClient),
	}
}

func newPipeline(name string, stages []aiopsv1alpha1.StageSpec) *aiopsv1alpha1.AgentPipeline {
	return &aiopsv1alpha1.AgentPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: aiopsv1alpha1.AgentPipelineSpec{
			Trigger: aiopsv1alpha1.TriggerSpec{Type: aiopsv1alpha1.TriggerManual},
			Stages:  stages,
		},
	}
}

var _ = Describe("AgentPipeline Controller", func() {
	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond

	ctx := context.Background()

	AfterEach(func() {
		// Clean up all AgentPipeline resources in default namespace.
		pipelineList := &aiopsv1alpha1.AgentPipelineList{}
		err := k8sClient.List(ctx, pipelineList)
		if err == nil {
			for i := range pipelineList.Items {
				p := &pipelineList.Items[i]
				// Remove finalizer to allow deletion.
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
		}
		// Clean up ConfigMaps created by the handler.
		cmList := &corev1.ConfigMapList{}
		if err := k8sClient.List(ctx, cmList); err == nil {
			for i := range cmList.Items {
				if cmList.Items[i].Labels["app.kubernetes.io/managed-by"] == "agentpipeline-operator" {
					_ = k8sClient.Delete(ctx, &cmList.Items[i])
				}
			}
		}
	})

	Context("Pending to Running transition", func() {
		It("should add finalizer and transition to Running", func() {
			pipeline := newPipeline("test-pending", []aiopsv1alpha1.StageSpec{
				{
					Name:     "stage-1",
					AgentRef: aiopsv1alpha1.AgentReference{Name: "test-agent", Namespace: "kagent"},
				},
			})
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{
				"test-agent": {Output: "done", Status: runner.RunStatusCompleted},
			})
			reconciler := newTestReconciler(mockRunner)
			nn := types.NamespacedName{Name: "test-pending", Namespace: "default"}

			// First reconcile: should add finalizer.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			// Verify finalizer was added.
			updated := &aiopsv1alpha1.AgentPipeline{}
			Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(finalizerName))

			// Second reconcile: should transition to Running.
			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(aiopsv1alpha1.PhaseRunning))
			Expect(updated.Status.CurrentStage).To(Equal("stage-1"))
			Expect(updated.Status.StartedAt).NotTo(BeNil())
		})
	})

	Context("Single stage pipeline", func() {
		It("should complete successfully", func() {
			pipeline := newPipeline("test-single", []aiopsv1alpha1.StageSpec{
				{
					Name:     "diagnose",
					AgentRef: aiopsv1alpha1.AgentReference{Name: "diagnostic", Namespace: "kagent"},
					Prompt:   "Run diagnostic",
				},
			})
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{
				"diagnostic": {Output: "All systems healthy", Status: runner.RunStatusCompleted},
			})
			reconciler := newTestReconciler(mockRunner)
			nn := types.NamespacedName{Name: "test-single", Namespace: "default"}

			// Reconcile until completed (finalizer → running → stage → complete).
			for i := 0; i < 10; i++ {
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())

				updated := &aiopsv1alpha1.AgentPipeline{}
				Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
				if updated.Status.Phase == aiopsv1alpha1.PhaseCompleted {
					break
				}
				if !result.Requeue && result.RequeueAfter == 0 {
					break
				}
			}

			// Verify final state.
			final := &aiopsv1alpha1.AgentPipeline{}
			Expect(k8sClient.Get(ctx, nn, final)).To(Succeed())
			Expect(final.Status.Phase).To(Equal(aiopsv1alpha1.PhaseCompleted))
			Expect(final.Status.CompletedAt).NotTo(BeNil())
			Expect(final.Status.Stages).To(HaveLen(1))
			Expect(final.Status.Stages[0].Phase).To(Equal(aiopsv1alpha1.StageCompleted))
			Expect(final.Status.Stages[0].Output).To(Equal("All systems healthy"))
			Expect(final.Status.Stages[0].OutputRef).To(Equal("test-single-stage-diagnose"))

			// Verify ConfigMap was created.
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-single-stage-diagnose", Namespace: "default",
			}, cm)).To(Succeed())
			Expect(cm.Data["output"]).To(Equal("All systems healthy"))

			// Verify agent was called with the right prompt.
			Expect(mockRunner.Calls).To(HaveLen(1))
			Expect(mockRunner.Calls[0].Prompt).To(Equal("Run diagnostic"))
		})
	})

	Context("Multi-stage pipeline with output passing", func() {
		It("should pass output between stages via templates", func() {
			pipeline := newPipeline("test-multi", []aiopsv1alpha1.StageSpec{
				{
					Name:     "diagnose",
					AgentRef: aiopsv1alpha1.AgentReference{Name: "diagnostic", Namespace: "kagent"},
					Prompt:   "Find issues",
				},
				{
					Name:      "advise",
					AgentRef:  aiopsv1alpha1.AgentReference{Name: "advisor", Namespace: "kagent"},
					DependsOn: []string{"diagnose"},
					Prompt:    "Previous: {{.PreviousOutput}}",
				},
			})
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{
				"diagnostic": {Output: "OOMKill on pod-xyz", Status: runner.RunStatusCompleted},
				"advisor":    {Output: "Increase memory limit to 512Mi", Status: runner.RunStatusCompleted},
			})
			reconciler := newTestReconciler(mockRunner)
			nn := types.NamespacedName{Name: "test-multi", Namespace: "default"}

			// Run reconcile loop.
			for i := 0; i < 15; i++ {
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())

				updated := &aiopsv1alpha1.AgentPipeline{}
				Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
				if updated.Status.Phase == aiopsv1alpha1.PhaseCompleted {
					break
				}
				if !result.Requeue && result.RequeueAfter == 0 {
					break
				}
			}

			final := &aiopsv1alpha1.AgentPipeline{}
			Expect(k8sClient.Get(ctx, nn, final)).To(Succeed())
			Expect(final.Status.Phase).To(Equal(aiopsv1alpha1.PhaseCompleted))
			Expect(final.Status.Stages).To(HaveLen(2))
			Expect(final.Status.Stages[0].Phase).To(Equal(aiopsv1alpha1.StageCompleted))
			Expect(final.Status.Stages[1].Phase).To(Equal(aiopsv1alpha1.StageCompleted))

			// Verify the advisor was called with diagnostic output interpolated.
			Expect(mockRunner.Calls).To(HaveLen(2))
			Expect(mockRunner.Calls[1].Prompt).To(Equal("Previous: OOMKill on pod-xyz"))
		})
	})

	Context("Stage failure with retry", func() {
		It("should retry a failed stage and eventually succeed", func() {
			retryBackoff := metav1.Duration{Duration: 1 * time.Millisecond}
			pipeline := newPipeline("test-retry", []aiopsv1alpha1.StageSpec{
				{
					Name:     "flaky",
					AgentRef: aiopsv1alpha1.AgentReference{Name: "flaky-agent", Namespace: "kagent"},
					RetryPolicy: &aiopsv1alpha1.RetryPolicy{
						MaxRetries: 2,
						Backoff:    &retryBackoff,
					},
				},
			})
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			callCount := 0
			// First call fails, second succeeds.
			mockRunner := &runner.MockRunner{
				Responses: map[string]runner.MockResponse{},
			}
			// Override RunAgent to track calls.
			originalRunner := runner.NewMockRunner(map[string]runner.MockResponse{})
			_ = originalRunner

			// Use a custom mock that fails then succeeds.
			failThenSucceedRunner := &failThenSucceedMockRunner{
				failCount: 1,
				callCount: &callCount,
			}
			_ = mockRunner

			reconciler := newTestReconciler(failThenSucceedRunner)
			nn := types.NamespacedName{Name: "test-retry", Namespace: "default"}

			// Run reconcile loop.
			for i := 0; i < 20; i++ {
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())

				updated := &aiopsv1alpha1.AgentPipeline{}
				Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
				if updated.Status.Phase == aiopsv1alpha1.PhaseCompleted ||
					updated.Status.Phase == aiopsv1alpha1.PhaseFailed {
					break
				}
				if !result.Requeue && result.RequeueAfter == 0 {
					break
				}
			}

			final := &aiopsv1alpha1.AgentPipeline{}
			Expect(k8sClient.Get(ctx, nn, final)).To(Succeed())
			Expect(final.Status.Phase).To(Equal(aiopsv1alpha1.PhaseCompleted))
			Expect(final.Status.Stages[0].RetryCount).To(Equal(int32(1)))
			Expect(callCount).To(Equal(2))
		})
	})

	Context("Stage failure exhausting retries", func() {
		It("should fail the pipeline after exhausting retries", func() {
			retryBackoff := metav1.Duration{Duration: 1 * time.Millisecond}
			pipeline := newPipeline("test-fail", []aiopsv1alpha1.StageSpec{
				{
					Name:     "always-fail",
					AgentRef: aiopsv1alpha1.AgentReference{Name: "bad-agent", Namespace: "kagent"},
					RetryPolicy: &aiopsv1alpha1.RetryPolicy{
						MaxRetries: 1,
						Backoff:    &retryBackoff,
					},
				},
			})
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{
				"bad-agent": {Status: runner.RunStatusFailed, Error: "agent crashed"},
			})
			reconciler := newTestReconciler(mockRunner)
			nn := types.NamespacedName{Name: "test-fail", Namespace: "default"}

			for i := 0; i < 20; i++ {
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())

				updated := &aiopsv1alpha1.AgentPipeline{}
				Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
				if updated.Status.Phase == aiopsv1alpha1.PhaseFailed {
					break
				}
				if !result.Requeue && result.RequeueAfter == 0 {
					break
				}
			}

			final := &aiopsv1alpha1.AgentPipeline{}
			Expect(k8sClient.Get(ctx, nn, final)).To(Succeed())
			Expect(final.Status.Phase).To(Equal(aiopsv1alpha1.PhaseFailed))
			Expect(final.Status.Stages[0].RetryCount).To(Equal(int32(1)))
		})
	})

	Context("Deletion with finalizer", func() {
		It("should remove finalizer on deletion", func() {
			pipeline := newPipeline("test-delete", []aiopsv1alpha1.StageSpec{
				{
					Name:     "stage-1",
					AgentRef: aiopsv1alpha1.AgentReference{Name: "agent", Namespace: "kagent"},
				},
			})
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{})
			reconciler := newTestReconciler(mockRunner)
			nn := types.NamespacedName{Name: "test-delete", Namespace: "default"}

			// Reconcile to add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer added.
			updated := &aiopsv1alpha1.AgentPipeline{}
			Expect(k8sClient.Get(ctx, nn, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(finalizerName))

			// Delete the resource.
			Expect(k8sClient.Delete(ctx, updated)).To(Succeed())

			// Reconcile deletion.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Verify resource is gone.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, nn, &aiopsv1alpha1.AgentPipeline{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Not found resource", func() {
		It("should handle missing resource gracefully", func() {
			mockRunner := runner.NewMockRunner(map[string]runner.MockResponse{})
			reconciler := newTestReconciler(mockRunner)

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})

// failThenSucceedMockRunner fails N times then succeeds.
type failThenSucceedMockRunner struct {
	failCount int
	callCount *int
}

func (m *failThenSucceedMockRunner) RunAgent(_ context.Context, req runner.RunRequest) (*runner.RunResult, error) {
	*m.callCount++
	if *m.callCount <= m.failCount {
		return &runner.RunResult{
			Status: runner.RunStatusFailed,
			Error:  "transient failure",
		}, nil
	}
	return &runner.RunResult{
		TaskID: "mock-success",
		Output: "recovered successfully",
		Status: runner.RunStatusCompleted,
	}, nil
}

func (m *failThenSucceedMockRunner) CheckHealth(_ context.Context) error {
	return nil
}
