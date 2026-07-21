package elmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Live-migration REST endpoints live on the source (GHES) API under
// /enterprise/live-migrations. See github/github
// app/api/description/operations/enterprise-admin/live-migration-*.yaml.
const migrationsBasePath = "/enterprise/live-migrations"

// Target repository visibilities accepted by the create endpoint. The REST API
// only permits private or internal; a source repository that is public must be
// migrated as internal (or private).
const (
	VisibilityPrivate  = "private"
	VisibilityInternal = "internal"
)

// Migration status filter values accepted by the list endpoint.
const (
	StatusCreated    = "created"
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusPaused     = "paused"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusTerminated = "terminated"
	StatusAll        = "all"
)

// CreateMigrationRequest is the body of a create-migration call. TargetVisibility
// is optional and defaults to internal server-side when omitted. TargetAPIEndpoint
// and PATName are required by the API; the migration commands derive/stub them
// (see newCreateCmd) rather than exposing dedicated flags.
type CreateMigrationRequest struct {
	SourceOrganizationLogin string `json:"source_organization_login"`
	SourceRepositoryName    string `json:"source_repository_name"`
	TargetOrganizationLogin string `json:"target_organization_login"`
	TargetRepositoryName    string `json:"target_repository_name"`
	TargetAPIEndpoint       string `json:"target_api_endpoint"`
	PATName                 string `json:"pat_name"`
	TargetVisibility        string `json:"target_visibility,omitempty"`
}

// CreateMigrationResponse is returned by the create endpoint. ExpiresAt is
// nullable, so it is a pointer that is nil when the API returns null.
type CreateMigrationResponse struct {
	MigrationID string  `json:"migration_id"`
	ExpiresAt   *string `json:"expires_at"`
}

// CreateMigration creates a new (unstarted) live migration.
// POST /enterprise/live-migrations.
func (c *Client) CreateMigration(ctx context.Context, req CreateMigrationRequest) (*CreateMigrationResponse, error) {
	var resp CreateMigrationResponse
	if err := c.post(ctx, migrationsBasePath, req, &resp, http.StatusCreated); err != nil {
		return nil, fmt.Errorf("creating migration: %w", err)
	}
	return &resp, nil
}

// StartMigration starts a previously created migration, launching backfill and
// live updates. POST /enterprise/live-migrations/{id}/start. Returns 204.
func (c *Client) StartMigration(ctx context.Context, migrationID string) error {
	if err := c.post(ctx, c.migrationPath(migrationID, "start"), nil, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("starting migration: %w", err)
	}
	return nil
}

// GetMigration returns the API's raw JSON status document for a migration,
// including migration, target_state, combined_state, and messages.
// GET /enterprise/live-migrations/{id}.
func (c *Client) GetMigration(ctx context.Context, migrationID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.get(ctx, c.migrationPath(migrationID), nil, &raw); err != nil {
		return nil, fmt.Errorf("getting migration: %w", err)
	}
	return raw, nil
}

// ListMigrationsOptions filters and paginates a list call. Leave a field at its
// zero value to omit it.
type ListMigrationsOptions struct {
	Status   string
	PageSize int
	After    string
}

// ListMigrations returns the API's raw JSON list document.
// GET /enterprise/live-migrations.
func (c *Client) ListMigrations(ctx context.Context, opts ListMigrationsOptions) (json.RawMessage, error) {
	q := url.Values{}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.PageSize > 0 {
		q.Set("page_size", fmt.Sprintf("%d", opts.PageSize))
	}
	if opts.After != "" {
		q.Set("after", opts.After)
	}

	var raw json.RawMessage
	if err := c.get(ctx, migrationsBasePath, q, &raw); err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}
	return raw, nil
}

// CancelMigration terminates a migration. This is a terminal action.
// POST /enterprise/live-migrations/{id}/cancel. Returns 204.
func (c *Client) CancelMigration(ctx context.Context, migrationID string) error {
	if err := c.post(ctx, c.migrationPath(migrationID, "cancel"), nil, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("cancelling migration: %w", err)
	}
	return nil
}

// CutoverRequest is the optional body of a cutover call.
type CutoverRequest struct {
	Force bool `json:"force"`
}

// Cutover initiates cutover to the destination, archiving the source repository
// and draining remaining changes. POST /enterprise/live-migrations/{id}/cutover.
// Returns 204.
func (c *Client) Cutover(ctx context.Context, migrationID string, force bool) error {
	body := CutoverRequest{Force: force}
	if err := c.post(ctx, c.migrationPath(migrationID, "cutover"), body, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("initiating cutover: %w", err)
	}
	return nil
}

// RevertCutoverResponse is returned by the revert-cutover endpoint.
type RevertCutoverResponse struct {
	Success                       bool `json:"success"`
	UnarchivedSourceRepository    bool `json:"unarchived_source_repository"`
	InProgressCutoverTerminated   bool `json:"in_progress_cutover_terminated"`
	InProgressMigrationTerminated bool `json:"in_progress_migration_terminated"`
}

// RevertCutover reverts the effects of a cutover so the source repository can be
// migrated again. POST /enterprise/live-migrations/{id}/revert-cutover.
func (c *Client) RevertCutover(ctx context.Context, migrationID string) (*RevertCutoverResponse, error) {
	var resp RevertCutoverResponse
	if err := c.post(ctx, c.migrationPath(migrationID, "revert-cutover"), nil, &resp, http.StatusOK); err != nil {
		return nil, fmt.Errorf("reverting cutover: %w", err)
	}
	return &resp, nil
}

// PauseMigration pauses source-load work (backfill and Git sync) while live
// event collection continues. POST /enterprise/live-migrations/{id}/pause.
// Returns 204.
func (c *Client) PauseMigration(ctx context.Context, migrationID string) error {
	if err := c.post(ctx, c.migrationPath(migrationID, "pause"), nil, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("pausing migration: %w", err)
	}
	return nil
}

// ResumeMigration resumes a paused migration.
// POST /enterprise/live-migrations/{id}/resume. Returns 204.
func (c *Client) ResumeMigration(ctx context.Context, migrationID string) error {
	if err := c.post(ctx, c.migrationPath(migrationID, "resume"), nil, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("resuming migration: %w", err)
	}
	return nil
}

// migrationPath builds /enterprise/live-migrations/{id}[/{action}], escaping the
// migration ID for use in a URL path segment.
func (c *Client) migrationPath(migrationID string, action ...string) string {
	p := migrationsBasePath + "/" + url.PathEscape(migrationID)
	for _, a := range action {
		p += "/" + a
	}
	return p
}

// --- Typed views over the GET status document, used by `watch`. ---

// MigrationDetail is a partial typed decode of the GET status document. Only the
// fields the watch renderer needs are modeled; the raw document (from
// GetMigration) is the source of truth for `status`.
type MigrationDetail struct {
	Migration     *MigrationSummary  `json:"migration"`
	TargetState   *TargetState       `json:"target_state"`
	CombinedState *CombinedState     `json:"combined_state"`
	Messages      []MigrationMessage `json:"messages"`
}

// MigrationSummary is the core migration record.
type MigrationSummary struct {
	MigrationID             string  `json:"migration_id"`
	Status                  *string `json:"status"`
	SourceOrganizationLogin string  `json:"source_organization_login"`
	TargetOrganizationLogin string  `json:"target_organization_login"`
	SourceRepositoryName    string  `json:"source_repository_name"`
	TargetRepositoryName    string  `json:"target_repository_name"`
	TargetVisibility        *string `json:"target_visibility"`
	TargetMigrationID       int64   `json:"target_migration_id"`
	CreatedAt               *string `json:"created_at"`
	StartedAt               *string `json:"started_at"`
	CompletedAt             *string `json:"completed_at"`
	ExpiresAt               *string `json:"expires_at"`
}

// TargetState carries destination-side aggregate progress.
type TargetState struct {
	Status             *string              `json:"status"`
	TargetUnavailable  bool                 `json:"target_unavailable"`
	RepositoryProgress []RepositoryProgress `json:"repository_progress"`
}

// RepositoryProgress is per-repository backfill/live-update counts.
type RepositoryProgress struct {
	RepositoryNWO                string `json:"repository_nwo"`
	BackfillResourcesAdded       int64  `json:"backfill_resources_added"`
	BackfillResourcesProcessed   int64  `json:"backfill_resources_processed"`
	BackfillResourcesFailed      int64  `json:"backfill_resources_failed"`
	LiveUpdateResourcesAdded     int64  `json:"live_update_resources_added"`
	LiveUpdateResourcesProcessed int64  `json:"live_update_resources_processed"`
	LiveUpdateResourcesFailed    int64  `json:"live_update_resources_failed"`
	AllResourcesSent             bool   `json:"all_resources_sent"`
	InitialGitPushComplete       bool   `json:"initial_git_push_complete"`
	RepositoryLocked             bool   `json:"repository_locked"`
}

// CombinedState is the derived, user-facing status including cutover readiness.
type CombinedState struct {
	Status          *string                   `json:"status"`
	DisplayMessage  string                    `json:"display_message"`
	ReadyForCutover bool                      `json:"ready_for_cutover"`
	CutoverBlockers []string                  `json:"cutover_blockers"`
	Repositories    []CombinedRepositoryState `json:"repositories"`
}

// CombinedRepositoryState is per-repository derived phase/status.
type CombinedRepositoryState struct {
	RepositoryNWO string  `json:"repository_nwo"`
	Phase         *string `json:"phase"`
	DisplayStatus string  `json:"display_status"`
}

// MigrationMessage is an informational or error message on the migration.
type MigrationMessage struct {
	MessageType string  `json:"message_type"`
	Message     string  `json:"message"`
	CreatedAt   *string `json:"created_at"`
}

// GetMigrationDetail fetches and decodes the typed status view for a migration.
func (c *Client) GetMigrationDetail(ctx context.Context, migrationID string) (*MigrationDetail, error) {
	raw, err := c.GetMigration(ctx, migrationID)
	if err != nil {
		return nil, err
	}
	var detail MigrationDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, fmt.Errorf("decoding migration status: %w", err)
	}
	return &detail, nil
}
