package state

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"

	"github.com/Airgoods-Inc/previewctl/src/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

// PostgresStateAdapter persists state to a PostgreSQL database.
// Each environment is stored as a JSONB row, scoped by project name.
// Deleted environments are hard-deleted from the database.
// Run RunMigrations before first use to ensure the schema is up to date.
type PostgresStateAdapter struct {
	db      *sql.DB
	project string
}

type EnvironmentActivity struct {
	Project           string
	Name              string
	LastProxyAccessAt *time.Time
	LastCLIAccessAt   *time.Time
	StateUpdatedAt    time.Time
	LastActivityAt    time.Time
}

// NewPostgresStateAdapter creates a new Postgres-backed state adapter.
// The dsn should be a valid PostgreSQL connection string.
// The project name scopes state so multiple projects can share one database.
// The caller should run RunMigrations separately (via `previewctl migrate`)
// before using the adapter for the first time.
func NewPostgresStateAdapter(dsn, project string) (*PostgresStateAdapter, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresStateAdapter{db: db, project: project}, nil
}

// RunMigrations applies all pending goose migrations from the embedded SQL files.
// Should be called explicitly via `previewctl migrate` before first use.
func RunMigrations(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("opening postgres connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

func (a *PostgresStateAdapter) Load(ctx context.Context) (*domain.State, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT name, data FROM environments WHERE project = $1 AND is_deleted = false`, a.project)
	if err != nil {
		return nil, fmt.Errorf("querying environments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	state := domain.NewState()
	for rows.Next() {
		var name string
		var data []byte
		if err := rows.Scan(&name, &data); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		var entry domain.EnvironmentEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("unmarshaling environment '%s': %w", name, err)
		}
		state.Environments[name] = &entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return state, nil
}

func (a *PostgresStateAdapter) Save(ctx context.Context, state *domain.State) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Soft-delete all active environments for this project
	if _, err := tx.ExecContext(ctx,
		`UPDATE environments SET is_deleted = true, updated_at = now() WHERE project = $1 AND is_deleted = false`,
		a.project); err != nil {
		return fmt.Errorf("soft-deleting environments: %w", err)
	}

	// Upsert all current environments
	for name, entry := range state.Environments {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshaling environment '%s': %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO environments (project, name, data, branch, status, is_deleted, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, false, $6, now())
			ON CONFLICT (project, name)
			DO UPDATE SET data = EXCLUDED.data, branch = EXCLUDED.branch, status = EXCLUDED.status,
			             is_deleted = false, updated_at = now()
		`, a.project, name, data, entry.Branch, string(entry.Status), entry.CreatedAt); err != nil {
			return fmt.Errorf("inserting environment '%s': %w", name, err)
		}
	}

	return tx.Commit()
}

func (a *PostgresStateAdapter) GetEnvironment(ctx context.Context, name string) (*domain.EnvironmentEntry, error) {
	var data []byte
	err := a.db.QueryRowContext(ctx,
		`SELECT data FROM environments WHERE project = $1 AND name = $2 AND is_deleted = false`,
		a.project, name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying environment '%s': %w", name, err)
	}

	var entry domain.EnvironmentEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshaling environment '%s': %w", name, err)
	}
	return &entry, nil
}

func (a *PostgresStateAdapter) SetEnvironment(ctx context.Context, name string, entry *domain.EnvironmentEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling environment '%s': %w", name, err)
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO environments (project, name, data, branch, status, is_deleted, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, false, $6, now())
		ON CONFLICT (project, name)
		DO UPDATE SET data = EXCLUDED.data, branch = EXCLUDED.branch, status = EXCLUDED.status,
		             is_deleted = false, updated_at = now()
	`, a.project, name, data, entry.Branch, string(entry.Status), entry.CreatedAt)
	if err != nil {
		return fmt.Errorf("upserting environment '%s': %w", name, err)
	}
	return nil
}

func (a *PostgresStateAdapter) RemoveEnvironment(ctx context.Context, name string) error {
	_, err := a.db.ExecContext(ctx,
		`DELETE FROM environments WHERE project = $1 AND name = $2`,
		a.project, name)
	if err != nil {
		return fmt.Errorf("deleting environment '%s': %w", name, err)
	}
	return nil
}

func (a *PostgresStateAdapter) RecordProxyActivity(ctx context.Context, name string, accessedAt time.Time, host string, status int) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO environment_proxy_activity (project, name, last_access_at, last_host, last_status, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (project, name)
		DO UPDATE SET
			last_access_at = GREATEST(environment_proxy_activity.last_access_at, EXCLUDED.last_access_at),
			last_host = CASE
				WHEN EXCLUDED.last_access_at >= environment_proxy_activity.last_access_at THEN EXCLUDED.last_host
				ELSE environment_proxy_activity.last_host
			END,
			last_status = CASE
				WHEN EXCLUDED.last_access_at >= environment_proxy_activity.last_access_at THEN EXCLUDED.last_status
				ELSE environment_proxy_activity.last_status
			END,
			updated_at = now()
	`, a.project, name, accessedAt, host, status)
	if err != nil {
		return fmt.Errorf("recording proxy activity for '%s': %w", name, err)
	}
	return nil
}

func (a *PostgresStateAdapter) RecordCLIActivity(ctx context.Context, name string, accessedAt time.Time, command, actor, machine string) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO environment_cli_activity (project, name, last_access_at, last_command, last_actor, last_machine, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (project, name)
		DO UPDATE SET
			last_access_at = GREATEST(environment_cli_activity.last_access_at, EXCLUDED.last_access_at),
			last_command = CASE
				WHEN EXCLUDED.last_access_at >= environment_cli_activity.last_access_at THEN EXCLUDED.last_command
				ELSE environment_cli_activity.last_command
			END,
			last_actor = CASE
				WHEN EXCLUDED.last_access_at >= environment_cli_activity.last_access_at THEN EXCLUDED.last_actor
				ELSE environment_cli_activity.last_actor
			END,
			last_machine = CASE
				WHEN EXCLUDED.last_access_at >= environment_cli_activity.last_access_at THEN EXCLUDED.last_machine
				ELSE environment_cli_activity.last_machine
			END,
			updated_at = now()
	`, a.project, name, accessedAt, command, actor, machine)
	if err != nil {
		return fmt.Errorf("recording cli activity for '%s': %w", name, err)
	}
	return nil
}

func (a *PostgresStateAdapter) ListEnvironmentActivity(ctx context.Context) ([]EnvironmentActivity, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT
			e.project,
			e.name,
			pa.last_access_at AS last_proxy_access_at,
			ca.last_access_at AS last_cli_access_at,
			e.updated_at AS state_updated_at,
			GREATEST(
				COALESCE(pa.last_access_at, '-infinity'::timestamptz),
				COALESCE(ca.last_access_at, '-infinity'::timestamptz),
				e.updated_at
			) AS last_activity_at
		FROM environments e
		LEFT JOIN environment_proxy_activity pa
			ON pa.project = e.project AND pa.name = e.name
		LEFT JOIN environment_cli_activity ca
			ON ca.project = e.project AND ca.name = e.name
		WHERE e.project = $1 AND e.is_deleted = false
		ORDER BY e.name
	`, a.project)
	if err != nil {
		return nil, fmt.Errorf("querying environment activity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var activities []EnvironmentActivity
	for rows.Next() {
		var activity EnvironmentActivity
		var proxyAt, cliAt sql.NullTime
		if err := rows.Scan(
			&activity.Project,
			&activity.Name,
			&proxyAt,
			&cliAt,
			&activity.StateUpdatedAt,
			&activity.LastActivityAt,
		); err != nil {
			return nil, fmt.Errorf("scanning environment activity: %w", err)
		}
		if proxyAt.Valid {
			activity.LastProxyAccessAt = &proxyAt.Time
		}
		if cliAt.Valid {
			activity.LastCLIAccessAt = &cliAt.Time
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating environment activity: %w", err)
	}
	return activities, nil
}

// Close closes the underlying database connection.
func (a *PostgresStateAdapter) Close() error {
	return a.db.Close()
}
