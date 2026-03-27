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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://kagent-controller.kagent.svc.cluster.local:8083"
	defaultUserID  = "admin@kagent.dev"
)

// a2aTaskRequest is the JSON-RPC 2.0 body sent to the kagent A2A endpoint.
// Follows the Google A2A spec: https://github.com/google/A2A
type a2aTaskRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  string        `json:"method"`
	Params  a2aTaskParams `json:"params"`
}

type a2aTaskParams struct {
	ID      string     `json:"id"`
	Message a2aMessage `json:"message"`
}

type a2aMessage struct {
	Role  string        `json:"role"`
	Parts []a2aTextPart `json:"parts"`
}

type a2aTextPart struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// a2aSSEEvent represents a parsed Server-Sent Event from the kagent response stream.
type a2aSSEEvent struct {
	Event string
	Data  string
}

// a2aSSEResponse represents a JSON-RPC 2.0 SSE response from kagent.
// The result field contains the A2A task status update.
type a2aSSEResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      string         `json:"id"`
	Result  *a2aTaskResult `json:"result,omitempty"`
	Error   *a2aRPCError   `json:"error,omitempty"`
}

type a2aRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type a2aTaskResult struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	TaskID string `json:"taskId"`
	Status struct {
		State   string      `json:"state"`
		Message *a2aMessage `json:"message,omitempty"`
	} `json:"status"`
	Artifact *a2aArtifact `json:"artifact,omitempty"`
}

type a2aArtifact struct {
	ArtifactID string        `json:"artifactId"`
	Parts      []a2aTextPart `json:"parts"`
}

// A2ARunner implements AgentRunner using the kagent A2A HTTP/SSE protocol.
type A2ARunner struct {
	baseURL    string
	userID     string
	httpClient *http.Client
}

// NewA2ARunner creates a new A2A runner with the given base URL and user ID.
// If baseURL is empty, it defaults to the in-cluster kagent service URL.
// If userID is empty, it defaults to "admin@kagent.dev".
func NewA2ARunner(baseURL, userID string) *A2ARunner {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if userID == "" {
		userID = defaultUserID
	}
	return &A2ARunner{
		baseURL: strings.TrimRight(baseURL, "/"),
		userID:  userID,
		httpClient: &http.Client{
			// No global timeout — we use context deadlines per-request.
			Timeout: 0,
		},
	}
}

// RunAgent invokes the kagent agent via A2A and blocks until completion.
func (r *A2ARunner) RunAgent(ctx context.Context, req RunRequest) (*RunResult, error) {
	start := time.Now()

	// Apply timeout from the request if the context doesn't already have a deadline.
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Build the A2A JSON-RPC 2.0 request.
	url := fmt.Sprintf("%s/api/a2a/%s/%s", r.baseURL, req.Namespace, req.AgentName)
	taskID := fmt.Sprintf("%s-%s-%d", req.Namespace, req.AgentName, time.Now().UnixNano())
	body := a2aTaskRequest{
		JSONRPC: "2.0",
		ID:      taskID,
		Method:  "message/stream",
		Params: a2aTaskParams{
			ID: taskID,
			Message: a2aMessage{
				Role:  "user",
				Parts: []a2aTextPart{{Kind: "text", Text: req.Prompt}},
			},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling A2A request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return &RunResult{
				Status:   RunStatusTimedOut,
				Duration: time.Since(start),
				Error:    "agent invocation timed out",
			}, nil
		}
		return nil, fmt.Errorf("sending A2A request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &RunResult{
			Status:   RunStatusFailed,
			Duration: time.Since(start),
			Error:    fmt.Sprintf("A2A returned HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	// Parse the SSE stream for task status updates.
	result, err := r.parseSSEStream(ctx, resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return &RunResult{
				Status:   RunStatusTimedOut,
				Duration: time.Since(start),
				Error:    "agent invocation timed out during SSE stream",
			}, nil
		}
		return nil, fmt.Errorf("parsing SSE stream: %w", err)
	}

	result.Duration = time.Since(start)
	return result, nil
}

// parseSSEStream reads the SSE event stream and extracts the final result.
func (r *A2ARunner) parseSSEStream(ctx context.Context, body io.Reader) (*RunResult, error) {
	scanner := bufio.NewScanner(body)
	// Allow up to 1MB lines for large agent outputs.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentEvent a2aSSEEvent
	var result RunResult
	var artifactTexts []string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		line := scanner.Text()

		// Empty line signals end of an event.
		if line == "" {
			if currentEvent.Data != "" {
				if err := r.processEvent(&currentEvent, &result, &artifactTexts); err != nil {
					return nil, err
				}
				// Return immediately on terminal states.
				if result.Status == RunStatusCompleted || result.Status == RunStatusFailed {
					if result.Output == "" && len(artifactTexts) > 0 {
						result.Output = strings.Join(artifactTexts, "\n")
					}
					return &result, nil
				}
			}
			currentEvent = a2aSSEEvent{}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			currentEvent.Data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// Stream ended without a terminal event — treat as failure.
	if result.Status == "" {
		result.Status = RunStatusFailed
		result.Error = "SSE stream ended without a terminal status"
	}

	return &result, nil
}

// processEvent handles a single SSE event and updates the result.
func (r *A2ARunner) processEvent(event *a2aSSEEvent, result *RunResult, artifactTexts *[]string) error {
	log.Printf("[A2A DEBUG] event=%s data=%s", event.Event, event.Data)

	var resp a2aSSEResponse
	if err := json.Unmarshal([]byte(event.Data), &resp); err != nil {
		// Non-JSON events (e.g. heartbeats) are ignored.
		return nil
	}

	// Handle JSON-RPC errors.
	if resp.Error != nil {
		result.Status = RunStatusFailed
		result.Error = fmt.Sprintf("A2A RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		return nil
	}

	if resp.Result == nil {
		return nil
	}

	if resp.Result.TaskID != "" {
		result.TaskID = resp.Result.TaskID
	} else if resp.Result.ID != "" {
		result.TaskID = resp.Result.ID
	}

	// Collect text from artifact events.
	if resp.Result.Artifact != nil {
		for _, p := range resp.Result.Artifact.Parts {
			if p.Text != "" {
				*artifactTexts = append(*artifactTexts, p.Text)
			}
		}
	}

	// Handle terminal status updates.
	switch resp.Result.Status.State {
	case "completed":
		result.Status = RunStatusCompleted
		result.Output = extractText(resp.Result)
	case "failed", "canceled":
		result.Status = RunStatusFailed
		result.Error = extractText(resp.Result)
	}

	return nil
}

// extractText concatenates all text parts from the task result message.
func extractText(task *a2aTaskResult) string {
	if task.Status.Message == nil {
		return ""
	}
	var parts []string
	for _, p := range task.Status.Message.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// CheckHealth verifies connectivity to the kagent A2A endpoint.
func (r *A2ARunner) CheckHealth(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/a2a", r.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating health check request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kagent A2A endpoint unreachable at %s: %w", url, err)
	}
	defer resp.Body.Close()

	return nil
}
