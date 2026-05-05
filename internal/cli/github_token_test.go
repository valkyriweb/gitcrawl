package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/openclaw/gitcrawl/internal/config"
)

func TestResolveGitHubTokenFallsBackToGHAuthToken(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\nif [ \"$1\" = auth ] && [ \"$2\" = token ]; then echo gh-fallback-token; exit 0; fi\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITCRAWL_GH_PATH", ghPath)

	token := New().resolveGitHubToken(context.Background(), config.Default())
	if token.Value != "gh-fallback-token" || token.Source != "gh auth token" {
		t.Fatalf("token = %#v", token)
	}
}
