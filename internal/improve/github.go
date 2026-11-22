package improve

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub is the small part of GitHub's REST API the loop uses: reading
// files, opening a pull request from a new branch, following it, and
// comparing commits. The token needs contents and pull requests write on
// the public repo and the overlay; it is never logged
type GitHub struct {
	// API is the API's base URL; https://api.github.com when empty
	API    string
	Token  string
	Client *http.Client
}

// errNotFound is a 404
var errNotFound = errors.New("not found")

// maxBody bounds a response read
const maxBody = 8 << 20

func (g *GitHub) do(ctx context.Context, method, p string, in, out any) error {
	base := g.API
	if base == "" {
		base = "https://api.github.com"
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(base, "/")+p, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	c := g.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, p, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, p, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s %s: %w", method, p, errNotFound)
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(b, &e)
		return fmt.Errorf("%s %s: %s %s", method, p, resp.Status, e.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%s %s: %w", method, p, err)
	}

	return nil
}

// escapePath escapes each segment of a repo path
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// DefaultBranch is the repo's default branch
func (g *GitHub) DefaultBranch(ctx context.Context, repo string) (string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.do(ctx, http.MethodGet, "/repos/"+repo, nil, &r); err != nil {
		return "", err
	}
	if r.DefaultBranch == "" {
		return "", fmt.Errorf("%s has no default branch", repo)
	}
	return r.DefaultBranch, nil
}

// File reads a file at ref: its contents and blob sha, or found false
func (g *GitHub) File(ctx context.Context, repo, file, ref string) (content, sha string, found bool, err error) {
	var r struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
	}
	err = g.do(ctx, http.MethodGet, "/repos/"+repo+"/contents/"+escapePath(file)+"?ref="+url.QueryEscape(ref), nil, &r)
	if errors.Is(err, errNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if r.Type != "file" || r.Encoding != "base64" {
		return "", "", false, fmt.Errorf("%s in %s isn't a file GitHub returned whole", file, repo)
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(r.Content, "\n", ""))
	if err != nil {
		return "", "", false, fmt.Errorf("decode %s: %w", file, err)
	}

	return string(b), r.SHA, true, nil
}

// branchSHA is the commit a branch points at
func (g *GitHub) branchSHA(ctx context.Context, repo, branch string) (string, error) {
	var r struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := g.do(ctx, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+escapePath(branch), nil, &r); err != nil {
		return "", err
	}
	return r.Object.SHA, nil
}

// PullRequest is what the loop reads of one
type PullRequest struct {
	Number   int       `json:"number"`
	URL      string    `json:"html_url"`
	State    string    `json:"state"`
	Merged   bool      `json:"merged"`
	MergedAt time.Time `json:"merged_at"`
	MergeSHA string    `json:"merge_commit_sha"`
}

// OpenPR creates branch from base, writes the files on it in one commit
// each, and opens a draft pull request labelled label
func (g *GitHub) OpenPR(ctx context.Context, repo, base, branch, title, body, label string, files []File, shas map[string]string) (PullRequest, error) {
	head, err := g.branchSHA(ctx, repo, base)
	if err != nil {
		return PullRequest{}, err
	}
	if err := g.do(ctx, http.MethodPost, "/repos/"+repo+"/git/refs", map[string]string{"ref": "refs/heads/" + branch, "sha": head}, nil); err != nil {
		return PullRequest{}, err
	}
	for _, f := range files {
		msg := "bud improve: " + title
		p := "/repos/" + repo + "/contents/" + escapePath(f.Path)
		if f.After == nil {
			err = g.do(ctx, http.MethodDelete, p, map[string]string{"message": msg, "sha": shas[f.Path], "branch": branch}, nil)
		} else {
			in := map[string]string{"message": msg, "content": base64.StdEncoding.EncodeToString([]byte(*f.After)), "branch": branch}
			if s := shas[f.Path]; s != "" {
				in["sha"] = s
			}
			err = g.do(ctx, http.MethodPut, p, in, nil)
		}
		if err != nil {
			return PullRequest{}, err
		}
	}
	var pr PullRequest
	if err := g.do(ctx, http.MethodPost, "/repos/"+repo+"/pulls", map[string]any{"title": title, "head": branch, "base": base, "body": body, "draft": true}, &pr); err != nil {
		return PullRequest{}, err
	}
	if label != "" {
		// a missing label is cosmetic; the draft PR stands
		_ = g.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/labels", repo, pr.Number), map[string][]string{"labels": {label}}, nil)
	}

	return pr, nil
}

// PR reads a pull request
func (g *GitHub) PR(ctx context.Context, repo string, n int) (PullRequest, error) {
	var pr PullRequest
	err := g.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, n), nil, &pr)
	return pr, err
}

// Contains says whether ref's history holds commit
func (g *GitHub) Contains(ctx context.Context, repo, ref, commit string) (bool, error) {
	var r struct {
		Status string `json:"status"`
	}
	err := g.do(ctx, http.MethodGet, "/repos/"+repo+"/compare/"+url.PathEscape(commit)+"..."+url.PathEscape(ref), nil, &r)
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return r.Status == "ahead" || r.Status == "identical", nil
}

// LatestRelease is a repo's newest release: its tag and its title
func (g *GitHub) LatestRelease(ctx context.Context, repo string) (tag, name string, err error) {
	var r struct {
		Tag  string `json:"tag_name"`
		Name string `json:"name"`
	}
	if err := g.do(ctx, http.MethodGet, "/repos/"+repo+"/releases/latest", nil, &r); err != nil {
		return "", "", err
	}
	return r.Tag, r.Name, nil
}
