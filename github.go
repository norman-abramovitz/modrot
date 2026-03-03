package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// RepoStatus holds the GitHub status for a single repository.
type RepoStatus struct {
	Module     Module
	IsArchived bool
	ArchivedAt time.Time
	PushedAt   time.Time
	NotFound   bool
	Error      string
}

// getGHToken retrieves the GitHub auth token via `gh auth token`.
func getGHToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub token (is gh installed and authenticated?): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// graphQLRequest represents a GitHub GraphQL request body.
type graphQLRequest struct {
	Query string `json:"query"`
}

// CheckRepos queries GitHub for the archived status of the given modules.
// Modules are batched into groups of batchSize per GraphQL request.
func CheckRepos(modules []Module, batchSize int) ([]RepoStatus, error) {
	if len(modules) == 0 {
		return nil, nil
	}

	token, err := getGHToken()
	if err != nil {
		return nil, err
	}

	var results []RepoStatus
	for i := 0; i < len(modules); i += batchSize {
		end := i + batchSize
		if end > len(modules) {
			end = len(modules)
		}
		batch := modules[i:end]

		statuses, err := queryBatch(token, batch)
		if err != nil {
			return nil, fmt.Errorf("querying batch starting at index %d: %w", i, err)
		}
		results = append(results, statuses...)
	}
	return results, nil
}

// buildGraphQLQuery constructs a batched GraphQL query for the given modules.
func buildGraphQLQuery(modules []Module) string {
	var qb strings.Builder
	qb.WriteString("{\n")
	for i, m := range modules {
		fmt.Fprintf(&qb, "  r%d: repository(owner: %q, name: %q) {\n", i, m.Owner, m.Repo)
		qb.WriteString("    isArchived\n")
		qb.WriteString("    archivedAt\n")
		qb.WriteString("    pushedAt\n")
		qb.WriteString("  }\n")
	}
	qb.WriteString("}\n")
	return qb.String()
}

// gqlResponse represents the GitHub GraphQL API response.
type gqlResponse struct {
	Data   map[string]*repoData `json:"data"`
	Errors []struct {
		Message string   `json:"message"`
		Path    []string `json:"path"`
	} `json:"errors"`
}

// parseGraphQLResponse converts a parsed GraphQL response into RepoStatus results.
func parseGraphQLResponse(gqlResp gqlResponse, modules []Module) []RepoStatus {
	errorAliases := make(map[string]string)
	for _, e := range gqlResp.Errors {
		if len(e.Path) > 0 {
			errorAliases[e.Path[0]] = e.Message
		}
	}

	results := make([]RepoStatus, len(modules))
	for i, m := range modules {
		alias := fmt.Sprintf("r%d", i)
		rs := RepoStatus{Module: m}

		if errMsg, ok := errorAliases[alias]; ok {
			rs.NotFound = true
			rs.Error = errMsg
		} else if rd, ok := gqlResp.Data[alias]; ok && rd != nil {
			rs.IsArchived = rd.IsArchived
			if rd.ArchivedAt != "" {
				rs.ArchivedAt, _ = time.Parse(time.RFC3339, rd.ArchivedAt)
			}
			if rd.PushedAt != "" {
				rs.PushedAt, _ = time.Parse(time.RFC3339, rd.PushedAt)
			}
		} else {
			rs.NotFound = true
			rs.Error = "repository not found"
		}

		results[i] = rs
	}
	return results
}

func queryBatch(token string, modules []Module) ([]RepoStatus, error) {
	query := buildGraphQLQuery(modules)

	reqBody, err := json.Marshal(graphQLRequest{Query: query})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return parseGraphQLResponse(gqlResp, modules), nil
}

type repoData struct {
	IsArchived bool   `json:"isArchived"`
	ArchivedAt string `json:"archivedAt"`
	PushedAt   string `json:"pushedAt"`
}
