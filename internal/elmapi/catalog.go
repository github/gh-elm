package elmapi

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const catalogPageSize = 100

// Repository is a repository visible to the authenticated user.
type Repository struct {
	FullName string `json:"full_name"`
	Owner    struct {
		Type string `json:"type"`
	} `json:"owner"`
}

// Organization is an organization visible to the authenticated user.
type Organization struct {
	Login string `json:"login"`
}

// ListRepositories lists repositories visible to the authenticated user.
func (c *Client) ListRepositories(ctx context.Context) ([]Repository, error) {
	var repositories []Repository
	for page := 1; ; page++ {
		query := url.Values{
			"affiliation": {"owner,collaborator,organization_member"},
			"page":        {strconv.Itoa(page)},
			"per_page":    {strconv.Itoa(catalogPageSize)},
			"sort":        {"full_name"},
		}
		var batch []Repository
		if err := c.get(ctx, "/user/repos", query, &batch); err != nil {
			return nil, fmt.Errorf("listing repositories: %w", err)
		}
		repositories = append(repositories, batch...)
		if len(batch) < catalogPageSize {
			break
		}
	}
	slices.SortFunc(repositories, func(a, b Repository) int {
		return compareFold(a.FullName, b.FullName)
	})
	return repositories, nil
}

// ListOrganizations lists organizations visible to the authenticated user.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var organizations []Organization
	for page := 1; ; page++ {
		query := url.Values{
			"page":     {strconv.Itoa(page)},
			"per_page": {strconv.Itoa(catalogPageSize)},
		}
		var batch []Organization
		if err := c.get(ctx, "/user/orgs", query, &batch); err != nil {
			return nil, fmt.Errorf("listing organizations: %w", err)
		}
		organizations = append(organizations, batch...)
		if len(batch) < catalogPageSize {
			break
		}
	}
	slices.SortFunc(organizations, func(a, b Organization) int {
		return compareFold(a.Login, b.Login)
	})
	return organizations, nil
}

func compareFold(a, b string) int {
	lowerA, lowerB := strings.ToLower(a), strings.ToLower(b)
	return strings.Compare(lowerA, lowerB)
}
