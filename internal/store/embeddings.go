package store

import (
	"encoding/json"
	"fmt"
)

// AgentDistance represents a KNN search result.
type AgentDistance struct {
	AgentID  string  `json:"agent_id"`
	Distance float32 `json:"distance"`
}

// SaveAgentEmbedding upserts an agent's description embedding.
// vec0 tables don't support ON CONFLICT, so we delete then insert.
func (s *Store) SaveAgentEmbedding(agentID, descHash string, embedding []float32) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM agent_embeddings WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("delete old embedding: %w", err)
	}

	vecJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO agent_embeddings(agent_id, desc_hash, embedding) VALUES (?, ?, ?)`,
		agentID, descHash, string(vecJSON)); err != nil {
		return fmt.Errorf("insert embedding: %w", err)
	}

	return tx.Commit()
}

// DeleteAgentEmbedding removes an agent's embedding.
func (s *Store) DeleteAgentEmbedding(agentID string) error {
	_, err := s.db.Exec(`DELETE FROM agent_embeddings WHERE agent_id = ?`, agentID)
	return err
}

// FindNearestAgent performs KNN search against agent embeddings.
// Returns up to limit results ordered by distance (ascending).
func (s *Store) FindNearestAgent(embedding []float32, limit int) ([]AgentDistance, error) {
	vecJSON, err := json.Marshal(embedding)
	if err != nil {
		return nil, fmt.Errorf("marshal query embedding: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT agent_id, distance FROM agent_embeddings WHERE embedding MATCH ? ORDER BY distance LIMIT ?`,
		string(vecJSON), limit)
	if err != nil {
		return nil, fmt.Errorf("knn query: %w", err)
	}
	defer rows.Close()

	var results []AgentDistance
	for rows.Next() {
		var ad AgentDistance
		if err := rows.Scan(&ad.AgentID, &ad.Distance); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, ad)
	}
	return results, rows.Err()
}

// GetAgentEmbeddingHash returns the stored description hash for an agent.
// Returns empty string if no embedding exists.
func (s *Store) GetAgentEmbeddingHash(agentID string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT desc_hash FROM agent_embeddings WHERE agent_id = ?`, agentID).Scan(&hash)
	if err != nil {
		return "", nil // not found is not an error
	}
	return hash, nil
}
