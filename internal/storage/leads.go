package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Lead is a fully enriched, scored project ready for the sales team.
type Lead struct {
	ID                      string
	RawProjectID            string
	Source                  string
	Title                   string
	Location                string
	ProjectValue            int64
	GeneralContractor       string
	Applicant               string // raw applicant from permit (may include phone/contact)
	Contractor              string // raw contractor from permit (may include phone/contact)
	SourceURL               string // direct link to the source document (PDF, page, etc.)
	ProjectType             string
	EstimatedCrewSize       int
	EstimatedDurationMonths int
	OutOfTownCrewLikely     bool
	PriorityScore           int
	PriorityReason          string
	SuggestedOutreachTiming string
	Notes                   string
	Status                  string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// LeadStore is the interface for persisting and querying enriched leads.
type LeadStore interface {
	Insert(ctx context.Context, l *Lead) error
	ListNew(ctx context.Context) ([]Lead, error)
	UpdateStatus(ctx context.Context, id, status string) error
}

type sqliteLeadStore struct{ db *sql.DB }

// NewLeadStore returns a SQLite-backed LeadStore.
func NewLeadStore(db *sql.DB) LeadStore {
	return &sqliteLeadStore{db: db}
}

func (s *sqliteLeadStore) Insert(ctx context.Context, l *Lead) error {
	now := time.Now().UTC()
	if l.ID == "" {
		l.ID = newUUID()
	}
	if l.Status == "" {
		l.Status = "new"
	}
	l.CreatedAt = now
	l.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO leads (
			id, raw_project_id, source, title, location, project_value,
			general_contractor, applicant, contractor, source_url, project_type,
			estimated_crew_size, estimated_duration_months, out_of_town_crew_likely,
			priority_score, priority_reason, suggested_outreach_timing,
			notes, status, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		l.ID, l.RawProjectID, l.Source, l.Title, l.Location, l.ProjectValue,
		l.GeneralContractor, l.Applicant, l.Contractor, l.SourceURL, l.ProjectType,
		l.EstimatedCrewSize, l.EstimatedDurationMonths, boolToInt(l.OutOfTownCrewLikely),
		l.PriorityScore, l.PriorityReason, l.SuggestedOutreachTiming,
		l.Notes, l.Status, now, now,
	)
	return err
}

func (s *sqliteLeadStore) ListNew(ctx context.Context) ([]Lead, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, raw_project_id, source, title, location, project_value,
		       general_contractor, applicant, contractor, source_url, project_type,
		       estimated_crew_size, estimated_duration_months, out_of_town_crew_likely,
		       priority_score, priority_reason, suggested_outreach_timing,
		       notes, status, created_at, updated_at
		FROM leads
		WHERE status = 'new'
		ORDER BY priority_score DESC, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var leads []Lead
	for rows.Next() {
		var l Lead
		var oot int
		if err := rows.Scan(
			&l.ID, &l.RawProjectID, &l.Source, &l.Title, &l.Location, &l.ProjectValue,
			&l.GeneralContractor, &l.Applicant, &l.Contractor, &l.SourceURL, &l.ProjectType,
			&l.EstimatedCrewSize, &l.EstimatedDurationMonths, &oot,
			&l.PriorityScore, &l.PriorityReason, &l.SuggestedOutreachTiming,
			&l.Notes, &l.Status, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		l.OutOfTownCrewLikely = oot == 1
		leads = append(leads, l)
	}
	return leads, rows.Err()
}

func (s *sqliteLeadStore) UpdateStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE leads SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("lead %s not found", id)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
