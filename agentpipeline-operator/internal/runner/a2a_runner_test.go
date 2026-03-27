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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestA2ARunnerSuccess(t *testing.T) {
	// Mock SSE server that returns a completed task via JSON-RPC 2.0.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/a2a/kagent/diagnostic" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}

		// Send working status.
		fmt.Fprint(w, "event: task-status\n")
		fmt.Fprint(w, `data: {"jsonrpc":"2.0","id":"req-1","result":{"id":"task-123","status":{"state":"working","message":{"role":"assistant","parts":[{"text":"analyzing..."}]}}}}`+"\n\n")
		flusher.Flush()

		// Send completed status.
		fmt.Fprint(w, "event: task-status\n")
		fmt.Fprint(w, `data: {"jsonrpc":"2.0","id":"req-1","result":{"id":"task-123","status":{"state":"completed","message":{"role":"assistant","parts":[{"text":"OOMKill on pod-xyz"}]}}}}`+"\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	result, err := runner.RunAgent(context.Background(), RunRequest{
		AgentName: "diagnostic",
		Namespace: "kagent",
		Prompt:    "Check cluster health",
		Timeout:   5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("expected Completed, got %s", result.Status)
	}
	if result.TaskID != "task-123" {
		t.Errorf("expected task-123, got %s", result.TaskID)
	}
	if result.Output != "OOMKill on pod-xyz" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestA2ARunnerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, "event: task-status\n")
		fmt.Fprint(w, `data: {"jsonrpc":"2.0","id":"req-1","result":{"id":"task-456","status":{"state":"failed","message":{"role":"assistant","parts":[{"text":"agent crashed"}]}}}}`+"\n\n")
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	result, err := runner.RunAgent(context.Background(), RunRequest{
		AgentName: "bad-agent",
		Namespace: "kagent",
		Prompt:    "do something",
		Timeout:   5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != RunStatusFailed {
		t.Errorf("expected Failed, got %s", result.Status)
	}
	if result.Error != "agent crashed" {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

func TestA2ARunnerTimeout(t *testing.T) {
	// Server that blocks forever.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until context cancelled.
		<-r.Context().Done()
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	result, err := runner.RunAgent(context.Background(), RunRequest{
		AgentName: "slow-agent",
		Namespace: "kagent",
		Prompt:    "be slow",
		Timeout:   100 * time.Millisecond,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != RunStatusTimedOut {
		t.Errorf("expected TimedOut, got %s", result.Status)
	}
}

func TestA2ARunnerHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	result, err := runner.RunAgent(context.Background(), RunRequest{
		AgentName: "agent",
		Namespace: "kagent",
		Prompt:    "test",
		Timeout:   5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != RunStatusFailed {
		t.Errorf("expected Failed, got %s", result.Status)
	}
}

func TestA2ARunnerMultiPartOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, "event: task-status\n")
		fmt.Fprint(w, `data: {"jsonrpc":"2.0","id":"req-1","result":{"id":"task-789","status":{"state":"completed","message":{"role":"assistant","parts":[{"text":"part1"},{"text":"part2"}]}}}}`+"\n\n")
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	result, err := runner.RunAgent(context.Background(), RunRequest{
		AgentName: "agent",
		Namespace: "kagent",
		Prompt:    "test",
		Timeout:   5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "part1\npart2" {
		t.Errorf("expected 'part1\\npart2', got %q", result.Output)
	}
}

func TestA2ARunnerRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, "event: task-status\n")
		fmt.Fprint(w, `data: {"jsonrpc":"2.0","id":"req-1","error":{"code":-32600,"message":"Invalid Request"}}`+"\n\n")
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	result, err := runner.RunAgent(context.Background(), RunRequest{
		AgentName: "agent",
		Namespace: "kagent",
		Prompt:    "test",
		Timeout:   5 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != RunStatusFailed {
		t.Errorf("expected Failed, got %s", result.Status)
	}
}

func TestA2ARunnerCheckHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runner := NewA2ARunner(server.URL, "test@test.com")
	err := runner.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("unexpected health check error: %v", err)
	}
}

func TestA2ARunnerCheckHealthUnreachable(t *testing.T) {
	runner := NewA2ARunner("http://localhost:1", "test@test.com")
	err := runner.CheckHealth(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
