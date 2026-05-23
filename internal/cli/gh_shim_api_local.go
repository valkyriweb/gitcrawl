package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func (a *App) execLocalGHAPI(ctx context.Context, args []string, controls ghShimControls) (bool, error) {
	if host := strings.TrimSpace(os.Getenv("GH_HOST")); host != "" && !strings.EqualFold(host, "github.com") {
		return false, nil
	}
	route, jqExpr, ok := parseLocalGHAPIArgs(args)
	if !ok {
		return false, nil
	}
	owner, repoName, number, ok := parsePullAPIRoute(route)
	if !ok {
		return false, nil
	}
	row, err := a.localGHPullAPIResponse(ctx, owner+"/"+repoName, number, !controls.Cached)
	if err != nil {
		if controls.Cached {
			return true, localGHUnsupported(err)
		}
		return false, nil
	}
	data, err := json.Marshal(row)
	if err != nil {
		return true, err
	}
	stdout := string(data) + "\n"
	if strings.TrimSpace(jqExpr) != "" {
		projectedOut, projectedErr, err := runGHAPIProjection(stdout, jqExpr)
		if err != nil {
			if controls.Cached {
				return true, err
			}
			return false, nil
		}
		_, _ = io.WriteString(a.Stdout, projectedOut)
		_, _ = io.WriteString(a.Stderr, projectedErr)
	} else {
		_, _ = io.WriteString(a.Stdout, stdout)
	}
	_ = a.incrementGHXCacheCounter("local_hits")
	return true, nil
}

func parseLocalGHAPIArgs(args []string) (string, string, bool) {
	if len(args) == 0 || args[0] != "api" {
		return "", "", false
	}
	method := "GET"
	route := ""
	jqExpr := ""
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-X", "--method":
			if index+1 >= len(args) {
				return "", "", false
			}
			method = strings.ToUpper(strings.TrimSpace(args[index+1]))
			index++
		case "--jq", "-q":
			if index+1 >= len(args) {
				return "", "", false
			}
			jqExpr = args[index+1]
			index++
		case "--cache":
			if index+1 >= len(args) {
				return "", "", false
			}
			index++
		case "-H", "--header", "--hostname", "--preview", "--paginate", "-i", "--silent", "--slurp":
			return "", "", false
		default:
			switch {
			case strings.HasPrefix(arg, "--method="):
				method = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(arg, "--method=")))
			case strings.HasPrefix(arg, "--jq="):
				jqExpr = strings.TrimPrefix(arg, "--jq=")
			case strings.HasPrefix(arg, "--cache="):
			case strings.HasPrefix(arg, "--header="), strings.HasPrefix(arg, "--hostname="), strings.HasPrefix(arg, "--preview="):
				return "", "", false
			case arg == "--paginate" || arg == "-i" || arg == "--silent" || arg == "--slurp":
				return "", "", false
			case strings.HasPrefix(arg, "-"):
				return "", "", false
			case route == "":
				route = arg
			default:
				return "", "", false
			}
		}
	}
	if method != "" && method != "GET" {
		return "", "", false
	}
	return route, jqExpr, route != ""
}

func parsePullAPIRoute(route string) (string, string, int, bool) {
	route = strings.TrimPrefix(strings.TrimSpace(route), "/")
	route = strings.SplitN(route, "?", 2)[0]
	parts := strings.Split(route, "/")
	if len(parts) != 5 || parts[0] != "repos" || parts[3] != "pulls" {
		return "", "", 0, false
	}
	number, err := strconv.Atoi(parts[4])
	if err != nil || number <= 0 {
		return "", "", 0, false
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", "", 0, false
	}
	return parts[1], parts[2], number, true
}

func (a *App) localGHPullAPIResponse(ctx context.Context, repoValue string, number int, requireFresh bool) (map[string]any, error) {
	thread, err := a.localGHThread(ctx, repoValue, "pull_request", number)
	if err != nil {
		return nil, err
	}
	cache, err := a.localGHPullRequestCache(ctx, repoValue, number)
	if err != nil {
		return nil, err
	}
	if requireFresh && !ghPullRequestCacheFresh(cache) {
		return nil, localGHUnsupported(fmt.Errorf("cached pull request detail is stale"))
	}
	raw := decodeRawJSON(cache.Detail.RawJSON)
	row := make(map[string]any, len(raw)+20)
	for key, value := range raw {
		row[key] = value
	}
	if id, ok := restPullID(row["id"], thread.GitHubID); ok {
		row["id"] = id
	}
	row["number"] = thread.Number
	row["state"] = thread.State
	row["title"] = thread.Title
	row["body"] = thread.Body
	row["html_url"] = thread.HTMLURL
	if _, ok := row["draft"]; !ok {
		row["draft"] = thread.IsDraft
	}
	user := rawMap(row, "user")
	if rawString(user, "login") == "" && thread.AuthorLogin != "" {
		user["login"] = thread.AuthorLogin
	}
	if rawString(user, "type") == "" && thread.AuthorType != "" {
		user["type"] = thread.AuthorType
	}
	if len(user) > 0 {
		row["user"] = user
	}
	if _, ok := row["labels"]; !ok {
		row["labels"] = ghLabelsFromJSON(thread.LabelsJSON)
	}
	if _, ok := row["assignees"]; !ok {
		row["assignees"] = ghUsersFromJSON(thread.AssigneesJSON)
	}
	row["created_at"] = thread.CreatedAtGitHub
	row["updated_at"] = firstNonEmpty(thread.UpdatedAtGitHub, thread.UpdatedAt)
	if thread.ClosedAtGitHub != "" {
		row["closed_at"] = thread.ClosedAtGitHub
	}
	if mergedAt := firstNonEmpty(rawString(row, "merged_at"), thread.MergedAtGitHub); mergedAt != "" {
		row["merged_at"] = mergedAt
	}
	if merged, ok := row["merged"].(bool); ok {
		row["merged"] = merged
	} else {
		row["merged"] = strings.TrimSpace(firstNonEmpty(rawString(row, "merged_at"), thread.MergedAtGitHub)) != ""
	}
	row["additions"] = cache.Detail.Additions
	row["deletions"] = cache.Detail.Deletions
	row["changed_files"] = cache.Detail.ChangedFiles
	row["mergeable_state"] = cache.Detail.MergeableState
	row["maintainer_can_modify"] = rawBool(row, "maintainer_can_modify")
	if cache.Detail.BaseSHA != "" || rawString(rawMap(row, "base"), "sha") != "" {
		base := rawMap(row, "base")
		if cache.Detail.BaseSHA != "" {
			base["sha"] = cache.Detail.BaseSHA
		}
		if rawString(base, "ref") == "" {
			base["ref"] = rawString(rawMap(decodeRawJSON(cache.Detail.RawJSON), "base"), "ref")
		}
		if len(rawMap(base, "repo")) == 0 {
			if owner, repoName, err := parseOwnerRepo(repoValue); err == nil {
				base["repo"] = map[string]any{"full_name": owner + "/" + repoName}
			}
		}
		row["base"] = base
	}
	head := rawMap(row, "head")
	if cache.Detail.HeadSHA != "" {
		head["sha"] = cache.Detail.HeadSHA
	}
	if cache.Detail.HeadRef != "" {
		head["ref"] = cache.Detail.HeadRef
	}
	if cache.Detail.HeadRepoFullName != "" {
		repo := rawMap(head, "repo")
		if rawString(repo, "full_name") == "" {
			repo["full_name"] = cache.Detail.HeadRepoFullName
		}
		head["repo"] = repo
	}
	row["head"] = head
	if rawString(row, "url") == "" {
		owner, repoName, err := parseOwnerRepo(repoValue)
		if err == nil {
			row["url"] = fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repoName, number)
		}
	}
	return row, nil
}

func restPullID(rawID any, fallback string) (any, bool) {
	switch typed := rawID.(type) {
	case float64:
		return typed, true
	case int:
		return typed, true
	case int64:
		return typed, true
	case string:
		if value, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return value, true
		}
		if strings.TrimSpace(typed) != "" {
			return typed, true
		}
	}
	if value, err := strconv.ParseInt(strings.TrimSpace(fallback), 10, 64); err == nil {
		return value, true
	}
	return nil, false
}
