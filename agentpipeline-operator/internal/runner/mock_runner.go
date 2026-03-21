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

import (
	"context"
	"fmt"
	"time"
)

// MockResponse defines the canned response for a specific agent.
type MockResponse struct {
	Output   string
	Status   RunStatus
	Error    string
	Duration time.Duration
}

// MockRunner implements AgentRunner with configurable responses for testing.
type MockRunner struct {
	// Responses maps agent names to their canned responses.
	Responses map[string]MockResponse

	// Calls records all RunAgent invocations for assertion.
	Calls []RunRequest

	// HealthErr is returned by CheckHealth if non-nil.
	HealthErr error
}

// NewMockRunner creates a MockRunner with the given per-agent responses.
func NewMockRunner(responses map[string]MockResponse) *MockRunner {
	return &MockRunner{
		Responses: responses,
	}
}

// RunAgent returns the pre-configured response for the requested agent.
func (m *MockRunner) RunAgent(ctx context.Context, req RunRequest) (*RunResult, error) {
	m.Calls = append(m.Calls, req)

	resp, ok := m.Responses[req.AgentName]
	if !ok {
		return &RunResult{
			Status:   RunStatusFailed,
			Duration: time.Millisecond,
			Error:    fmt.Sprintf("no mock response configured for agent %q", req.AgentName),
		}, nil
	}

	// Simulate timeout if context is already cancelled.
	if ctx.Err() != nil {
		return &RunResult{
			Status:   RunStatusTimedOut,
			Duration: time.Millisecond,
			Error:    "context cancelled",
		}, nil
	}

	return &RunResult{
		TaskID:   fmt.Sprintf("mock-task-%s", req.AgentName),
		Output:   resp.Output,
		Status:   resp.Status,
		Duration: resp.Duration,
		Error:    resp.Error,
	}, nil
}

// CheckHealth returns the pre-configured health error.
func (m *MockRunner) CheckHealth(_ context.Context) error {
	return m.HealthErr
}
