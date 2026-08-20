// Package workflow provides command-neutral operations shared by interactive
// clients such as the TUI.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
	"github.com/github/gh-elm/internal/ghapi"
)

// SourceMigrationID is a source-side migration UUID.
type SourceMigrationID string

// TargetMigrationID is a target-side numeric migration ID.
type TargetMigrationID int64

// Service executes ELM workflows using configured endpoints.
type Service struct{}

// New returns a workflow service.
func New() *Service {
	return &Service{}
}

// Configuration is the redacted configuration state shown by interactive UIs.
type Configuration struct {
	SourceURL       string
	SourceTokenSet  bool
	TargetURL       string
	TargetTokenSet  bool
	ConfigPath      string
	CredentialStore string
}

// ConfigurationInput updates stored endpoint URLs and optional tokens. Empty
// token values preserve the currently stored token.
type ConfigurationInput struct {
	SourceURL   string
	SourceToken string
	TargetURL   string
	TargetToken string
}

// SourceCreateInput describes a source-driven live migration.
type SourceCreateInput struct {
	SourceOwner string
	SourceRepo  string
	TargetOwner string
	TargetRepo  string
	Visibility  string
	Start       bool
}

// SourceCreateResult is returned after source migration creation.
type SourceCreateResult struct {
	Migration elmapi.CreateMigrationResponse
	Started   bool
}

// TargetCreateInput describes a direct target-side migration.
type TargetCreateInput struct {
	SourceRepositoryURL string
	Repository          string
	Description         string
	ExporterGUID        string
}

// ResourceInput filters target migration resources.
type ResourceInput struct {
	MigrationID TargetMigrationID
	Repository  string
	Origin      string
	State       string
	MaxResults  int
}

// ReportInput identifies a target node report.
type ReportInput struct {
	MigrationID TargetMigrationID
	Stage       string
	State       string
}

// MannequinReclaimInput describes a single or CSV mannequin reclaim.
type MannequinReclaimInput struct {
	Organization   string
	CSVPath        string
	Mannequin      string
	MannequinID    string
	TargetUser     string
	Force          bool
	SkipInvitation bool
}

// ListSourceMigrations lists source-side migrations.
func (s *Service) ListSourceMigrations(ctx context.Context, status string) ([]elmapi.MigrationSummary, error) {
	client, err := s.sourceClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.ListMigrations(ctx, elmapi.ListMigrationsOptions{Status: status})
	if err != nil {
		return nil, err
	}
	if status == "" && len(resp.Value.Migrations) == 0 && resp.Value.TotalCount == 0 {
		resp, err = client.ListMigrations(ctx, elmapi.ListMigrationsOptions{Status: elmapi.StatusCreated})
		if err != nil {
			return nil, err
		}
	}
	return resp.Value.Migrations, nil
}

// GetSourceMigration fetches source migration status.
func (s *Service) GetSourceMigration(ctx context.Context, id SourceMigrationID) (*elmapi.MigrationDetail, error) {
	if err := requireSourceID(id); err != nil {
		return nil, err
	}
	client, err := s.sourceClient()
	if err != nil {
		return nil, err
	}
	return client.GetMigrationDetail(ctx, string(id))
}

// CreateSourceMigration creates and optionally starts a source migration.
func (s *Service) CreateSourceMigration(ctx context.Context, in SourceCreateInput) (*SourceCreateResult, error) {
	in.SourceOwner = strings.TrimSpace(in.SourceOwner)
	in.SourceRepo = strings.TrimSpace(in.SourceRepo)
	in.TargetOwner = strings.TrimSpace(in.TargetOwner)
	in.TargetRepo = strings.TrimSpace(in.TargetRepo)
	if in.SourceOwner == "" || in.SourceRepo == "" || in.TargetOwner == "" || in.TargetRepo == "" {
		return nil, errors.New("source owner, source repository, target owner, and target repository are required")
	}
	visibility, err := visibility(in.Visibility)
	if err != nil {
		return nil, err
	}
	sourceClient, err := s.sourceClient()
	if err != nil {
		return nil, err
	}
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return nil, err
	}
	target, err := resolver.Target("", "")
	if err != nil {
		return nil, err
	}
	if target.URL == "" {
		return nil, errors.New("a target URL is required to create a migration; configure the target first")
	}
	req := elmapi.CreateMigrationRequest{
		SourceOrganizationLogin: in.SourceOwner,
		SourceRepositoryName:    in.SourceRepo,
		TargetOrganizationLogin: in.TargetOwner,
		TargetRepositoryName:    in.TargetRepo,
		TargetAPIEndpoint:       target.URL,
		PATName:                 "BOGON",
		TargetVisibility:        visibility,
	}
	if err := ensureUniqueSourceMigration(ctx, sourceClient, req); err != nil {
		return nil, err
	}
	resp, err := sourceClient.CreateMigration(ctx, req)
	if err != nil {
		return nil, err
	}
	result := &SourceCreateResult{Migration: resp.Value}
	if in.Start {
		if err := sourceClient.StartMigration(ctx, resp.Value.MigrationID); err != nil {
			return nil, fmt.Errorf("migration %s created but failed to start: %w", resp.Value.MigrationID, err)
		}
		result.Started = true
	}
	return result, nil
}

// StartSourceMigration starts a source migration.
func (s *Service) StartSourceMigration(ctx context.Context, id SourceMigrationID) error {
	return s.sourceAction(ctx, id, (*elmapi.Client).StartMigration)
}

// PauseSourceMigration pauses a source migration.
func (s *Service) PauseSourceMigration(ctx context.Context, id SourceMigrationID) error {
	return s.sourceAction(ctx, id, (*elmapi.Client).PauseMigration)
}

// ResumeSourceMigration resumes a source migration.
func (s *Service) ResumeSourceMigration(ctx context.Context, id SourceMigrationID) error {
	return s.sourceAction(ctx, id, (*elmapi.Client).ResumeMigration)
}

// CancelSourceMigration cancels a source migration.
func (s *Service) CancelSourceMigration(ctx context.Context, id SourceMigrationID) error {
	return s.sourceAction(ctx, id, (*elmapi.Client).CancelMigration)
}

// CutoverSourceMigration initiates source migration cutover.
func (s *Service) CutoverSourceMigration(ctx context.Context, id SourceMigrationID, force bool) error {
	if err := requireSourceID(id); err != nil {
		return err
	}
	client, err := s.sourceClient()
	if err != nil {
		return err
	}
	return client.Cutover(ctx, string(id), force)
}

// RevertSourceCutover reverts source migration cutover.
func (s *Service) RevertSourceCutover(ctx context.Context, id SourceMigrationID) (*elmapi.RevertCutoverResponse, error) {
	if err := requireSourceID(id); err != nil {
		return nil, err
	}
	client, err := s.sourceClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.RevertCutover(ctx, string(id))
	if err != nil {
		return nil, err
	}
	return &resp.Value, nil
}

// ListTargetMigrations lists target-side migration records.
func (s *Service) ListTargetMigrations(ctx context.Context, status string, maxResults int) ([]elmapi.TargetMigration, error) {
	client, err := s.targetClient()
	if err != nil {
		return nil, err
	}
	status = normalizeTargetStatus(status)
	var migrations []elmapi.TargetMigration
	for migration, iterErr := range client.IterTargetMigrations(ctx, elmapi.ListTargetMigrationsOptions{}) {
		if iterErr != nil {
			return nil, iterErr
		}
		if status != "" && migration.Status != status {
			continue
		}
		migrations = append(migrations, migration)
		if maxResults > 0 && len(migrations) >= maxResults {
			break
		}
	}
	return migrations, nil
}

// CreateTargetMigration creates a migration directly on the target.
func (s *Service) CreateTargetMigration(ctx context.Context, in TargetCreateInput) (json.RawMessage, error) {
	if strings.TrimSpace(in.SourceRepositoryURL) == "" || strings.TrimSpace(in.Repository) == "" {
		return nil, errors.New("source repository URL and target repository are required")
	}
	client, err := s.targetClient()
	if err != nil {
		return nil, err
	}
	return client.CreateTargetMigration(ctx, elmapi.CreateTargetMigrationRequest{
		SourceURL:             strings.TrimSpace(in.SourceRepositoryURL),
		Repositories:          []string{strings.TrimSpace(in.Repository)},
		Description:           strings.TrimSpace(in.Description),
		ExporterMigrationGUID: strings.TrimSpace(in.ExporterGUID),
	})
}

// GetTargetMigration fetches target status and repository progress.
func (s *Service) GetTargetMigration(ctx context.Context, id TargetMigrationID) (*elmapi.TargetMigration, error) {
	if err := requireTargetID(id); err != nil {
		return nil, err
	}
	client, err := s.targetClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.GetTargetMigrationStatus(ctx, int64(id))
	if err != nil {
		return nil, err
	}
	return &resp.Migration, nil
}

// PauseTargetMigration pauses a target migration.
func (s *Service) PauseTargetMigration(ctx context.Context, id TargetMigrationID) error {
	return s.targetAction(ctx, id, (*elmapi.Client).PauseTargetMigration)
}

// ResumeTargetMigration resumes a target migration.
func (s *Service) ResumeTargetMigration(ctx context.Context, id TargetMigrationID) error {
	return s.targetAction(ctx, id, (*elmapi.Client).ResumeTargetMigration)
}

// AbortTargetMigration aborts a target migration.
func (s *Service) AbortTargetMigration(ctx context.Context, id TargetMigrationID) error {
	return s.targetAction(ctx, id, (*elmapi.Client).AbortTargetMigration)
}

// ListResources lists target migration resources.
func (s *Service) ListResources(ctx context.Context, in ResourceInput) ([]elmapi.Node, error) {
	if err := requireTargetID(in.MigrationID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Repository) == "" {
		return nil, errors.New("repository is required")
	}
	client, err := s.targetClient()
	if err != nil {
		return nil, err
	}
	origins, err := resourceOrigins(in.Origin)
	if err != nil {
		return nil, err
	}
	state, err := resourceState(in.State)
	if err != nil {
		return nil, err
	}
	var nodes []elmapi.Node
	for _, origin := range origins {
		for node, iterErr := range client.IterNodes(ctx, int64(in.MigrationID), elmapi.ListNodesOptions{
			RepositoryNWO: strings.TrimSpace(in.Repository),
			Origin:        origin,
			State:         state,
		}) {
			if iterErr != nil {
				return nil, iterErr
			}
			nodes = append(nodes, node)
			if in.MaxResults > 0 && len(nodes) >= in.MaxResults {
				return nodes, nil
			}
		}
	}
	return nodes, nil
}

// RequestReport requests a target node report.
func (s *Service) RequestReport(ctx context.Context, in ReportInput) (json.RawMessage, error) {
	client, stage, err := s.reportClient(in)
	if err != nil {
		return nil, err
	}
	state, err := reportState(in.State)
	if err != nil {
		return nil, err
	}
	return client.CreateReport(ctx, int64(in.MigrationID), stage, state)
}

// ReportStatus fetches target report status.
func (s *Service) ReportStatus(ctx context.Context, in ReportInput) (json.RawMessage, error) {
	client, stage, err := s.reportClient(in)
	if err != nil {
		return nil, err
	}
	return client.GetReportStatus(ctx, int64(in.MigrationID), stage)
}

// ReportURL fetches a signed target report URL.
func (s *Service) ReportURL(ctx context.Context, in ReportInput) (json.RawMessage, error) {
	client, stage, err := s.reportClient(in)
	if err != nil {
		return nil, err
	}
	return client.GetReportURL(ctx, int64(in.MigrationID), stage)
}

// ListMannequins lists mannequins for a target organization.
func (s *Service) ListMannequins(ctx context.Context, organization string, includeReclaimed bool) ([]ghapi.MannequinRecord, error) {
	organization = strings.TrimSpace(organization)
	if organization == "" {
		return nil, errors.New("organization is required")
	}
	client, err := s.mannequinClient()
	if err != nil {
		return nil, err
	}
	orgID, err := client.OrganizationID(ctx, organization)
	if err != nil {
		return nil, err
	}
	mannequins, err := client.Mannequins(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return ghapi.ToMannequinRecords(mannequins, includeReclaimed), nil
}

// ExportMannequins writes mannequin records to a CSV file.
func (s *Service) ExportMannequins(ctx context.Context, organization, path string, includeReclaimed bool) error {
	records, err := s.ListMannequins(ctx, organization, includeReclaimed)
	if err != nil {
		return err
	}
	file, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return fmt.Errorf("creating mannequin CSV: %w", err)
	}
	defer func() { _ = file.Close() }()
	return ghapi.WriteMannequinCSV(file, records)
}

// ReclaimMannequins reclaims a single mannequin or a CSV batch.
func (s *Service) ReclaimMannequins(ctx context.Context, in MannequinReclaimInput, log ghapi.Logger) error {
	if strings.TrimSpace(in.Organization) == "" {
		return errors.New("organization is required")
	}
	client, err := s.mannequinClient()
	if err != nil {
		return err
	}
	reclaimer := ghapi.NewReclaimService(client, log)
	if strings.TrimSpace(in.CSVPath) != "" {
		file, err := os.Open(strings.TrimSpace(in.CSVPath))
		if err != nil {
			return fmt.Errorf("opening mannequin CSV: %w", err)
		}
		defer func() { _ = file.Close() }()
		records, err := ghapi.ReadMannequinCSV(file)
		if err != nil {
			return err
		}
		return reclaimer.ReclaimMannequins(ctx, records, in.Organization, in.Force, in.SkipInvitation)
	}
	if strings.TrimSpace(in.Mannequin) == "" || strings.TrimSpace(in.TargetUser) == "" {
		return errors.New("mannequin and target user are required for a single reclaim")
	}
	return reclaimer.ReclaimMannequin(
		ctx,
		strings.TrimSpace(in.Mannequin),
		strings.TrimSpace(in.MannequinID),
		strings.TrimSpace(in.TargetUser),
		strings.TrimSpace(in.Organization),
		in.Force,
		in.SkipInvitation,
	)
}

// GetConfiguration returns redacted stored configuration.
func (s *Service) GetConfiguration(context.Context) (*Configuration, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	store, err := creds.NewStore()
	if err != nil {
		return nil, err
	}
	sourceToken, err := store.Get(creds.SourceToken)
	if err != nil {
		return nil, err
	}
	targetToken, err := store.Get(creds.TargetToken)
	if err != nil {
		return nil, err
	}
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	return &Configuration{
		SourceURL:       cfg.SourceURL,
		SourceTokenSet:  sourceToken != "",
		TargetURL:       cfg.TargetURL,
		TargetTokenSet:  targetToken != "",
		ConfigPath:      path,
		CredentialStore: store.Location(),
	}, nil
}

// SaveConfiguration saves URLs and non-empty tokens.
func (s *Service) SaveConfiguration(ctx context.Context, in ConfigurationInput) error {
	_ = ctx
	if strings.TrimSpace(in.SourceURL) == "" {
		return errors.New("source URL is required")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.SourceURL = strings.TrimSpace(in.SourceURL)
	cfg.TargetURL = strings.TrimSpace(in.TargetURL)
	if err := cfg.Save(); err != nil {
		return err
	}
	store, err := creds.NewStore()
	if err != nil {
		return err
	}
	if in.SourceToken != "" {
		if err := store.Set(creds.SourceToken, in.SourceToken); err != nil {
			return err
		}
	}
	if in.TargetToken != "" {
		if err := store.Set(creds.TargetToken, in.TargetToken); err != nil {
			return err
		}
	}
	return nil
}

// ResetConfiguration removes stored configuration and credentials.
func (s *Service) ResetConfiguration(context.Context) error {
	if err := (&config.Config{}).Save(); err != nil {
		return err
	}
	return creds.ClearAll(creds.SourceToken, creds.TargetToken)
}

func (s *Service) sourceClient() (*elmapi.Client, error) {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return nil, err
	}
	ep, err := resolver.Source("", "")
	if err != nil {
		return nil, err
	}
	if ep.URL == "" || ep.Token == "" {
		return nil, errors.New("source URL and token are not configured")
	}
	return elmapi.NewClient(ep.URL, ep.Token), nil
}

func (s *Service) targetClient() (*elmapi.Client, error) {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return nil, err
	}
	ep, err := resolver.Target("", "")
	if err != nil {
		return nil, err
	}
	if ep.URL == "" || ep.Token == "" {
		return nil, errors.New("target URL and token are not configured")
	}
	return elmapi.NewClient(ep.URL, ep.Token), nil
}

func (s *Service) mannequinClient() (*ghapi.Client, error) {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return nil, err
	}
	ep, err := resolver.Target("", "")
	if err != nil {
		return nil, err
	}
	if ep.URL == "" || ep.Token == "" {
		return nil, errors.New("target URL and token are not configured")
	}
	return ghapi.NewClient(ep.URL, ep.Token), nil
}

func (s *Service) sourceAction(ctx context.Context, id SourceMigrationID, action func(*elmapi.Client, context.Context, string) error) error {
	if err := requireSourceID(id); err != nil {
		return err
	}
	client, err := s.sourceClient()
	if err != nil {
		return err
	}
	return action(client, ctx, string(id))
}

func (s *Service) targetAction(ctx context.Context, id TargetMigrationID, action func(*elmapi.Client, context.Context, int64) error) error {
	if err := requireTargetID(id); err != nil {
		return err
	}
	client, err := s.targetClient()
	if err != nil {
		return err
	}
	return action(client, ctx, int64(id))
}

func (s *Service) reportClient(in ReportInput) (*elmapi.Client, string, error) {
	if err := requireTargetID(in.MigrationID); err != nil {
		return nil, "", err
	}
	stage, err := reportStage(in.Stage)
	if err != nil {
		return nil, "", err
	}
	client, err := s.targetClient()
	return client, stage, err
}

func requireSourceID(id SourceMigrationID) error {
	if strings.TrimSpace(string(id)) == "" {
		return errors.New("source migration ID is required")
	}
	return nil
}

func requireTargetID(id TargetMigrationID) error {
	if id <= 0 {
		return errors.New("target migration ID must be a positive integer")
	}
	return nil
}

func visibility(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", elmapi.VisibilityInternal:
		return elmapi.VisibilityInternal, nil
	case elmapi.VisibilityPrivate:
		return elmapi.VisibilityPrivate, nil
	default:
		return "", errors.New("visibility must be internal or private")
	}
}

func resourceOrigins(value string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return []string{elmapi.OriginBackfill, elmapi.OriginLiveUpdate}, nil
	case "backfill":
		return []string{elmapi.OriginBackfill}, nil
	case "live-update", "live_update":
		return []string{elmapi.OriginLiveUpdate}, nil
	default:
		return nil, errors.New("origin must be backfill or live-update")
	}
}

func resourceState(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case "pending":
		return elmapi.StatePending, nil
	case "processed":
		return elmapi.StateProcessed, nil
	case "failed":
		return elmapi.StateFailed, nil
	case "eligible":
		return elmapi.StateEligible, nil
	default:
		return "", errors.New("state must be pending, processed, failed, or eligible")
	}
}

func reportStage(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "backfill":
		return elmapi.ReportStageBackfill, nil
	case "live-update", "live_updates", "live-updates":
		return elmapi.ReportStageLiveUpdates, nil
	default:
		return "", errors.New("stage must be backfill or live-update")
	}
}

func reportState(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return elmapi.ReportStateAll, nil
	case "migrated":
		return elmapi.ReportStateMigrated, nil
	case "unmigrated":
		return elmapi.ReportStateUnmigrated, nil
	default:
		return "", errors.New("report state must be all, migrated, or unmigrated")
	}
}

func normalizeTargetStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "in-progress", "in_progress":
		return elmapi.TargetMigrationStatusInProgress
	case "complete", "completed":
		return elmapi.TargetMigrationStatusComplete
	case "failed":
		return elmapi.TargetMigrationStatusFailed
	case "aborted":
		return elmapi.TargetMigrationStatusAborted
	case "expired":
		return elmapi.TargetMigrationStatusExpired
	case "paused":
		return elmapi.TargetMigrationStatusPaused
	default:
		return ""
	}
}

func ensureUniqueSourceMigration(ctx context.Context, client *elmapi.Client, req elmapi.CreateMigrationRequest) error {
	after := ""
	for {
		resp, err := client.ListMigrations(ctx, elmapi.ListMigrationsOptions{
			Status:   elmapi.StatusCreated,
			PageSize: 100,
			After:    after,
		})
		if err != nil {
			return fmt.Errorf("checking for an existing migration: %w", err)
		}
		for _, migration := range resp.Value.Migrations {
			if strings.EqualFold(migration.SourceOrganizationLogin, req.SourceOrganizationLogin) &&
				strings.EqualFold(migration.SourceRepositoryName, req.SourceRepositoryName) &&
				strings.EqualFold(migration.TargetOrganizationLogin, req.TargetOrganizationLogin) &&
				strings.EqualFold(migration.TargetRepositoryName, req.TargetRepositoryName) {
				return fmt.Errorf("a created migration already exists for these repositories (migration ID: %s)", migration.MigrationID)
			}
		}
		if resp.Value.NextCursor == "" {
			return nil
		}
		if resp.Value.NextCursor == after {
			return errors.New("checking for an existing migration: API returned a repeated pagination cursor")
		}
		after = resp.Value.NextCursor
	}
}

// ParseTargetMigrationID parses a positive target migration ID.
func ParseTargetMigrationID(value string) (TargetMigrationID, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("target migration ID must be a positive integer")
	}
	return TargetMigrationID(id), nil
}

// WriteMannequinCSV writes records in the shared CSV format.
func WriteMannequinCSV(w io.Writer, records []ghapi.MannequinRecord) error {
	return ghapi.WriteMannequinCSV(w, records)
}
