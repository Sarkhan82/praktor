package router

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/store"
)

// mockEmbedder returns a fixed vector for any input.
type mockEmbedder struct {
	vec []float32
	err error
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = m.vec
	}
	return out, nil
}

func (m *mockEmbedder) Dims() int { return len(m.vec) }

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	agents := map[string]config.AgentDefinition{
		"general": {Description: "General assistant", Workspace: "general"},
		"coder":   {Description: "Code specialist", Workspace: "coder"},
	}

	reg := registry.New(s, agents, config.DefaultsConfig{}, filepath.Join(dir, "agents"))
	_ = reg.Sync()

	return New(reg, config.RouterConfig{DefaultAgent: "general"})
}

func TestRouteWithAtPrefix(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "@coder fix the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected agent 'coder', got %q", agentID)
	}
	if msg != "fix the bug" {
		t.Errorf("expected cleaned message 'fix the bug', got %q", msg)
	}
}

func TestRouteWithAtPrefixNoMessage(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "@coder")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected agent 'coder', got %q", agentID)
	}
	if msg != "" {
		t.Errorf("expected empty cleaned message, got %q", msg)
	}
}

func TestRouteWithUnknownAtPrefix(t *testing.T) {
	rtr := newTestRouter(t)

	// Unknown agent name falls back to default
	agentID, msg, err := rtr.Route(context.Background(), "@unknown hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "general" {
		t.Errorf("expected fallback to 'general', got %q", agentID)
	}
	if msg != "@unknown hello" {
		t.Errorf("expected original message preserved, got %q", msg)
	}
}

func TestRouteFallbackToDefault(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "general" {
		t.Errorf("expected default agent 'general', got %q", agentID)
	}
	if msg != "hello world" {
		t.Errorf("expected message 'hello world', got %q", msg)
	}
}

func newTestRouterWithVec(t *testing.T) (*Router, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	agents := map[string]config.AgentDefinition{
		"devops": {Description: "DevOps and infrastructure", Workspace: "devops"},
		"coder":  {Description: "Code review and development", Workspace: "coder"},
	}

	reg := registry.New(s, agents, config.DefaultsConfig{}, filepath.Join(dir, "agents"))
	_ = reg.Sync()

	rtr := New(reg, config.RouterConfig{DefaultAgent: "devops", VectorThreshold: 1.5})
	return rtr, s
}

func TestRouteVectorMatch(t *testing.T) {
	rtr, s := newTestRouterWithVec(t)

	// Store embeddings: devops at [1,0,...], coder at [0,1,...]
	devopsVec := make([]float32, 384)
	devopsVec[0] = 1.0
	coderVec := make([]float32, 384)
	coderVec[1] = 1.0

	s.SaveAgentEmbedding("devops", "h1", devopsVec)
	s.SaveAgentEmbedding("coder", "h2", coderVec)

	// Mock embedder returns vector close to devops
	queryVec := make([]float32, 384)
	queryVec[0] = 0.95
	queryVec[1] = 0.05

	rtr.SetEmbedder(&mockEmbedder{vec: queryVec}, s)

	agentID, msg, err := rtr.Route(context.Background(), "deploy the app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "devops" {
		t.Errorf("expected devops, got %q", agentID)
	}
	if msg != "deploy the app" {
		t.Errorf("expected original message, got %q", msg)
	}
}

func TestRouteVectorNoMatch(t *testing.T) {
	rtr, s := newTestRouterWithVec(t)

	devopsVec := make([]float32, 384)
	devopsVec[0] = 1.0
	s.SaveAgentEmbedding("devops", "h1", devopsVec)

	// Query vector very far from any agent — distance will exceed threshold
	queryVec := make([]float32, 384)
	queryVec[100] = 1.0

	// Set a very low threshold so nothing matches
	rtr.threshold = 0.01
	rtr.SetEmbedder(&mockEmbedder{vec: queryVec}, s)

	agentID, _, err := rtr.Route(context.Background(), "random stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall through to default agent
	if agentID != "devops" {
		t.Errorf("expected fallback to devops, got %q", agentID)
	}
}

func TestRouteVectorDisabled(t *testing.T) {
	rtr, _ := newTestRouterWithVec(t)
	// No embedder set — should use default routing
	agentID, _, err := rtr.Route(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "devops" {
		t.Errorf("expected default agent devops, got %q", agentID)
	}
}

func TestRouteVectorErrorFallthrough(t *testing.T) {
	rtr, s := newTestRouterWithVec(t)
	rtr.SetEmbedder(&mockEmbedder{err: fmt.Errorf("model failed")}, s)

	agentID, _, err := rtr.Route(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "devops" {
		t.Errorf("expected fallback to devops on error, got %q", agentID)
	}
}

func TestRouteSwarmPrefixBeatsVector(t *testing.T) {
	rtr, s := newTestRouterWithVec(t)

	devopsVec := make([]float32, 384)
	devopsVec[0] = 1.0
	s.SaveAgentEmbedding("devops", "h1", devopsVec)
	rtr.SetEmbedder(&mockEmbedder{vec: devopsVec}, s)

	agentID, msg, err := rtr.Route(context.Background(), "@swarm agent1,agent2: do stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "swarm" {
		t.Errorf("expected swarm, got %q", agentID)
	}
	if msg != "agent1,agent2: do stuff" {
		t.Errorf("expected cleaned swarm message, got %q", msg)
	}
}

func TestRouteVectorEmptyEmbeddingResult(t *testing.T) {
	rtr, s := newTestRouterWithVec(t)

	// Embedder returns empty slice
	rtr.SetEmbedder(&mockEmbedder{vec: nil}, s)

	agentID, _, err := rtr.Route(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "devops" {
		t.Errorf("expected fallback to devops, got %q", agentID)
	}
}

func TestRouteAtPrefixBeatsVector(t *testing.T) {
	rtr, s := newTestRouterWithVec(t)

	// Set up vector routing to point to devops
	devopsVec := make([]float32, 384)
	devopsVec[0] = 1.0
	s.SaveAgentEmbedding("devops", "h1", devopsVec)
	rtr.SetEmbedder(&mockEmbedder{vec: devopsVec}, s)

	// But use @coder prefix — should win
	agentID, msg, err := rtr.Route(context.Background(), "@coder fix this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected @prefix to win, got %q", agentID)
	}
	if msg != "fix this" {
		t.Errorf("expected cleaned message, got %q", msg)
	}
}
