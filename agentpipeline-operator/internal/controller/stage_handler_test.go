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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	aiopsv1alpha1 "github.com/Glutenfree69/agentpipeline-operator/api/v1alpha1"
)

var _ = Describe("StageHandler", func() {
	handler := &StageHandler{}

	Context("RenderPrompt", func() {
		It("should render a simple template with PreviousOutput", func() {
			stage := &aiopsv1alpha1.StageSpec{
				Name:   "advise",
				Prompt: "Analyze: {{.PreviousOutput}}",
			}
			pCtx := &PipelineContext{
				PreviousOutput: "OOMKill detected",
			}

			result, err := handler.RenderPrompt(stage, pCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("Analyze: OOMKill detected"))
		})

		It("should render a template with StageOutput function", func() {
			stage := &aiopsv1alpha1.StageSpec{
				Name:   "propose",
				Prompt: `Diagnostic: {{.StageOutput "diagnose"}} | Advice: {{.StageOutput "advise"}}`,
			}
			pCtx := &PipelineContext{
				stageOutputs: map[string]string{
					"diagnose": "issue found",
					"advise":   "fix it",
				},
			}

			result, err := handler.RenderPrompt(stage, pCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("Diagnostic: issue found | Advice: fix it"))
		})

		It("should render a template with Inputs", func() {
			stage := &aiopsv1alpha1.StageSpec{
				Name:   "custom",
				Prompt: `Repo: {{index .Inputs "repo"}}`,
				Inputs: map[string]string{"repo": "Glutenfree69/Cluster69"},
			}
			pCtx := &PipelineContext{
				Inputs: stage.Inputs,
			}

			result, err := handler.RenderPrompt(stage, pCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("Repo: Glutenfree69/Cluster69"))
		})

		It("should return default prompt when no template and no previous output", func() {
			stage := &aiopsv1alpha1.StageSpec{
				Name:     "first",
				AgentRef: aiopsv1alpha1.AgentReference{Name: "diagnostic"},
			}
			pCtx := &PipelineContext{}

			result, err := handler.RenderPrompt(stage, pCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("Execute agent diagnostic"))
		})

		It("should return previous output when no template", func() {
			stage := &aiopsv1alpha1.StageSpec{
				Name:     "second",
				AgentRef: aiopsv1alpha1.AgentReference{Name: "advisor"},
			}
			pCtx := &PipelineContext{PreviousOutput: "previous result"}

			result, err := handler.RenderPrompt(stage, pCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("previous result"))
		})

		It("should return error for invalid template", func() {
			stage := &aiopsv1alpha1.StageSpec{
				Name:   "bad",
				Prompt: "{{.Invalid",
			}
			pCtx := &PipelineContext{}

			_, err := handler.RenderPrompt(stage, pCtx)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("truncateOutput", func() {
		It("should not truncate short output", func() {
			result := truncateOutput("short")
			Expect(result).To(Equal("short"))
		})

		It("should truncate long output", func() {
			long := strings.Repeat("x", 2000)
			result := truncateOutput(long)
			Expect(len(result)).To(Equal(maxStatusOutputLen))
			Expect(result).To(HavePrefix("..."))
		})
	})

	Context("AreDependenciesMet", func() {
		It("should return true when no dependencies", func() {
			pipeline := &aiopsv1alpha1.AgentPipeline{}
			stage := &aiopsv1alpha1.StageSpec{Name: "first"}
			Expect(AreDependenciesMet(pipeline, stage)).To(BeTrue())
		})

		It("should return true when all deps completed", func() {
			pipeline := &aiopsv1alpha1.AgentPipeline{
				Status: aiopsv1alpha1.AgentPipelineStatus{
					Stages: []aiopsv1alpha1.StageStatus{
						{Name: "stage-a", Phase: aiopsv1alpha1.StageCompleted},
					},
				},
			}
			stage := &aiopsv1alpha1.StageSpec{
				Name:      "stage-b",
				DependsOn: []string{"stage-a"},
			}
			Expect(AreDependenciesMet(pipeline, stage)).To(BeTrue())
		})

		It("should return false when deps not completed", func() {
			pipeline := &aiopsv1alpha1.AgentPipeline{
				Status: aiopsv1alpha1.AgentPipelineStatus{
					Stages: []aiopsv1alpha1.StageStatus{
						{Name: "stage-a", Phase: aiopsv1alpha1.StageRunning},
					},
				},
			}
			stage := &aiopsv1alpha1.StageSpec{
				Name:      "stage-b",
				DependsOn: []string{"stage-a"},
			}
			Expect(AreDependenciesMet(pipeline, stage)).To(BeFalse())
		})
	})

	Context("GetEffectiveRetryPolicy", func() {
		It("should prefer stage-level policy", func() {
			pipeline := &aiopsv1alpha1.AgentPipeline{
				Spec: aiopsv1alpha1.AgentPipelineSpec{
					RetryPolicy: &aiopsv1alpha1.RetryPolicy{MaxRetries: 1},
				},
			}
			stage := &aiopsv1alpha1.StageSpec{
				RetryPolicy: &aiopsv1alpha1.RetryPolicy{MaxRetries: 3},
			}
			policy := GetEffectiveRetryPolicy(pipeline, stage)
			Expect(policy.MaxRetries).To(Equal(int32(3)))
		})

		It("should fall back to pipeline-level policy", func() {
			pipeline := &aiopsv1alpha1.AgentPipeline{
				Spec: aiopsv1alpha1.AgentPipelineSpec{
					RetryPolicy: &aiopsv1alpha1.RetryPolicy{MaxRetries: 2},
				},
			}
			stage := &aiopsv1alpha1.StageSpec{}
			policy := GetEffectiveRetryPolicy(pipeline, stage)
			Expect(policy.MaxRetries).To(Equal(int32(2)))
		})

		It("should default to zero retries", func() {
			pipeline := &aiopsv1alpha1.AgentPipeline{}
			stage := &aiopsv1alpha1.StageSpec{}
			policy := GetEffectiveRetryPolicy(pipeline, stage)
			Expect(policy.MaxRetries).To(Equal(int32(0)))
		})
	})
})
