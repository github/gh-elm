package elmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"time"
)

// Node origins accepted by the target API (wire values).
const (
	OriginBackfill   = "NODE_ORIGIN_BACKFILL"
	OriginLiveUpdate = "NODE_ORIGIN_LIVE_UPDATE"
)

// Node states accepted by the target API (wire values).
const (
	StatePending   = "NODE_STATE_PENDING"
	StateProcessed = "NODE_STATE_PROCESSED"
	StateFailed    = "NODE_STATE_FAILED"
	StateEligible  = "NODE_STATE_ELIGIBLE"
)

// nodesPageSize is the page size used when following pagination.
const nodesPageSize = 100

// Node represents a migration node as exposed by the target API. Raw holds the
// exact JSON object the API returned for this node, so callers rendering JSON
// can echo the API's response verbatim — preserving fields this struct does not
// model and avoiding zero-valued fields that re-marshaling would inject.
type Node struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Origin    string    `json:"origin"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Raw is the original JSON object for this node. It is populated on decode
	// and excluded from (re-)marshaling.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the node's original JSON
// bytes in Raw. The bytes are copied because the decoder may reuse the buffer
// backing data after this call returns.
func (n *Node) UnmarshalJSON(data []byte) error {
	type nodeFields Node // strip methods to avoid recursing into this method
	var f nodeFields
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*n = Node(f)
	n.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListMigrationNodesResponse is a single page of migration nodes. After is the
// cursor for the next page; it is empty on the last page.
type ListMigrationNodesResponse struct {
	Nodes []Node `json:"nodes"`
	After string `json:"after"`
}

// ListNodesOptions filters and paginates a ListMigrationNodes call. Origin and
// State are wire values (see the Origin*/State* constants); leave them empty to
// omit the filter.
type ListNodesOptions struct {
	RepositoryNWO string
	Origin        string
	State         string
	PageSize      int
	After         string
}

// ListMigrationNodes fetches a single page of nodes for a migration.
func (c *Client) ListMigrationNodes(ctx context.Context, migrationID int64, opts ListNodesOptions) (*ListMigrationNodesResponse, error) {
	path := fmt.Sprintf("/enterprise/migration/%d/nodes", migrationID)

	q := url.Values{}
	if opts.RepositoryNWO != "" {
		q.Set("repository_nwo", opts.RepositoryNWO)
	}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.Origin != "" {
		q.Set("origin", opts.Origin)
	}
	if opts.PageSize > 0 {
		q.Set("page_size", strconv.Itoa(opts.PageSize))
	}
	if opts.After != "" {
		q.Set("after", opts.After)
	}

	var resp ListMigrationNodesResponse
	if err := c.get(ctx, path, q, &resp); err != nil {
		return nil, fmt.Errorf("listing migration nodes: %w", err)
	}
	return &resp, nil
}

// IterNodes yields every node matching opts, following pagination until the API
// stops returning progress. Iteration stops on the first error, which is
// delivered as the second value of the final pair; callers must check it.
func (c *Client) IterNodes(ctx context.Context, migrationID int64, opts ListNodesOptions) iter.Seq2[Node, error] {
	return func(yield func(Node, error) bool) {
		opts.PageSize = nodesPageSize
		opts.After = ""

		seen := make(map[string]bool)
		for ctx.Err() == nil {
			page, err := c.ListMigrationNodes(ctx, migrationID, opts)
			if err != nil {
				yield(Node{}, err)
				return
			}

			for _, n := range page.Nodes {
				if !yield(n, nil) {
					return
				}
			}

			// A non-empty cursor is pagination progress even when this page
			// has no nodes: a filtered request can return an empty page that
			// still points at later pages containing matching nodes, so we
			// must keep following the cursor rather than stop on an empty
			// page. Stop only when the cursor is empty, or when the API hands
			// back a cursor we've already followed, which would otherwise spin
			// this loop forever making no progress.
			if page.After == "" || seen[page.After] {
				return
			}
			seen[page.After] = true
			opts.After = page.After
		}
	}
}
