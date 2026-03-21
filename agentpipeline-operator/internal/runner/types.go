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

package runner

import "time"

// RunStatus represents the outcome of an agent invocation.
type RunStatus string

const (
	RunStatusCompleted RunStatus = "Completed"
	RunStatusFailed    RunStatus = "Failed"
	RunStatusTimedOut  RunStatus = "TimedOut"
)

// RunRequest contains the parameters for invoking a kagent agent via A2A.
type RunRequest struct {
	// AgentName is the kagent Agent CR name.
	AgentName string

	// Namespace is the namespace where the agent is deployed.
	Namespace string

	// Prompt is the rendered prompt text to send to the agent.
	Prompt string

	// Timeout is the maximum duration to wait for the agent to respond.
	Timeout time.Duration
}

// RunResult contains the outcome of an agent invocation.
type RunResult struct {
	// TaskID is the A2A task identifier returned by kagent.
	TaskID string

	// Output is the final text response from the agent.
	Output string

	// Status indicates whether the invocation succeeded, failed, or timed out.
	Status RunStatus

	// Duration is the wall-clock time the invocation took.
	Duration time.Duration

	// Error contains details if the invocation failed.
	Error string
}
