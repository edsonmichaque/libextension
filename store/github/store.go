package extension

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

var _ Store = &GitHubStore{}

// GitHubStore implements the Store interface for GitHub-hosted plugins
type GitHubStore struct {
	client *github.Client
	topic  string
	prefix string
	log    logr.Logger
}

// NewGitHubStore creates a new GitHub plugin store
func NewGitHubStore(token string, logger logr.Logger) *GitHubStore {
	var client *github.Client

	if token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		tc := oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(tc)
	} else {
		client = github.NewClient(nil)
	}

	return &GitHubStore{
		client: client,
		log:    logger,
	}
}

// Setup configures the store with specific parameters
func (s *GitHubStore) Setup(config StoreConfig) error {
	topic, ok := config["topic"].(string)
	if !ok || topic == "" {
		return fmt.Errorf("topic is required")
	}

	s.topic = topic

	prefix, ok := config["prefix"].(string)
	if !ok || prefix == "" {
		return fmt.Errorf("prefix is required")
	}

	s.prefix = prefix

	return nil
}

// Fetch retrieves information about a specific plugin
func (s *GitHubStore) Fetch(ctx context.Context, name string, version string) (*Info, error) {
	s.log.Info("starting fetch", "name", name, "version", version)

	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		s.log.Error(nil, "invalid name format", "name", name)
		return nil, fmt.Errorf("plugin name must be in format 'owner/name'")
	}

	pluginName := parts[1]
	if !strings.HasPrefix(parts[1], s.prefix) {
		s.log.Error(nil, "invalid plugin name prefix", "expected", s.prefix, "got", pluginName)
		return nil, fmt.Errorf("plugin name must have second part starting with %s", s.prefix)
	}

	// Extract owner and repo name
	parts = strings.SplitN(strings.TrimPrefix(name, s.prefix), "/", 2)
	if len(parts) != 2 {
		s.log.Error(nil, "invalid plugin name format", "name", name)
		return nil, fmt.Errorf("invalid plugin name format, expected %s<owner>/<repo>", s.prefix)
	}

	owner, repoName := parts[0], parts[1]
	s.log.Info("parsed plugin name", "owner", owner, "repo", repoName)

	repo, _, err := s.client.Repositories.Get(ctx, owner, repoName)
	if err != nil {
		s.log.Error(err, "failed to fetch repository", "owner", owner, "repo", repoName)
		return nil, fmt.Errorf("failed to fetch repository: %w", err)
	}

	s.log.Info("successfully fetched repository",
		"owner", owner,
		"repo", repoName,
		"description", repo.GetDescription())

	var releaseVersion string
	var assetNames []string
	rt := "exec" // default

	s.log.Info("releases url", "url", repo.GetReleasesURL())

	// Check releases first
	var release *github.RepositoryRelease
	if version == "" || version == "latest" {
		release, _, err = s.client.Repositories.GetLatestRelease(ctx, owner, repoName)
		if err != nil {
			s.log.Error(err, "failed to fetch latest release", "owner", owner, "repo", repoName)
			return nil, fmt.Errorf("failed to fetch latest release: %w", err)
		}
	} else {
		release, _, err = s.client.Repositories.GetReleaseByTag(ctx, owner, repoName, version)
		if err != nil {
			s.log.Error(err, "failed to fetch release by tag", "owner", owner, "repo", repoName, "tag", version)
			return nil, fmt.Errorf("failed to fetch release by tag: %w", err)
		}
	}

	// Helper function to get asset names from a release
	getAssetNames := func() []string {
		for _, asset := range release.Assets {
			assetNames = append(assetNames, asset.GetName())
		}

		return assetNames
	}

	var content interface{}

	if err == nil && release != nil {
		s.log.Info("found release", "tag", release.GetTagName(), "assets", len(release.Assets), "created_at", release.GetCreatedAt().String())

		releaseVersion = release.GetTagName()

		match, rtAsset, err := FindAsset(
			s.prefix,
			repoName,
			releaseVersion,
			runtime.GOOS,
			runtime.GOARCH,
			getAssetNames,
		)
		if err != nil {
			s.log.Error(err, "error finding matching asset")
			return nil, err
		}

		s.log.Info("found matching asset", "name", match, "runtime", rtAsset)

		rt = rtAsset

		// Download the matching asset
		for _, asset := range release.Assets {
			if asset.GetName() == match {
				s.log.Info("downloading asset", "name", asset.GetName(), "size", asset.GetSize())

				httpclient := s.client.Client()

				rc, resp, err := s.client.Repositories.DownloadReleaseAsset(ctx, owner, repoName, asset.GetID(), httpclient)
				if err != nil {
					return nil, fmt.Errorf("failed to download asset: %w", err)
				}

				s.log.Info("resp", "resp", resp)

				//defer rc.Close()

				s.log.Info("rc", "rc", rc)

				content, err = io.ReadAll(rc)
				if err != nil {
					return nil, fmt.Errorf("failed to read asset content: %w", err)
				}

				break
			}
		}
	} else {
		s.log.Info("no release found", "err", err)
		// Fallback to checking root directory if no release found
		rc, resp, err := s.client.Repositories.DownloadContents(ctx, owner, repoName, fmt.Sprintf("%s-%s", s.prefix, repoName), nil)
		if err == nil && resp.StatusCode == 200 {
			releaseVersion = "main"
			// Download the content
			defer rc.Close()

			content, err = io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("failed to read content: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no release or root executable found")
		}
	}

	s.log.Info("successfully fetched plugin", "name", repo.GetName(), "version", releaseVersion, "runtime", rt)

	return &Info{
		Name:        repo.GetName(),
		Version:     releaseVersion,
		Description: repo.GetDescription(),
		Store:       "github",
		Runtime:     rt,
		Content:     content,
		Metadata: map[string]string{
			"owner":      repo.GetOwner().GetLogin(),
			"stars":      fmt.Sprintf("%d", repo.GetStargazersCount()),
			"repository": repo.GetHTMLURL(),
		},
	}, nil
}

// Search finds plugins matching the given criteria
func (s *GitHubStore) Search(ctx context.Context, criteria SearchOptions) ([]Info, error) {
	s.log.Info("starting search with criteria", "criteria", criteria)

	if s.topic == "" {
		s.log.Error(nil, "store initialization error", "topic", s.topic)
		return nil, fmt.Errorf("store not properly initialized: topic is empty")
	}

	query := fmt.Sprintf("topic:%s fork:false", s.topic)
	s.log.Info("executing search query", "query", query)

	result, _, err := s.client.Search.Repositories(ctx, query, &github.SearchOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		s.log.Error(err, "search failed")
		return nil, fmt.Errorf("failed to search repositories: %w", err)
	}

	s.log.Info("found repositories matching search criteria", "count", len(result.Repositories))

	var plugins []Info

	for _, repo := range result.Repositories {
		s.log.Info("processing repository", "name", repo.GetFullName(), "stars", repo.GetStargazersCount(), "created_at", repo.GetCreatedAt().String())

		s.log.Info("latest release", "owner", repo.GetOwner().GetLogin(), "repo", repo.GetName())

		release, resp, err := s.client.Repositories.GetLatestRelease(ctx, repo.GetOwner().GetLogin(), repo.GetName())
		if err != nil {
			s.log.Error(err, "failed to fetch release", "repo", repo.GetFullName(), "status", resp.StatusCode)
			continue
		}

		// Use Filter function to get valid assets
		getAssetNames := func() []string {
			names := make([]string, len(release.Assets))
			for i, asset := range release.Assets {
				names[i] = asset.GetName()
			}

			return names
		}

		validAssets := Filter(s.prefix, repo.GetName(), release.GetTagName(), getAssetNames)

		// Determine runtime based on valid assets
		runtime := "exec"

		for _, asset := range validAssets {
			if strings.HasSuffix(asset, ".wasm") {
				runtime = "wasm"
				break
			}
		}

		plugins = append(plugins, Info{
			Name:        repo.GetName(),
			Version:     release.GetTagName(),
			Description: repo.GetDescription(),
			Store:       "github",
			Runtime:     runtime,
			Metadata: map[string]string{
				"owner":      repo.GetOwner().GetLogin(),
				"stars":      fmt.Sprintf("%d", repo.GetStargazersCount()),
				"repository": repo.GetHTMLURL(),
			},
		})

		s.log.Info("added plugin to results", "name", repo.GetName(), "version", release.GetTagName())
	}

	s.log.Info("search complete", "found", len(plugins), "criteria", criteria)

	return plugins, nil
}
