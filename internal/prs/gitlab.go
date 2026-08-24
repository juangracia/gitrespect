package prs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	gitlabDefaultBase = "https://gitlab.com/api/v4"
	gitlabPerPage     = 100
	// gitlabMaxPages guards against a runaway loop. At 100 per page this is
	// 50k merge requests, far past any realistic reporting window.
	gitlabMaxPages = 500
)

// gitlabProvider reads merge requests from a whole group in one paginated
// query.
//
// The group scoped endpoint is the only sane primitive at organisation scale:
// GET /groups/:id/merge_requests covers every project under the group,
// including subgroups, so a 200 project group costs the same number of calls
// as a 2 project one. Walking projects individually would multiply the request
// count by the project count and is deliberately not implemented, not even as
// a fallback.
type gitlabProvider struct {
	scope string
	tr    transport
}

func newGitLabProvider(scope string, tr transport) *gitlabProvider {
	return &gitlabProvider{scope: scope, tr: tr}
}

func (g *gitlabProvider) Name() string  { return ProviderGitLab }
func (g *gitlabProvider) Scope() string { return g.scope }

func (g *gitlabProvider) Fetch(ctx context.Context, since, until time.Time) (Fetched, error) {
	// A group path contains slashes, which have to survive as a single path
	// segment: bunn-digital/web becomes bunn-digital%2Fweb.
	path := "groups/" + url.PathEscape(strings.Trim(g.scope, "/")) + "/merge_requests"

	base := url.Values{}
	base.Set("scope", "all")
	base.Set("state", "all")
	base.Set("order_by", "created_at")
	base.Set("sort", "asc")
	base.Set("created_after", since.UTC().Format(time.RFC3339))
	base.Set("created_before", until.UTC().Format(time.RFC3339))

	var out Fetched
	seen := map[string]bool{}

	requests, hitCap, err := paginate(ctx, g.tr, path, base, gitlabPerPage, gitlabMaxPages,
		func(body []byte) (int, error) {
			var raw []gitlabMR
			if err := json.Unmarshal(body, &raw); err != nil {
				return 0, fmt.Errorf("parsing gitlab merge request page: %w", err)
			}
			for _, m := range raw {
				mr, ok := m.normalize()
				if !ok {
					continue
				}
				// The window is enforced here as well as in the query, so a
				// server that ignores created_after cannot inflate the report.
				if mr.CreatedAt.Before(since) || mr.CreatedAt.After(until) {
					continue
				}
				if seen[mr.ID] {
					continue
				}
				seen[mr.ID] = true
				out.Items = append(out.Items, mr)
			}
			return len(raw), nil
		})

	out.Requests = requests
	if err != nil {
		return out, err
	}
	if hitCap {
		out.Truncated = true
		out.Note = fmt.Sprintf("stopped after %d pages (%d merge requests); narrow the window with --since/--until",
			gitlabMaxPages, len(out.Items))
	}
	return out, nil
}

// gitlabMR is the subset of GitLab's merge request payload this report needs.
type gitlabMR struct {
	ID         int    `json:"id"`
	IID        int    `json:"iid"`
	Title      string `json:"title"`
	WebURL     string `json:"web_url"`
	State      string `json:"state"`
	CreatedAt  string `json:"created_at"`
	MergedAt   string `json:"merged_at"`
	ProjectID  int    `json:"project_id"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
	Author struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		// GitLab omits both of these on most instances. When one is present
		// it upgrades identity matching from a heuristic to an exact match,
		// so it is worth reading.
		Email       string `json:"email"`
		PublicEmail string `json:"public_email"`
	} `json:"author"`
}

func (m gitlabMR) normalize() (MergeRequest, bool) {
	created, err := parseAPITime(m.CreatedAt)
	if err != nil {
		return MergeRequest{}, false
	}
	merged, _ := parseAPITime(m.MergedAt)

	email := strings.TrimSpace(m.Author.Email)
	if email == "" {
		email = strings.TrimSpace(m.Author.PublicEmail)
	}

	id := strconv.Itoa(m.ID)
	if m.ID == 0 {
		id = m.WebURL
	}

	return MergeRequest{
		ID:          id,
		Title:       m.Title,
		URL:         m.WebURL,
		Project:     m.projectPath(),
		AuthorEmail: email,
		AuthorUser:  m.Author.Username,
		AuthorName:  m.Author.Name,
		CreatedAt:   created,
		MergedAt:    merged,
		State:       m.State,
	}, true
}

// projectPath recovers "group/project" from the reference "group/project!42",
// falling back to the web URL and finally to the numeric project id.
func (m gitlabMR) projectPath() string {
	if ref := m.References.Full; ref != "" {
		if path, _, ok := strings.Cut(ref, "!"); ok && path != "" {
			return path
		}
	}
	if m.WebURL != "" {
		if u, err := url.Parse(m.WebURL); err == nil {
			if path, _, ok := strings.Cut(strings.Trim(u.Path, "/"), "/-/"); ok {
				return path
			}
		}
	}
	if m.ProjectID != 0 {
		return "project/" + strconv.Itoa(m.ProjectID)
	}
	return ""
}

// parseAPITime accepts the timestamp shapes GitLab and GitHub emit. An empty
// value is not an error: it just means "not merged".
func parseAPITime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

// gitlabBaseURL normalizes an override so users can pass either a bare host or
// a full API root for a self-hosted instance.
func gitlabBaseURL(override string) string {
	override = strings.TrimSpace(strings.TrimSuffix(override, "/"))
	if override == "" {
		return gitlabDefaultBase
	}
	if strings.Contains(override, "/api/v") {
		return override
	}
	return override + "/api/v4"
}
