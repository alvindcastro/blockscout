-- 001_init.up.sql
-- Initial schema for blockscout.
-- For local dev this is applied inline by storage.Migrate().
-- In production (Phase 5) golang-migrate will version and apply these files.

CREATE TABLE IF NOT EXISTS raw_projects (
    id           TEXT PRIMARY KEY,
    source       TEXT NOT NULL,
    external_id  TEXT,
    raw_data     TEXT NOT NULL,
    collected_at DATETIME NOT NULL,
    hash         TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS leads (
    id                        TEXT PRIMARY KEY,
    raw_project_id            TEXT REFERENCES raw_projects(id),
    source                    TEXT,
    title                     TEXT,
    location                  TEXT,
    project_value             INTEGER,
    general_contractor        TEXT,
    project_type              TEXT,
    estimated_crew_size       INTEGER,
    estimated_duration_months INTEGER,
    out_of_town_crew_likely   INTEGER DEFAULT 0,
    priority_score            INTEGER,
    priority_reason           TEXT,
    suggested_outreach_timing TEXT,
    applicant                 TEXT,
    contractor                TEXT,
    source_url                TEXT,
    notes                     TEXT,
    status                    TEXT DEFAULT 'new',
    created_at                DATETIME NOT NULL,
    updated_at                DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS outreach_log (
    id        TEXT PRIMARY KEY,
    lead_id   TEXT REFERENCES leads(id),
    contact   TEXT,
    channel   TEXT,
    notes     TEXT,
    outcome   TEXT,
    logged_at DATETIME NOT NULL
);
