// Package github implements the VCS adapter for GitHub. Phase 1 scope:
// read PR metadata, list changed files, upsert a marker-identified PR
// comment. ListReviews / checks / CODEOWNERS land in Phase 2.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"

	"github.com/thefynx/reeve/internal/vcs"
)

// Client wraps a go-github Client. Authentication is via personal access
// token / GITHUB_TOKEN. GitHub App auth lands in Phase 4.
type Client struct {
	gh    *gh.Client
	owner string
	repo  string
}

// New returns a Client. If token is empty, an unauthenticated client is
// returned - useful only for public-repo reads in tests.
func New(ctx context.Context, token, owner, repo string) (*Client, error) {
	if owner == "" || repo == "" {
		return nil, errors.New("github: owner and repo required")
	}
	var httpClient *http.Client
	if token != "" {
		src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(ctx, src)
	}
	return &Client{gh: gh.NewClient(httpClient), owner: owner, repo: repo}, nil
}

// Capabilities returns GitHub's supported comment features.
func (c *Client) Capabilities() vcs.CommentCapabilities {
	return vcs.CommentCapabilities{SupportsEdit: true}
}

// GetPR returns the normalized PR shape.
func (c *Client) GetPR(ctx context.Context, number int) (*vcs.PR, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, c.owner, c.repo, number)
	if err != nil {
		return nil, err
	}
	out := &vcs.PR{
		Number:   pr.GetNumber(),
		HeadSHA:  pr.GetHead().GetSHA(),
		BaseRef:  pr.GetBase().GetRef(),
		Author:   pr.GetUser().GetLogin(),
		OpenedAt: pr.GetCreatedAt().Format("2006-01-02T15:04:05Z"),
		URL:      pr.GetHTMLURL(),
	}
	// IsFork: head repo full name differs from base repo full name.
	if pr.GetHead() != nil && pr.GetHead().GetRepo() != nil && pr.GetBase() != nil && pr.GetBase().GetRepo() != nil {
		out.IsFork = pr.GetHead().GetRepo().GetFullName() != pr.GetBase().GetRepo().GetFullName()
	}
	return out, nil
}

// ListChangedFiles returns the file paths changed in the PR.
func (c *Client) ListChangedFiles(ctx context.Context, number int) ([]string, error) {
	var out []string
	opt := &gh.ListOptions{PerPage: 100}
	for {
		files, resp, err := c.gh.PullRequests.ListFiles(ctx, c.owner, c.repo, number, opt)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.GetFilename() != "" {
				out = append(out, f.GetFilename())
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}

// UpsertComment finds reeve's existing PR comment by marker substring and
// edits it; creates a new one if none exists.
func (c *Client) UpsertComment(ctx context.Context, number int, body, marker string) error {
	if marker == "" {
		return errors.New("marker is required")
	}
	opt := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := c.gh.Issues.ListComments(ctx, c.owner, c.repo, number, opt)
		if err != nil {
			return fmt.Errorf("list comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.GetBody(), marker) {
				_, _, err := c.gh.Issues.EditComment(ctx, c.owner, c.repo, cm.GetID(), &gh.IssueComment{Body: gh.String(body)})
				return err
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	_, _, err := c.gh.Issues.CreateComment(ctx, c.owner, c.repo, number, &gh.IssueComment{Body: gh.String(body)})
	return err
}
