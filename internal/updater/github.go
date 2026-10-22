package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"lara4ever/internal/i18n"
)

// GitHubRepo is the public repo whose Releases we consult.
const GitHubRepo = "thayronarrais/laraenv"

type ghAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

// fetchLatestRelease hits the GitHub API for the latest non-draft release of
// the public LaraEnv repo. Returns the parsed Release shape used by the rest
// of the package, or a friendly error.
func fetchLatestRelease(ctx context.Context, client *http.Client) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GitHubRepo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "LaraEnv-Updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.updater.reachServerFailed", nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, i18n.Errorf("errors.updater.noPublishedRelease", nil)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, i18n.Errorf("errors.updater.rateLimited", nil)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, i18n.Errorf("errors.updater.githubApiError", map[string]any{"status": resp.StatusCode, "body": string(body)})
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, i18n.Wrapf(err, "errors.updater.invalidGithubResponse", nil)
	}
	if rel.Draft {
		return nil, i18n.Errorf("errors.updater.releaseIsDraft", nil)
	}

	out := &Release{
		Version:     strings.TrimPrefix(rel.TagName, "v"),
		Notes:       rel.Body,
		PublishedAt: rel.PublishedAt.Format(time.RFC3339),
	}
	applyAssets(out, rel.Assets)
	// Whether the release carries what this copy can install is Manager.Check's
	// call — it is the only place that knows if we are portable.
	return out, nil
}

// fetchSha256 downloads the .msi.sha256 sidecar (a one-line text file in the
// shape "<hex>  LaraEnv-<version>.msi"). Returns the hex digest.
func fetchSha256(ctx context.Context, client *http.Client, url string) (string, error) {
	if url == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LaraEnv-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", i18n.Errorf("errors.updater.sha256FetchFailed", map[string]any{"status": resp.StatusCode})
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	// Accept either "<hex>" or "<hex>  filename" — split on whitespace.
	fields := strings.Fields(strings.TrimSpace(string(body)))
	if len(fields) == 0 {
		return "", i18n.Errorf("errors.updater.emptySha256File", nil)
	}
	hex := strings.ToLower(fields[0])
	if len(hex) != 64 {
		return "", i18n.Errorf("errors.updater.invalidSha256Length", map[string]any{"length": len(hex)})
	}
	return hex, nil
}

// applyAssets sorts a release's attachments into the fields the updater uses.
// The checksum suffixes are matched before the payload suffixes they extend,
// so "LaraEnv.exe.sha256" is never mistaken for the executable.
func applyAssets(out *Release, assets []ghAsset) {
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		switch {
		case strings.HasSuffix(name, ".msi.sha256"):
			out.Sha256URL = a.DownloadURL
		case strings.HasSuffix(name, ".exe.sha256"):
			out.ExeSha256URL = a.DownloadURL
		case strings.HasSuffix(name, ".msi"):
			out.MsiURL = a.DownloadURL
			out.MsiName = a.Name
			out.SizeBytes = a.Size
		case strings.HasSuffix(name, ".exe"):
			out.ExeURL = a.DownloadURL
			out.ExeName = a.Name
			out.ExeSizeBytes = a.Size
		}
	}
}
