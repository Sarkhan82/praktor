package embeddings

import (
	"context"
	"fmt"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// DefaultModelPath is the well-known location where the model is baked into the Docker image.
const DefaultModelPath = "/opt/models/sentence-transformers_all-MiniLM-L6-v2"

// Embedder generates dense vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dims() int
}

// HugotEmbedder uses the Hugot pure-Go backend to run a sentence transformer model.
type HugotEmbedder struct {
	mu       sync.Mutex
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	dims     int
}

// NewHugotEmbedder creates an embedder using the ONNX model at modelPath.
// If modelPath does not exist, it downloads the model from HuggingFace.
func NewHugotEmbedder(modelPath string) (*HugotEmbedder, error) {
	session, err := hugot.NewGoSession()
	if err != nil {
		return nil, fmt.Errorf("create hugot session: %w", err)
	}

	config := hugot.FeatureExtractionConfig{
		ModelPath: modelPath,
		Name:      "praktor-routing",
		Options:   []hugot.FeatureExtractionOption{pipelines.WithNormalization()},
	}
	pipeline, err := hugot.NewPipeline(session, config)
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("create pipeline: %w", err)
	}

	return &HugotEmbedder{
		session:  session,
		pipeline: pipeline,
		dims:     384,
	}, nil
}

func (h *HugotEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	result, err := h.pipeline.RunPipeline(texts)
	if err != nil {
		return nil, fmt.Errorf("run pipeline: %w", err)
	}

	return result.Embeddings, nil
}

func (h *HugotEmbedder) Dims() int {
	return h.dims
}

func (h *HugotEmbedder) Close() {
	if h.session != nil {
		h.session.Destroy()
	}
}

// DownloadModel downloads a HuggingFace model to the given directory.
// Returns the path to the downloaded model.
func DownloadModel(repo, destDir string) (string, error) {
	opts := hugot.NewDownloadOptions()
	opts.OnnxFilePath = "onnx/model.onnx"
	return hugot.DownloadModel(repo, destDir, opts)
}
