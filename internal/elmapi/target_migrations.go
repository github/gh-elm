package elmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// targetMigrationBasePath is the base path for the target (GHEC/Proxima)
// migration-management API, which manages the migration record itself. This is
// distinct from the source (GHES) live-migrations API (migrations.go, base
// path /enterprise/live-migrations) and from the per-migration nodes/reports
// APIs (nodes.go, reports.go), which share this same /enterprise/migration
// prefix but address a different resource.
const targetMigrationBasePath = "/enterprise/migration"

// Status values for a target migration (wire values), returned by list and
// status.
const (
	TargetMigrationStatusInvalid    = "STATUS_TYPE_INVALID"
	TargetMigrationStatusInProgress = "STATUS_TYPE_IN_PROGRESS"
	TargetMigrationStatusComplete   = "STATUS_TYPE_COMPLETE"
	TargetMigrationStatusFailed     = "STATUS_TYPE_FAILED"
	TargetMigrationStatusAborted    = "STATUS_TYPE_ABORTED"
	TargetMigrationStatusExpired    = "STATUS_TYPE_EXPIRED"
	TargetMigrationStatusPaused     = "STATUS_TYPE_PAUSED"
)

// targetMigrationsPageSize is the page size used when following pagination in
// IterTargetMigrations.
const targetMigrationsPageSize = 100

// TargetMigrationInitiatorCustomer identifies lifecycle mutations initiated by
// a customer-operated gh-elm invocation.
const TargetMigrationInitiatorCustomer = "customer"

// TargetMigrationTransitionRequest attributes a target migration lifecycle
// mutation to the customer invocation that caused it.
type TargetMigrationTransitionRequest struct {
	Initiator   string `json:"initiator"`
	OperationID string `json:"operation_id"`
}

// NewTargetMigrationOperationID returns the operation ID for one logical
// target migration lifecycle mutation. Callers should reuse it if they retry
// that mutation.
func NewTargetMigrationOperationID() string {
	return uuid.NewString()
}

// NewTargetMigrationTransitionRequest returns attribution metadata for one
// customer-initiated target migration lifecycle mutation.
func NewTargetMigrationTransitionRequest() TargetMigrationTransitionRequest {
	return TargetMigrationTransitionRequest{
		Initiator:   TargetMigrationInitiatorCustomer,
		OperationID: NewTargetMigrationOperationID(),
	}
}

// CreateTargetMigrationRequest is the body of a create-migration call against
// the target (GHEC/Proxima) migration-management API. Repositories currently
// accepts exactly one entry; the API does not yet support multi-repository
// migrations.
type CreateTargetMigrationRequest struct {
	SourceURL             string   `json:"source_url"`
	Repositories          []string `json:"repositories"`
	Description           string   `json:"description,omitempty"`
	ExporterMigrationGUID string   `json:"exporter_migration_guid,omitempty"`
	Initiator             string   `json:"initiator"`
	OperationID           string   `json:"operation_id"`
}

// CreateTargetMigration creates a migration on the target (GHEC/Proxima) side
// and returns the API's raw JSON response (migrationId, expiresAt).
// POST /enterprise/migration/create.
func (c *Client) CreateTargetMigration(ctx context.Context, req CreateTargetMigrationRequest) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.post(ctx, targetMigrationBasePath+"/create", req, &raw, http.StatusCreated); err != nil {
		return nil, fmt.Errorf("creating target migration: %w", err)
	}
	return raw, nil
}

// TargetRepositoryProgress is a single repository's processing progress within
// a target migration, as returned by the status endpoint. The list endpoint
// always returns this empty (deprecated there); use GetTargetMigrationStatus
// for real progress.
type TargetRepositoryProgress struct {
	RepositoryNWO                   string `json:"repositoryNwo"`
	ResourcesAdded                  int64  `json:"resourcesAdded"`
	ResourcesProcessed              int64  `json:"resourcesProcessed"`
	AllResourcesSent                bool   `json:"allResourcesSent"`
	AllLiveUpdatesSent              bool   `json:"allLiveUpdatesSent"`
	EventsAdded                     int64  `json:"eventsAdded"`
	EventsProcessed                 int64  `json:"eventsProcessed"`
	BackfillResourcesAcknowledged   int64  `json:"backfillResourcesAcknowledged"`
	LiveUpdateResourcesAcknowledged int64  `json:"liveUpdateResourcesAcknowledged"`
}

// TargetMigration is a target-side migration record, as returned by the list
// and status endpoints. Raw holds the exact JSON object the API returned for
// this migration, so callers rendering JSON can echo the API's response
// verbatim — preserving fields this struct does not model and avoiding
// zero-valued fields that re-marshaling would inject.
type TargetMigration struct {
	MigrationID        string                     `json:"migrationId"`
	Status             string                     `json:"status"`
	ExpiresAt          time.Time                  `json:"expiresAt"`
	Description        string                     `json:"description,omitempty"`
	Repositories       []string                   `json:"repositories,omitempty"`
	RepositoryProgress []TargetRepositoryProgress `json:"repositoryProgress,omitempty"`

	// Raw is the original JSON object for this migration. It is populated on
	// decode and excluded from (re-)marshaling.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the migration's original
// JSON bytes in Raw. The bytes are copied because the decoder may reuse the
// buffer backing data after this call returns.
func (m *TargetMigration) UnmarshalJSON(data []byte) error {
	type migrationFields TargetMigration // strip methods to avoid recursing
	var f migrationFields
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*m = TargetMigration(f)
	m.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListTargetMigrationsOptions paginates a ListTargetMigrations call. Leave a
// field at its zero value to omit it.
type ListTargetMigrationsOptions struct {
	PageSize  int
	PageToken string
}

// ListTargetMigrationsResponse is a single page of target migrations.
// NextPageToken is the cursor for the next page; it is empty on the last page.
type ListTargetMigrationsResponse struct {
	Migrations    []TargetMigration `json:"migrations"`
	NextPageToken string            `json:"nextPageToken"`
}

// ListTargetMigrations fetches a single page of migrations on the target
// (GHEC/Proxima) side. GET /enterprise/migration/list.
func (c *Client) ListTargetMigrations(ctx context.Context, opts ListTargetMigrationsOptions) (*ListTargetMigrationsResponse, error) {
	q := url.Values{}
	if opts.PageSize > 0 {
		q.Set("page_size", strconv.Itoa(opts.PageSize))
	}
	if opts.PageToken != "" {
		q.Set("page_token", opts.PageToken)
	}

	var resp ListTargetMigrationsResponse
	if err := c.get(ctx, targetMigrationBasePath+"/list", q, &resp); err != nil {
		return nil, fmt.Errorf("listing target migrations: %w", err)
	}
	return &resp, nil
}

// IterTargetMigrations yields every target migration, following pagination
// until the API stops returning a next page token. Iteration stops on the
// first error, delivered as the second value of the final pair; callers must
// check it. A repeated page token is treated as an error rather than a quiet
// stop, since it means the API can't be trusted to deliver the remaining
// migrations and a caller checking only the loop's error return would
// otherwise mistake a truncated result for a complete one.
func (c *Client) IterTargetMigrations(ctx context.Context, opts ListTargetMigrationsOptions) iter.Seq2[TargetMigration, error] {
	return func(yield func(TargetMigration, error) bool) {
		opts.PageSize = targetMigrationsPageSize
		opts.PageToken = ""

		seen := make(map[string]bool)
		for {
			if err := ctx.Err(); err != nil {
				yield(TargetMigration{}, err)
				return
			}

			page, err := c.ListTargetMigrations(ctx, opts)
			if err != nil {
				yield(TargetMigration{}, err)
				return
			}

			for _, m := range page.Migrations {
				if !yield(m, nil) {
					return
				}
			}

			if err := ctx.Err(); err != nil {
				yield(TargetMigration{}, err)
				return
			}

			if page.NextPageToken == "" {
				return
			}
			if seen[page.NextPageToken] {
				yield(TargetMigration{}, fmt.Errorf("target migration pagination repeated page token %q; results may be incomplete", page.NextPageToken))
				return
			}
			seen[page.NextPageToken] = true
			opts.PageToken = page.NextPageToken
		}
	}
}

// TargetMigrationStatusResponse wraps the single-migration status response.
type TargetMigrationStatusResponse struct {
	Migration TargetMigration `json:"migration"`
}

// GetTargetMigrationStatus fetches full status and per-repository progress for
// one migration. GET /enterprise/migration/{id}/status.
func (c *Client) GetTargetMigrationStatus(ctx context.Context, migrationID int64) (*TargetMigrationStatusResponse, error) {
	path := fmt.Sprintf("%s/%d/status", targetMigrationBasePath, migrationID)
	var resp TargetMigrationStatusResponse
	if err := c.get(ctx, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("getting target migration status: %w", err)
	}
	return &resp, nil
}

// PauseTargetMigration pauses a migration on the target (GHEC/Proxima) side.
// POST /enterprise/migration/{id}/pause. Returns 204.
func (c *Client) PauseTargetMigration(ctx context.Context, migrationID int64, req TargetMigrationTransitionRequest) error {
	path := fmt.Sprintf("%s/%d/pause", targetMigrationBasePath, migrationID)
	if err := c.post(ctx, path, req, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("pausing target migration: %w", err)
	}
	return nil
}

// ResumeTargetMigration resumes a paused migration on the target side.
// POST /enterprise/migration/{id}/resume. Returns 204.
func (c *Client) ResumeTargetMigration(ctx context.Context, migrationID int64, req TargetMigrationTransitionRequest) error {
	path := fmt.Sprintf("%s/%d/resume", targetMigrationBasePath, migrationID)
	if err := c.post(ctx, path, req, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("resuming target migration: %w", err)
	}
	return nil
}

// AbortTargetMigration aborts a migration on the target side. This is a
// terminal action. POST /enterprise/migration/{id}/abort. Returns 204.
func (c *Client) AbortTargetMigration(ctx context.Context, migrationID int64, req TargetMigrationTransitionRequest) error {
	path := fmt.Sprintf("%s/%d/abort", targetMigrationBasePath, migrationID)
	if err := c.post(ctx, path, req, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("aborting target migration: %w", err)
	}
	return nil
}
