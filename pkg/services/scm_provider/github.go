package scm_provider

import (
	"context"
	"fmt"
	"os"

	"github.com/google/go-github/v35/github"
	"golang.org/x/oauth2"
)

type GithubProvider struct {
	client       *github.Client
	organization string
	allBranches  bool
}

var _ SCMProviderService = &GithubProvider{}

func NewGithubProvider(ctx context.Context, organization string, token string, url string, allBranches bool) (*GithubProvider, error) {
	var ts oauth2.TokenSource
	// Undocumented environment variable to set a default token, to be used in testing to dodge anonymous rate limits.
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token != "" {
		ts = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}
	httpClient := oauth2.NewClient(ctx, ts)
	var client *github.Client
	if url == "" {
		client = github.NewClient(httpClient)
	} else {
		var err error
		client, err = github.NewEnterpriseClient(url, url, httpClient)
		if err != nil {
			return nil, err
		}
	}
	return &GithubProvider{client: client, organization: organization, allBranches: allBranches}, nil
}

func (g *GithubProvider) ListRepos(ctx context.Context, cloneProtocol string) ([]*Repository, error) {
	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	repos := []*Repository{}
	for {
		githubRepos, resp, err := g.client.Repositories.ListByOrg(ctx, g.organization, opt)
		if err != nil {
			return nil, fmt.Errorf("error listing repositories for %s: %v", g.organization, err)
		}
		for _, githubRepo := range githubRepos {
			var url string
			switch cloneProtocol {
			// Default to SSH if unspecified (i.e. if "").
			case "", "ssh":
				url = githubRepo.GetSSHURL()
			case "https":
				url = githubRepo.GetCloneURL()
			default:
				return nil, fmt.Errorf("unknown clone protocol for GitHub %v", cloneProtocol)
			}

			branches, err := g.listBranches(ctx, githubRepo)
			if err != nil {
				return nil, fmt.Errorf("error listing branches for %s/%s: %v", githubRepo.Owner.GetLogin(), githubRepo.GetName(), err)
			}

			for _, branch := range branches {
				repos = append(repos, &Repository{
					Organization: githubRepo.Owner.GetLogin(),
					Repository:   githubRepo.GetName(),
					URL:          url,
					Branch:       branch.GetName(),
					SHA:          branch.GetCommit().GetSHA(),
					Labels:       githubRepo.Topics,
				})
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return repos, nil
}

func (g *GithubProvider) RepoHasPath(ctx context.Context, repo *Repository, path string) (bool, error) {
	_, _, resp, err := g.client.Repositories.GetContents(ctx, repo.Organization, repo.Repository, path, &github.RepositoryContentGetOptions{
		Ref: repo.Branch,
	})
	// 404s are not an error here, just a normal false.
	if resp != nil && resp.StatusCode == 404 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (g *GithubProvider) listBranches(ctx context.Context, repo *github.Repository) ([]github.Branch, error) {
	// If we don't specifically want to query for all branches, just use the default branch and call it a day.
	if !g.allBranches {
		defaultBranch, _, err := g.client.Repositories.GetBranch(ctx, repo.Owner.GetLogin(), repo.GetName(), repo.GetDefaultBranch())
		if err != nil {
			return nil, err
		}
		return []github.Branch{*defaultBranch}, nil
	}
	// Otherwise, scrape the ListBranches API.
	opt := &github.BranchListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	branches := []github.Branch{}
	for {
		githubBranches, resp, err := g.client.Repositories.ListBranches(ctx, repo.Owner.GetLogin(), repo.GetName(), opt)
		if err != nil {
			return nil, err
		}
		for _, githubBranch := range githubBranches {
			branches = append(branches, *githubBranch)
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return branches, nil
}
