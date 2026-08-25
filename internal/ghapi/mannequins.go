package ghapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrUserNotFound is returned (wrapped) by UserID when the target login does not
// resolve to a user. Callers can distinguish it with errors.Is to skip a missing
// claimant while still propagating auth/network/other failures.
var ErrUserNotFound = errors.New("user not found")

// Claimant is the user a mannequin has been mapped to (its "target user").
type Claimant struct {
	ID    string
	Login string
}

// Mannequin is a source identity awaiting attribution on the target org.
// MappedUser is nil when the mannequin has not been reclaimed yet.
type Mannequin struct {
	ID         string
	Login      string
	MappedUser *Claimant
}

// AttributionResult is the source/target pair echoed by the reclaim mutations.
type AttributionResult struct {
	SourceID    string
	SourceLogin string
	TargetID    string
	TargetLogin string
}

// mannequinNode mirrors one node of the mannequins GraphQL connection.
type mannequinNode struct {
	ID       string `json:"id"`
	Login    string `json:"login"`
	Claimant *struct {
		ID    string `json:"id"`
		Login string `json:"login"`
	} `json:"claimant"`
}

// mannequinsData decodes the paginated mannequins query response.
type mannequinsData struct {
	Node struct {
		Mannequins struct {
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
			Nodes []mannequinNode `json:"nodes"`
		} `json:"mannequins"`
	} `json:"node"`
}

const mannequinsQuery = `query($id: ID!, $first: Int, $after: String) {
  node(id: $id) {
    ... on Organization {
      mannequins(first: $first, after: $after) {
        pageInfo { endCursor hasNextPage }
        nodes { login id claimant { login id } }
      }
    }
  }
}`

const mannequinsByLoginQuery = `query($id: ID!, $first: Int, $after: String, $login: String) {
  node(id: $id) {
    ... on Organization {
      mannequins(first: $first, after: $after, login: $login) {
        pageInfo { endCursor hasNextPage }
        nodes { login id claimant { login id } }
      }
    }
  }
}`

// OrganizationID looks up the node ID for an organization login.
func (c *Client) OrganizationID(ctx context.Context, org string) (string, error) {
	var data struct {
		Organization struct {
			ID string `json:"id"`
		} `json:"organization"`
	}
	vars := map[string]any{"login": org}
	if err := c.graphQL(ctx, `query($login: String!) { organization(login: $login) { login id name } }`, vars, &data); err != nil {
		return "", fmt.Errorf("looking up organization ID for %q: %w", org, err)
	}
	if data.Organization.ID == "" {
		return "", fmt.Errorf("organization %q not found", org)
	}
	return data.Organization.ID, nil
}

// UserID looks up the node ID for a user login.
func (c *Client) UserID(ctx context.Context, login string) (string, error) {
	var data struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	vars := map[string]any{"login": login}
	if err := c.graphQL(ctx, `query($login: String!) { user(login: $login) { id name } }`, vars, &data); err != nil {
		if isUserNotFound(err) {
			return "", fmt.Errorf("user %q not found: %w", login, ErrUserNotFound)
		}
		return "", fmt.Errorf("looking up user ID for %q: %w", login, err)
	}
	if data.User.ID == "" {
		return "", fmt.Errorf("user %q not found: %w", login, ErrUserNotFound)
	}
	return data.User.ID, nil
}

// isUserNotFound reports whether err is GitHub's GraphQL "could not resolve to a
// User" response, i.e. the login simply does not exist (as opposed to an auth,
// network, or other transient failure).
func isUserNotFound(err error) bool {
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) {
		return false
	}
	for _, m := range gqlErr.Messages {
		if strings.Contains(m, "Could not resolve to a User with the login") {
			return true
		}
	}
	return false
}

// BotID resolves a GitHub App / bot account login (e.g. "example-ci[bot]") to
// its GraphQL node ID. The GraphQL user(login:) query hides bots, so we use the
// REST users endpoint, which returns bot accounts (type "Bot") along with their
// node_id. A 404 is reported as ErrUserNotFound so callers can skip a missing
// claimant, mirroring UserID.
func (c *Client) BotID(ctx context.Context, login string) (string, error) {
	path := fmt.Sprintf("/users/%s", url.PathEscape(login))
	var data struct {
		Type   string `json:"type"`
		NodeID string `json:"node_id"`
	}
	if err := c.restGet(ctx, path, &data, 200, 404); err != nil {
		return "", fmt.Errorf("looking up bot ID for %q: %w", login, err)
	}
	if data.NodeID == "" {
		return "", fmt.Errorf("bot %q not found: %w", login, ErrUserNotFound)
	}
	if !strings.EqualFold(data.Type, "Bot") {
		return "", fmt.Errorf("%q is not a GitHub App / bot account", login)
	}
	return data.NodeID, nil
}

// LoginName returns the login of the authenticated user (the token's viewer).
func (c *Client) LoginName(ctx context.Context) (string, error) {
	var data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := c.graphQL(ctx, `query { viewer { login } }`, nil, &data); err != nil {
		return "", fmt.Errorf("looking up the current user's login: %w", err)
	}
	return data.Viewer.Login, nil
}

// OrgMembership returns the role of member in org (e.g. "admin", "member"), or
// an empty string when the user is not a member. GET /orgs/:org/memberships/:member.
func (c *Client) OrgMembership(ctx context.Context, org, member string) (string, error) {
	path := fmt.Sprintf("/orgs/%s/memberships/%s", url.PathEscape(org), url.PathEscape(member))
	var data struct {
		Role string `json:"role"`
	}
	if err := c.restGet(ctx, path, &data, 200, 404); err != nil {
		return "", fmt.Errorf("looking up org membership for %q in %q: %w", member, org, err)
	}
	return data.Role, nil
}

// Mannequins returns every mannequin in the org, following pagination.
func (c *Client) Mannequins(ctx context.Context, orgID string) ([]Mannequin, error) {
	return c.fetchMannequins(ctx, mannequinsQuery, map[string]any{"id": orgID})
}

// MannequinsByLogin returns the org's mannequins filtered to a single login.
func (c *Client) MannequinsByLogin(ctx context.Context, orgID, login string) ([]Mannequin, error) {
	return c.fetchMannequins(ctx, mannequinsByLoginQuery, map[string]any{"id": orgID, "login": login})
}

// fetchMannequins runs a mannequins query, following the pageInfo cursor until
// there are no more pages.
func (c *Client) fetchMannequins(ctx context.Context, query string, vars map[string]any) ([]Mannequin, error) {
	var out []Mannequin
	after := ""
	for {
		vars["first"] = graphQLPageSize
		if after != "" {
			vars["after"] = after
		} else {
			delete(vars, "after")
		}

		var data mannequinsData
		if err := c.graphQL(ctx, query, vars, &data); err != nil {
			return nil, fmt.Errorf("retrieving mannequins: %w", err)
		}

		for _, n := range data.Node.Mannequins.Nodes {
			m := Mannequin{ID: n.ID, Login: n.Login}
			if n.Claimant != nil && n.Claimant.Login != "" {
				m.MappedUser = &Claimant{ID: n.Claimant.ID, Login: n.Claimant.Login}
			}
			out = append(out, m)
		}

		page := data.Node.Mannequins.PageInfo
		if !page.HasNextPage || page.EndCursor == "" {
			return out, nil
		}
		after = page.EndCursor
	}
}

const createAttributionInvitationMutation = `mutation($orgId: ID!, $sourceId: ID!, $targetId: ID!) {
  createAttributionInvitation(input: { ownerId: $orgId, sourceId: $sourceId, targetId: $targetId }) {
    source { ... on Mannequin { id login } }
    target { ... on User { id login } }
  }
}`

const reattributeMannequinToUserMutation = `mutation($orgId: ID!, $sourceId: ID!, $targetId: ID!) {
  reattributeMannequinToUser(input: { ownerId: $orgId, sourceId: $sourceId, targetId: $targetId }) {
    source { ... on Mannequin { id login } }
    target { ... on User { id login } }
  }
}`

const reattributeMannequinToBotMutation = `mutation($orgId: ID!, $sourceId: ID!, $targetId: ID!) {
  reattributeMannequinToBot(input: { ownerId: $orgId, sourceId: $sourceId, targetId: $targetId }) {
    source { ... on Mannequin { id login } }
    target { ... on Bot { id login } }
  }
}`

// userInfo decodes a { id, login } source/target node.
type userInfo struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

// CreateAttributionInvitation sends a reclaim invitation email flow for a
// mannequin (used when --skip-invitation is NOT set).
func (c *Client) CreateAttributionInvitation(ctx context.Context, orgID, mannequinID, targetUserID string) (*AttributionResult, error) {
	var data struct {
		CreateAttributionInvitation struct {
			Source userInfo `json:"source"`
			Target userInfo `json:"target"`
		} `json:"createAttributionInvitation"`
	}
	vars := map[string]any{"orgId": orgID, "sourceId": mannequinID, "targetId": targetUserID}
	if err := c.graphQL(ctx, createAttributionInvitationMutation, vars, &data); err != nil {
		return nil, err
	}
	r := data.CreateAttributionInvitation
	return &AttributionResult{
		SourceID: r.Source.ID, SourceLogin: r.Source.Login,
		TargetID: r.Target.ID, TargetLogin: r.Target.Login,
	}, nil
}

// ReattributeMannequinToUser immediately reattributes a mannequin's content to
// a user (used when --skip-invitation is set). Only available to EMU orgs.
func (c *Client) ReattributeMannequinToUser(ctx context.Context, orgID, mannequinID, targetUserID string) (*AttributionResult, error) {
	var data struct {
		ReattributeMannequinToUser struct {
			Source userInfo `json:"source"`
			Target userInfo `json:"target"`
		} `json:"reattributeMannequinToUser"`
	}
	vars := map[string]any{"orgId": orgID, "sourceId": mannequinID, "targetId": targetUserID}
	if err := c.graphQL(ctx, reattributeMannequinToUserMutation, vars, &data); err != nil {
		return nil, err
	}
	r := data.ReattributeMannequinToUser
	return &AttributionResult{
		SourceID: r.Source.ID, SourceLogin: r.Source.Login,
		TargetID: r.Target.ID, TargetLogin: r.Target.Login,
	}, nil
}

// ReattributeMannequinToBot immediately reattributes a mannequin's content to a
// customer-owned GitHub App / bot account. The acting admin must own or
// administer the target app; GitHub-owned apps are rejected server-side.
func (c *Client) ReattributeMannequinToBot(ctx context.Context, orgID, mannequinID, targetBotID string) (*AttributionResult, error) {
	var data struct {
		ReattributeMannequinToBot struct {
			Source userInfo `json:"source"`
			Target userInfo `json:"target"`
		} `json:"reattributeMannequinToBot"`
	}
	vars := map[string]any{"orgId": orgID, "sourceId": mannequinID, "targetId": targetBotID}
	if err := c.graphQL(ctx, reattributeMannequinToBotMutation, vars, &data); err != nil {
		return nil, err
	}
	r := data.ReattributeMannequinToBot
	return &AttributionResult{
		SourceID: r.Source.ID, SourceLogin: r.Source.Login,
		TargetID: r.Target.ID, TargetLogin: r.Target.Login,
	}, nil
}
