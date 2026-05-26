package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/openclaw/gitcrawl/internal/store"
)

func (a *App) execLocalGHAPI(ctx context.Context, args []string, controls ghShimControls) (bool, error) {
	if host := strings.TrimSpace(os.Getenv("GH_HOST")); host != "" && !strings.EqualFold(host, "github.com") {
		return false, nil
	}
	request, ok := parseLocalGHAPIArgsDetailed(args)
	if !ok {
		return false, nil
	}
	parsed, ok := parseLocalGHAPIRoute(request.Route)
	if !ok {
		return false, nil
	}
	if parsed.Kind != "pull" && (!request.Paginate || !localGHCollectionQuerySupported(parsed.RawQuery)) {
		return false, nil
	}
	if parsed.Kind == "commit_check_runs" && !controls.Cached {
		if _, ok := a.activeGHLivenessTombstone(ctx, args); ok {
			return false, nil
		}
	}
	row, err := a.localGHAPIResponse(ctx, parsed, !controls.Cached)
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
	if strings.TrimSpace(request.JQExpr) != "" {
		projectedOut, projectedErr, err := runGHAPIProjection(stdout, request.JQExpr)
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
	request, ok := parseLocalGHAPIArgsDetailed(args)
	if !ok {
		return "", "", false
	}
	return request.Route, request.JQExpr, true
}

type localGHAPIArgs struct {
	Route    string
	JQExpr   string
	Paginate bool
}

func parseLocalGHAPIArgsDetailed(args []string) (localGHAPIArgs, bool) {
	if len(args) == 0 || args[0] != "api" {
		return localGHAPIArgs{}, false
	}
	method := "GET"
	route := ""
	jqExpr := ""
	paginate := false
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-X", "--method":
			if index+1 >= len(args) {
				return localGHAPIArgs{}, false
			}
			method = strings.ToUpper(strings.TrimSpace(args[index+1]))
			index++
		case "--jq", "-q":
			if index+1 >= len(args) {
				return localGHAPIArgs{}, false
			}
			jqExpr = args[index+1]
			index++
		case "--cache":
			if index+1 >= len(args) {
				return localGHAPIArgs{}, false
			}
			index++
		case "--paginate":
			paginate = true
		case "-H", "--header", "--hostname", "--preview", "-i", "--silent", "--slurp":
			return localGHAPIArgs{}, false
		default:
			switch {
			case strings.HasPrefix(arg, "--method="):
				method = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(arg, "--method=")))
			case strings.HasPrefix(arg, "--jq="):
				jqExpr = strings.TrimPrefix(arg, "--jq=")
			case strings.HasPrefix(arg, "--cache="):
			case strings.HasPrefix(arg, "--header="), strings.HasPrefix(arg, "--hostname="), strings.HasPrefix(arg, "--preview="):
				return localGHAPIArgs{}, false
			case arg == "--paginate":
				paginate = true
			case arg == "-i" || arg == "--silent" || arg == "--slurp":
				return localGHAPIArgs{}, false
			case strings.HasPrefix(arg, "-"):
				return localGHAPIArgs{}, false
			case route == "":
				route = arg
			default:
				return localGHAPIArgs{}, false
			}
		}
	}
	if method != "" && method != "GET" {
		return localGHAPIArgs{}, false
	}
	if route == "" {
		return localGHAPIArgs{}, false
	}
	return localGHAPIArgs{Route: route, JQExpr: jqExpr, Paginate: paginate}, true
}

type localGHAPIRoute struct {
	Kind     string
	Owner    string
	Repo     string
	Number   int
	SHA      string
	RawQuery string
}

func parseLocalGHAPIRoute(route string) (localGHAPIRoute, bool) {
	route = strings.TrimPrefix(strings.TrimSpace(route), "/")
	rawQuery := ""
	if before, after, found := strings.Cut(route, "?"); found {
		route = before
		rawQuery = after
	}
	parts := strings.Split(route, "/")
	if len(parts) < 5 || parts[0] != "repos" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return localGHAPIRoute{}, false
	}
	parsed := localGHAPIRoute{Owner: parts[1], Repo: parts[2], RawQuery: rawQuery}
	if parts[3] == "pulls" {
		number, err := strconv.Atoi(parts[4])
		if err != nil || number <= 0 {
			return localGHAPIRoute{}, false
		}
		parsed.Number = number
		if len(parts) == 5 {
			parsed.Kind = "pull"
			return parsed, true
		}
		if len(parts) == 6 && (parts[5] == "files" || parts[5] == "commits") {
			parsed.Kind = "pull_" + parts[5]
			return parsed, true
		}
	}
	if len(parts) == 6 && parts[3] == "commits" && parts[5] == "check-runs" && strings.TrimSpace(parts[4]) != "" {
		parsed.Kind = "commit_check_runs"
		parsed.SHA = parts[4]
		return parsed, true
	}
	return localGHAPIRoute{}, false
}

func localGHCollectionQuerySupported(rawQuery string) bool {
	rawQuery = strings.TrimSpace(rawQuery)
	if rawQuery == "" {
		return true
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}
	for key := range values {
		if key != "per_page" {
			return false
		}
	}
	return true
}

func localGHCollectionPerPage(rawQuery string) int {
	const defaultPerPage = 30
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return defaultPerPage
	}
	raw := strings.TrimSpace(values.Get("per_page"))
	if raw == "" {
		return defaultPerPage
	}
	perPage, err := strconv.Atoi(raw)
	if err != nil || perPage <= 0 {
		return defaultPerPage
	}
	if perPage > 100 {
		return 100
	}
	return perPage
}

func parsePullAPIRoute(route string) (string, string, int, bool) {
	parsed, ok := parseLocalGHAPIRoute(route)
	if !ok || parsed.Kind != "pull" {
		return "", "", 0, false
	}
	return parsed.Owner, parsed.Repo, parsed.Number, true
}

func (a *App) localGHAPIResponse(ctx context.Context, route localGHAPIRoute, requireFresh bool) (any, error) {
	repoValue := route.Owner + "/" + route.Repo
	switch route.Kind {
	case "pull":
		return a.localGHPullAPIResponse(ctx, repoValue, route.Number, requireFresh)
	case "pull_files", "pull_commits":
		cache, err := a.localGHPullRequestCache(ctx, repoValue, route.Number)
		if err != nil {
			return nil, err
		}
		if requireFresh && !ghPullRequestCacheFresh(cache) {
			return nil, localGHUnsupported(fmt.Errorf("cached pull request detail is stale"))
		}
		if route.Kind == "pull_files" {
			if !pullFilesHaveRawJSON(cache.Files) {
				return nil, localGHUnsupported(fmt.Errorf("cached pull request files lack raw API rows"))
			}
			if len(cache.Files) > localGHCollectionPerPage(route.RawQuery) {
				return nil, localGHUnsupported(fmt.Errorf("cached pull request files span multiple pages"))
			}
			return localGHPullFilesAPIResponse(cache.Files), nil
		}
		if !pullCommitsHaveRawJSON(cache.Commits) {
			return nil, localGHUnsupported(fmt.Errorf("cached pull request commits lack raw API rows"))
		}
		if len(cache.Commits) > localGHCollectionPerPage(route.RawQuery) {
			return nil, localGHUnsupported(fmt.Errorf("cached pull request commits span multiple pages"))
		}
		return localGHPullCommitsAPIResponse(cache.Commits), nil
	case "commit_check_runs":
		return a.localGHCommitCheckRunsAPIResponse(ctx, repoValue, route.SHA, route.RawQuery, requireFresh)
	default:
		return nil, localGHUnsupported(fmt.Errorf("unsupported local API route"))
	}
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

func localGHPullFilesAPIResponse(files []store.PullRequestFile) []map[string]any {
	out := make([]map[string]any, 0, len(files))
	for _, file := range files {
		row := decodeRawJSON(file.RawJSON)
		if rawString(row, "filename") == "" {
			row["filename"] = file.Path
		}
		if rawString(row, "status") == "" {
			row["status"] = file.Status
		}
		row["additions"] = firstNonZero(rawInt(row, "additions"), file.Additions)
		row["deletions"] = firstNonZero(rawInt(row, "deletions"), file.Deletions)
		row["changes"] = firstNonZero(rawInt(row, "changes"), file.Changes)
		if rawString(row, "previous_filename") == "" && file.PreviousPath != "" {
			row["previous_filename"] = file.PreviousPath
		}
		if rawString(row, "patch") == "" && file.Patch != "" {
			row["patch"] = file.Patch
		}
		out = append(out, row)
	}
	return out
}

func localGHPullCommitsAPIResponse(commits []store.PullRequestCommit) []map[string]any {
	out := make([]map[string]any, 0, len(commits))
	for _, commit := range commits {
		row := decodeRawJSON(commit.RawJSON)
		if rawString(row, "sha") == "" {
			row["sha"] = commit.SHA
		}
		if rawString(row, "html_url") == "" && commit.HTMLURL != "" {
			row["html_url"] = commit.HTMLURL
		}
		commitMap := rawMap(row, "commit")
		if rawString(commitMap, "message") == "" && commit.Message != "" {
			commitMap["message"] = commit.Message
		}
		author := rawMap(commitMap, "author")
		if rawString(author, "name") == "" && commit.AuthorName != "" {
			author["name"] = commit.AuthorName
		}
		if rawString(author, "date") == "" && commit.CommittedAt != "" {
			author["date"] = commit.CommittedAt
		}
		if len(author) > 0 {
			commitMap["author"] = author
		}
		if len(commitMap) > 0 {
			row["commit"] = commitMap
		}
		user := rawMap(row, "author")
		if rawString(user, "login") == "" && commit.AuthorLogin != "" {
			user["login"] = commit.AuthorLogin
		}
		if len(user) > 0 {
			row["author"] = user
		}
		out = append(out, row)
	}
	return out
}

func (a *App) localGHCommitCheckRunsAPIResponse(ctx context.Context, repoValue, sha, rawQuery string, requireFresh bool) (map[string]any, error) {
	cache, err := a.localGHPullRequestCacheByHeadSHA(ctx, repoValue, sha)
	if err != nil {
		return nil, err
	}
	if requireFresh && !ghPullRequestCacheFresh(cache) {
		return nil, localGHUnsupported(fmt.Errorf("cached pull request detail is stale"))
	}
	rows := localGHCheckRunsAPIRows(cache.Checks)
	if !pullChecksHaveRawJSON(cache.Checks) {
		return nil, localGHUnsupported(fmt.Errorf("cached check runs lack raw API rows"))
	}
	if len(rows) > localGHCollectionPerPage(rawQuery) {
		return nil, localGHUnsupported(fmt.Errorf("cached check runs span multiple pages"))
	}
	return map[string]any{"total_count": len(rows), "check_runs": rows}, nil
}

func pullFilesHaveRawJSON(files []store.PullRequestFile) bool {
	for _, file := range files {
		if len(decodeRawJSON(file.RawJSON)) == 0 {
			return false
		}
	}
	return true
}

func pullCommitsHaveRawJSON(commits []store.PullRequestCommit) bool {
	for _, commit := range commits {
		if len(decodeRawJSON(commit.RawJSON)) == 0 {
			return false
		}
	}
	return true
}

func pullChecksHaveRawJSON(checks []store.PullRequestCheck) bool {
	for _, check := range checks {
		if len(decodeRawJSON(check.RawJSON)) == 0 {
			return false
		}
	}
	return true
}

func (a *App) localGHPullRequestCacheByHeadSHA(ctx context.Context, repoValue, sha string) (store.PullRequestCache, error) {
	owner, repoName, err := parseOwnerRepo(repoValue)
	if err != nil {
		return store.PullRequestCache{}, err
	}
	rt, err := a.openLocalRuntimeReadOnly(ctx)
	if err != nil {
		return store.PullRequestCache{}, localGHUnsupported(err)
	}
	defer rt.Store.Close()
	repo, err := rt.repository(ctx, owner, repoName)
	if err != nil {
		return store.PullRequestCache{}, localGHUnsupported(err)
	}
	number, err := localGHPullNumberByHeadSHA(ctx, rt.Store, repo.ID, sha)
	if err != nil {
		return store.PullRequestCache{}, localGHUnsupported(err)
	}
	cache, err := rt.Store.PullRequestCache(ctx, repo.ID, number)
	if err != nil {
		return store.PullRequestCache{}, localGHUnsupported(err)
	}
	checks, err := rt.Store.PullRequestChecksAPIOrder(ctx, cache.Detail.ThreadID)
	if err != nil {
		return store.PullRequestCache{}, localGHUnsupported(err)
	}
	cache.Checks = checks
	return cache, nil
}

func localGHPullNumberByHeadSHA(ctx context.Context, st *store.Store, repoID int64, sha string) (int, error) {
	sha = strings.ToLower(strings.TrimSpace(sha))
	if sha == "" {
		return 0, fmt.Errorf("cached commit SHA was not found")
	}
	number, ok, err := localGHPullNumberByHeadSHAQuery(ctx, st, repoID, sha)
	if err != nil || ok {
		return number, err
	}
	return 0, fmt.Errorf("cached commit SHA was not found")
}

func localGHPullNumberByHeadSHAQuery(ctx context.Context, st *store.Store, repoID int64, sha string) (int, bool, error) {
	query := `select number, lower(head_sha) from pull_request_details where repo_id = ? and lower(head_sha) = ? order by number`
	args := []any{repoID, sha}
	rows, err := st.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	number := 0
	matchedSHA := ""
	for rows.Next() {
		var rowNumber int
		var rowSHA string
		if err := rows.Scan(&rowNumber, &rowSHA); err != nil {
			return 0, false, err
		}
		if number == 0 {
			number = rowNumber
			matchedSHA = rowSHA
			continue
		}
		if !strings.EqualFold(matchedSHA, rowSHA) {
			return 0, false, fmt.Errorf("cached commit SHA is ambiguous")
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	return number, number != 0, nil
}

func localGHCheckRunsAPIRows(checks []store.PullRequestCheck) []map[string]any {
	out := make([]map[string]any, 0, len(checks))
	for _, check := range checks {
		row := decodeRawJSON(check.RawJSON)
		if _, ok := row["name"]; !ok {
			row["name"] = check.Name
		}
		if _, ok := row["status"]; !ok {
			row["status"] = check.Status
		}
		if _, ok := row["conclusion"]; !ok && check.Conclusion != "" {
			row["conclusion"] = check.Conclusion
		}
		if _, ok := row["details_url"]; !ok && check.DetailsURL != "" {
			row["details_url"] = check.DetailsURL
		}
		if _, ok := row["html_url"]; !ok && check.DetailsURL != "" {
			row["html_url"] = check.DetailsURL
		}
		if _, ok := row["started_at"]; !ok && check.StartedAt != "" {
			row["started_at"] = check.StartedAt
		}
		if _, ok := row["completed_at"]; !ok && check.CompletedAt != "" {
			row["completed_at"] = check.CompletedAt
		}
		if check.WorkflowName != "" {
			suite := rawMap(row, "check_suite")
			app := rawMap(suite, "app")
			if rawString(app, "name") == "" {
				app["name"] = check.WorkflowName
			}
			suite["app"] = app
			row["check_suite"] = suite
		}
		out = append(out, row)
	}
	return out
}

func firstNonZero(first, second int) int {
	if first != 0 {
		return first
	}
	return second
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
