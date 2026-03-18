package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mtzanidakis/praktor/internal/embeddings"
)

func main() {
	repo := flag.String("repo", "sentence-transformers/all-MiniLM-L6-v2", "HuggingFace model repo")
	dest := flag.String("dest", "data/models", "destination directory")
	flag.Parse()

	path, err := embeddings.DownloadModel(*repo, *dest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(path)
}
