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

import "context"

// AgentRunner abstracts the invocation of a kagent agent.
// The primary implementation uses the A2A HTTP protocol to communicate
// with the kagent controller service. A mock implementation is provided
// for unit testing.
type AgentRunner interface {
	// RunAgent invokes the named agent with the given request and blocks
	// until the agent completes, fails, or the context is cancelled.
	RunAgent(ctx context.Context, req RunRequest) (*RunResult, error)

	// CheckHealth verifies connectivity to the kagent A2A endpoint.
	CheckHealth(ctx context.Context) error
}
