package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// graphQLRequest is the request body for the GitHub GraphQL API.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// doGraphQL posts a query/mutation to the GitHub GraphQL endpoint and returns
// the raw `data` payload. It errors if the transport fails, the HTTP status is
// >= 400, or the response carries GraphQL errors.
func doGraphQL(ctx context.Context, client *GitHubClient, query string, variables map[string]any) (json.RawMessage, error) {
	reqBody, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	resp, err := client.Do(ctx, http.MethodPost, "/graphql", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GraphQL request returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("GraphQL error: %s", strings.Join(msgs, "; "))
	}
	return envelope.Data, nil
}

const reviewThreadsQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviewThreads(first:100,after:$cursor){
        pageInfo{hasNextPage endCursor}
        nodes{id isResolved comments(first:100){nodes{databaseId}}}
      }
    }
  }
}`

const resolveThreadMutation = `mutation($threadId:ID!){
  resolveReviewThread(input:{threadId:$threadId}){thread{id isResolved}}
}`

// reviewThread is a PR review thread along with whether it is resolved.
type reviewThread struct {
	ID         string
	IsResolved bool
}

// findReviewThreadByComment returns the review thread on owner/repo#number that
// contains the review comment with the given database ID. It paginates over the
// PR's review threads and returns nil (no error) if no matching thread exists.
// Scoping the lookup to owner/repo is what keeps thread resolution restricted
// to the configured repository.
func findReviewThreadByComment(ctx context.Context, client *GitHubClient, owner, repo string, number, commentID int) (*reviewThread, error) {
	var cursor *string
	for {
		vars := map[string]any{"owner": owner, "repo": repo, "number": number}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		data, err := doGraphQL(ctx, client, reviewThreadsQuery, vars)
		if err != nil {
			return nil, err
		}

		var parsed struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int64 `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse review threads: %w", err)
		}

		for _, n := range parsed.Repository.PullRequest.ReviewThreads.Nodes {
			for _, c := range n.Comments.Nodes {
				if c.DatabaseID == int64(commentID) {
					return &reviewThread{ID: n.ID, IsResolved: n.IsResolved}, nil
				}
			}
		}

		pi := parsed.Repository.PullRequest.ReviewThreads.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			return nil, nil
		}
		next := pi.EndCursor
		cursor = &next
	}
}

// resolveReviewThreadByID resolves a review thread by its GraphQL node ID and
// returns the resulting resolved state.
func resolveReviewThreadByID(ctx context.Context, client *GitHubClient, threadID string) (bool, error) {
	data, err := doGraphQL(ctx, client, resolveThreadMutation, map[string]any{"threadId": threadID})
	if err != nil {
		return false, err
	}

	var parsed struct {
		ResolveReviewThread struct {
			Thread struct {
				IsResolved bool `json:"isResolved"`
			} `json:"thread"`
		} `json:"resolveReviewThread"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, fmt.Errorf("failed to parse resolve response: %w", err)
	}
	return parsed.ResolveReviewThread.Thread.IsResolved, nil
}
