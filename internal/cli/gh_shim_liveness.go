package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultGHLivenessTTL = 5 * time.Minute

type ghLivenessTombstone struct {
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Args      []string  `json:"args"`
	Tags      []string  `json:"tags"`
	Reason    string    `json:"reason"`
}

type ghLivenessTombstoneInfo struct {
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Age       string    `json:"age"`
	ExpiresIn string    `json:"expires_in"`
	Tags      []string  `json:"tags"`
	Reason    string    `json:"reason"`
	Args      []string  `json:"args"`
}

func (a *App) recordGHLivenessTombstone(ctx context.Context, args []string) error {
	if !ghCommandMutationNeedsLiveness(args) {
		return nil
	}
	tags := a.ghMutationInvalidationTags(ctx, args)
	if !ghTagsNeedLiveness(tags) {
		return nil
	}
	dir, err := a.ghLivenessDir()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tombstone := ghLivenessTombstone{
		CreatedAt: now,
		ExpiresAt: now.Add(ghLivenessTTL()),
		Args:      append([]string(nil), args...),
		Tags:      tags,
		Reason:    ghCommandName(args),
	}
	material := strings.Join(append([]string{now.Format(time.RFC3339Nano)}, canonicalGHCommandArgs(args)...), "\x00")
	sum := sha256.Sum256([]byte(material))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
	data, err := json.Marshal(tombstone)
	if err != nil {
		return err
	}
	return writeAtomicFile(path, data, 0o600)
}

func (a *App) activeGHLivenessTombstone(ctx context.Context, args []string) (ghLivenessTombstone, bool) {
	readTags := a.ghCommandCacheTags(ctx, args)
	if !ghTagsNeedLiveness(readTags) {
		return ghLivenessTombstone{}, false
	}
	tombstones, err := a.activeGHLivenessTombstones()
	if err != nil {
		return ghLivenessTombstone{}, false
	}
	for _, tombstone := range tombstones {
		if ghLivenessTagsMatch(readTags, tombstone.Tags) {
			return tombstone, true
		}
	}
	return ghLivenessTombstone{}, false
}

func (a *App) shouldBypassGHCacheForLiveness(ctx context.Context, args []string, controls ghShimControls) bool {
	if controls.Cached {
		return false
	}
	tombstone, ok := a.activeGHLivenessTombstone(ctx, args)
	if !ok {
		return false
	}
	_ = a.incrementGHXCacheCounter("live_bypasses")
	_, _ = fmt.Fprintf(a.Stderr, "gitcrawl: bypassing gh cache after recent %s mutation (%s old); use --cached to force cache\n",
		tombstone.Reason, time.Since(tombstone.CreatedAt).Round(time.Second))
	return true
}

func (a *App) activeGHLivenessTombstones() ([]ghLivenessTombstone, error) {
	cacheDir, err := a.ghCommandCacheDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cacheDir, "_liveness")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	now := time.Now().UTC()
	var tombstones []ghLivenessTombstone
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var tombstone ghLivenessTombstone
		if err := json.Unmarshal(data, &tombstone); err != nil {
			_ = os.Remove(path)
			continue
		}
		if tombstone.ExpiresAt.IsZero() || !tombstone.ExpiresAt.After(now) {
			_ = os.Remove(path)
			continue
		}
		tombstones = append(tombstones, tombstone)
	}
	return tombstones, nil
}

func (a *App) activeGHLivenessTombstoneInfos() ([]ghLivenessTombstoneInfo, error) {
	tombstones, err := a.activeGHLivenessTombstones()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	infos := make([]ghLivenessTombstoneInfo, 0, len(tombstones))
	for _, tombstone := range tombstones {
		infos = append(infos, ghLivenessTombstoneInfo{
			CreatedAt: tombstone.CreatedAt,
			ExpiresAt: tombstone.ExpiresAt,
			Age:       now.Sub(tombstone.CreatedAt).Round(time.Second).String(),
			ExpiresIn: tombstone.ExpiresAt.Sub(now).Round(time.Second).String(),
			Tags:      tombstone.Tags,
			Reason:    tombstone.Reason,
			Args:      tombstone.Args,
		})
	}
	return infos, nil
}

func (a *App) ghLivenessDir() (string, error) {
	cacheDir, err := a.ghCommandCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "_liveness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func ghLivenessTTL() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("GITCRAWL_GH_LIVENESS_TTL")); raw != "" {
		if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
			return duration
		}
	}
	return defaultGHLivenessTTL
}

func ghCommandMutationNeedsLiveness(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "run", "workflow", "release":
		return mutatingGHCommand(args)
	case "api":
		return mutatingGHCommand(args) && ghTagsNeedLiveness(ghAPITags(args[1:]))
	default:
		return false
	}
}

func ghCommandNeedsLivenessNotice(args []string) bool {
	return ghTagsNeedLiveness(ghAPITagsIfAPI(args)) || (len(args) > 0 && (args[0] == "run" || args[0] == "workflow" || args[0] == "release"))
}

func ghAPITagsIfAPI(args []string) []string {
	if len(args) > 0 && args[0] == "api" {
		return ghAPITags(args[1:])
	}
	return nil
}

func ghTagsNeedLiveness(tags []string) bool {
	for _, tag := range tags {
		if tag == "actions" || tag == "releases" {
			return true
		}
	}
	return false
}

func ghLivenessTagsMatch(readTags, mutationTags []string) bool {
	readRepo := ghFirstRepoTag(readTags)
	mutationRepo := ghFirstRepoTag(mutationTags)
	if readRepo != "" && mutationRepo != "" && readRepo != mutationRepo {
		return false
	}
	mutationSet := stringSet(mutationTags)
	for _, tag := range readTags {
		if tag == "" || strings.HasPrefix(tag, "repo:") {
			continue
		}
		if _, ok := mutationSet[tag]; ok {
			return true
		}
	}
	return false
}

func ghFirstRepoTag(tags []string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, "repo:") {
			return tag
		}
	}
	return ""
}
