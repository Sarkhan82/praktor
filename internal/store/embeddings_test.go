package store

import (
	"path/filepath"
	"testing"
)

func newTestStoreWithVec(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSaveAndFindNearestAgent(t *testing.T) {
	s := newTestStoreWithVec(t)

	// Create agents first (foreign key not enforced by vec0, but descriptions need to exist)
	s.SaveAgent(&Agent{ID: "devops", Name: "devops", Description: "DevOps", Workspace: "devops"})
	s.SaveAgent(&Agent{ID: "coder", Name: "coder", Description: "Coding", Workspace: "coder"})
	s.SaveAgent(&Agent{ID: "researcher", Name: "researcher", Description: "Research", Workspace: "researcher"})

	// Simple 4-dimensional embeddings for testing
	// In reality these are 384-dim, but vec0 was created with float[384]
	// We need to use 384-dim vectors to match the table definition
	devopsVec := make([]float32, 384)
	devopsVec[0] = 1.0 // heavily weighted on dim 0

	coderVec := make([]float32, 384)
	coderVec[1] = 1.0 // heavily weighted on dim 1

	researcherVec := make([]float32, 384)
	researcherVec[2] = 1.0 // heavily weighted on dim 2

	// Save embeddings
	if err := s.SaveAgentEmbedding("devops", "hash1", devopsVec); err != nil {
		t.Fatalf("save devops embedding: %v", err)
	}
	if err := s.SaveAgentEmbedding("coder", "hash2", coderVec); err != nil {
		t.Fatalf("save coder embedding: %v", err)
	}
	if err := s.SaveAgentEmbedding("researcher", "hash3", researcherVec); err != nil {
		t.Fatalf("save researcher embedding: %v", err)
	}

	// Query vector close to devops
	queryVec := make([]float32, 384)
	queryVec[0] = 0.9
	queryVec[1] = 0.1

	results, err := s.FindNearestAgent(queryVec, 3)
	if err != nil {
		t.Fatalf("find nearest: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].AgentID != "devops" {
		t.Errorf("expected devops as nearest, got %s", results[0].AgentID)
	}

	// Test hash lookup
	hash, err := s.GetAgentEmbeddingHash("devops")
	if err != nil {
		t.Fatalf("get hash: %v", err)
	}
	if hash != "hash1" {
		t.Errorf("expected hash1, got %s", hash)
	}

	// Test upsert (save again with different hash)
	if err := s.SaveAgentEmbedding("devops", "hash1_updated", devopsVec); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}
	hash, _ = s.GetAgentEmbeddingHash("devops")
	if hash != "hash1_updated" {
		t.Errorf("expected hash1_updated, got %s", hash)
	}

	// Test delete
	if err := s.DeleteAgentEmbedding("coder"); err != nil {
		t.Fatalf("delete embedding: %v", err)
	}
	results, err = s.FindNearestAgent(queryVec, 10)
	if err != nil {
		t.Fatalf("find after delete: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results after delete, got %d", len(results))
	}
}

func TestFindNearestAgentEmptyTable(t *testing.T) {
	s := newTestStoreWithVec(t)

	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	results, err := s.FindNearestAgent(queryVec, 5)
	if err != nil {
		t.Fatalf("find nearest on empty table: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestGetAgentEmbeddingHashNotFound(t *testing.T) {
	s := newTestStoreWithVec(t)

	hash, err := s.GetAgentEmbeddingHash("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "" {
		t.Errorf("expected empty hash for nonexistent agent, got %q", hash)
	}
}

func TestFindNearestAgentOrdering(t *testing.T) {
	s := newTestStoreWithVec(t)

	s.SaveAgent(&Agent{ID: "a", Name: "a", Workspace: "a"})
	s.SaveAgent(&Agent{ID: "b", Name: "b", Workspace: "b"})
	s.SaveAgent(&Agent{ID: "c", Name: "c", Workspace: "c"})

	vecA := make([]float32, 384)
	vecA[0] = 1.0
	vecB := make([]float32, 384)
	vecB[0] = 0.5
	vecB[1] = 0.5
	vecC := make([]float32, 384)
	vecC[1] = 1.0

	s.SaveAgentEmbedding("a", "ha", vecA)
	s.SaveAgentEmbedding("b", "hb", vecB)
	s.SaveAgentEmbedding("c", "hc", vecC)

	// Query close to vecA
	query := make([]float32, 384)
	query[0] = 0.9
	query[1] = 0.1

	results, err := s.FindNearestAgent(query, 3)
	if err != nil {
		t.Fatalf("find nearest: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	// a should be closest, then b, then c
	if results[0].AgentID != "a" {
		t.Errorf("expected a first, got %s", results[0].AgentID)
	}
	if results[1].AgentID != "b" {
		t.Errorf("expected b second, got %s", results[1].AgentID)
	}
	if results[2].AgentID != "c" {
		t.Errorf("expected c third, got %s", results[2].AgentID)
	}
	// Distances should be ascending
	if results[0].Distance >= results[1].Distance {
		t.Errorf("expected ascending distance: %f >= %f", results[0].Distance, results[1].Distance)
	}
	if results[1].Distance >= results[2].Distance {
		t.Errorf("expected ascending distance: %f >= %f", results[1].Distance, results[2].Distance)
	}
}

func TestDeleteAgentEmbeddingNonexistent(t *testing.T) {
	s := newTestStoreWithVec(t)

	// Should not error on nonexistent agent
	if err := s.DeleteAgentEmbedding("ghost"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
