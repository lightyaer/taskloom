package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lightyaer/taskloom/pkg/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// TursoRepository implements the Repository interface for Turso
type TursoRepository struct {
	db *sql.DB
}

// NewTursoRepository creates a new TursoRepository
func NewTursoRepository(url, authToken string) (*TursoRepository, error) {
	dsn := url
	if authToken != "" {
		dsn = fmt.Sprintf("%s?authToken=%s", url, authToken)
	}

	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)                  // Limit maximum number of open connections
	db.SetMaxIdleConns(5)                   // Limit idle connections
	db.SetConnMaxLifetime(30 * time.Minute) // Recycle connections after 30 minutes

	repo := &TursoRepository{db: db}

	// Enable WAL mode if using SQLite (not Turso cloud)
	if isLocalSQLite(url) {
		if err := repo.enableWALMode(); err != nil {
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	return repo, nil
}

// isLocalSQLite checks if the URL is for a local SQLite database
func isLocalSQLite(url string) bool {
	return url == "file:taskloom.db" || url == ":memory:" ||
		len(url) > 5 && url[:5] == "file:"
}

// enableWALMode enables Write-Ahead Logging mode for better concurrency
func (r *TursoRepository) enableWALMode() error {
	// Execute pragmas to enable WAL mode
	pragmas := []string{
		"PRAGMA journal_mode=WAL",   // Enable WAL mode
		"PRAGMA synchronous=NORMAL", // Sync less often for better performance
		"PRAGMA busy_timeout=5000",  // Wait up to 5 seconds when the database is busy
		"PRAGMA foreign_keys=ON",    // Enforce foreign key constraints
		"PRAGMA cache_size=-64000",  // Use a 64MB cache (negative means KB)
		"PRAGMA temp_store=MEMORY",  // Store temp tables in memory
	}

	for _, pragma := range pragmas {
		_, err := r.db.Exec(pragma)
		if err != nil {
			return fmt.Errorf("failed to execute %s: %w", pragma, err)
		}
	}

	// Verify WAL mode is enabled
	var journalMode string
	err := r.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		return fmt.Errorf("failed to check journal mode: %w", err)
	}

	if journalMode != "wal" {
		return fmt.Errorf("failed to enable WAL mode, journal mode is: %s", journalMode)
	}

	return nil
}

// InitDB initializes the database with tables
func (r *TursoRepository) InitDB(ctx context.Context) error {
	// Create tables if they don't exist
	queries := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			api_key TEXT NOT NULL,
			paused BOOLEAN DEFAULT FALSE,
			retry_failed BOOLEAN DEFAULT TRUE,
			task_rate INTEGER DEFAULT 60,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			oauth_provider TEXT,
			oauth_id TEXT,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			UNIQUE(oauth_provider, oauth_id)
		)`,
		`CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			name TEXT NOT NULL,
			cron_expression TEXT NOT NULL,
			url TEXT NOT NULL,
			headers TEXT,
			body TEXT,
			timeout INTEGER DEFAULT 30,
			enabled BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS execution_logs (
			id TEXT PRIMARY KEY,
			schedule_id TEXT NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP,
			status TEXT,
			response_code INTEGER,
			response_body TEXT,
			error TEXT,
			FOREIGN KEY (schedule_id) REFERENCES schedules(id) ON DELETE CASCADE
		)`,
		`DROP TABLE IF EXISTS tasks`,
	}

	// Create indexes for better query performance
	indexes := []string{
		// Execution logs indexes
		`CREATE INDEX IF NOT EXISTS idx_execution_logs_schedule_id ON execution_logs(schedule_id)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_logs_start_time ON execution_logs(start_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_logs_status ON execution_logs(status)`,

		// Schedules indexes
		`CREATE INDEX IF NOT EXISTS idx_schedules_project_id ON schedules(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_schedules_enabled ON schedules(enabled)`,

		// Users indexes
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
		`CREATE INDEX IF NOT EXISTS idx_users_oauth ON users(oauth_provider, oauth_id)`,
	}

	// Execute all table creation queries first
	for _, query := range queries {
		_, err := r.db.ExecContext(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to execute query: %s: %w", query, err)
		}
	}

	// Then execute all index creation queries
	for _, query := range indexes {
		_, err := r.db.ExecContext(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to execute query: %s: %w", query, err)
		}
	}

	// Analyze the database to optimize query plans
	_, err := r.db.ExecContext(ctx, "ANALYZE")
	if err != nil {
		return fmt.Errorf("failed to analyze database: %w", err)
	}

	return nil
}

// Close closes the database connection
func (r *TursoRepository) Close() error {
	return r.db.Close()
}

// Project methods

// CreateProject creates a new project
func (r *TursoRepository) CreateProject(ctx context.Context, project *models.Project) error {
	query := `INSERT INTO projects (id, name, description, api_key, paused, retry_failed, task_rate, created_at, updated_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(
		ctx,
		query,
		project.ID,
		project.Name,
		project.Description,
		project.APIKey,
		project.Paused,
		project.RetryFailed,
		project.TaskRate,
		project.CreatedAt,
		project.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	return nil
}

// GetProject gets a project by ID
func (r *TursoRepository) GetProject(ctx context.Context, id string) (*models.Project, error) {
	query := `SELECT id, name, description, api_key, paused, retry_failed, task_rate, created_at, updated_at
			  FROM projects WHERE id = ?`

	var project models.Project
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&project.ID,
		&project.Name,
		&project.Description,
		&project.APIKey,
		&project.Paused,
		&project.RetryFailed,
		&project.TaskRate,
		&project.CreatedAt,
		&project.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	return &project, nil
}

// UpdateProject updates an existing project
func (r *TursoRepository) UpdateProject(ctx context.Context, project *models.Project) error {
	query := `UPDATE projects
			  SET name = ?, description = ?, api_key = ?, paused = ?, retry_failed = ?, task_rate = ?, updated_at = ?
			  WHERE id = ?`

	project.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(
		ctx,
		query,
		project.Name,
		project.Description,
		project.APIKey,
		project.Paused,
		project.RetryFailed,
		project.TaskRate,
		project.UpdatedAt,
		project.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}

	return nil
}

// DeleteProject deletes a project
func (r *TursoRepository) DeleteProject(ctx context.Context, id string) error {
	query := `DELETE FROM projects WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	return nil
}

// ListProjects lists all projects
func (r *TursoRepository) ListProjects(ctx context.Context) ([]*models.Project, error) {
	query := `SELECT id, name, description, api_key, paused, retry_failed, task_rate, created_at, updated_at
			  FROM projects ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []*models.Project
	for rows.Next() {
		var project models.Project
		err := rows.Scan(
			&project.ID,
			&project.Name,
			&project.Description,
			&project.APIKey,
			&project.Paused,
			&project.RetryFailed,
			&project.TaskRate,
			&project.CreatedAt,
			&project.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projects = append(projects, &project)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating projects: %w", err)
	}

	return projects, nil
}

// User methods

// CreateUser creates a new user
func (r *TursoRepository) CreateUser(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (id, name, email, oauth_provider, oauth_id, created_at, updated_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(
		ctx,
		query,
		user.ID,
		user.Name,
		user.Email,
		user.OAuthProvider,
		user.OAuthID,
		user.CreatedAt,
		user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetUser gets a user by ID
func (r *TursoRepository) GetUser(ctx context.Context, id string) (*models.User, error) {
	query := `SELECT id, name, email, oauth_provider, oauth_id, created_at, updated_at
			  FROM users WHERE id = ?`

	var user models.User
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.OAuthProvider,
		&user.OAuthID,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetUserByEmail gets a user by email
func (r *TursoRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `SELECT id, name, email, oauth_provider, oauth_id, created_at, updated_at
			  FROM users WHERE email = ?`

	var user models.User
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.OAuthProvider,
		&user.OAuthID,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found with email: %s", email)
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// GetUserByOAuth gets a user by OAuth provider and ID
func (r *TursoRepository) GetUserByOAuth(ctx context.Context, provider, id string) (*models.User, error) {
	query := `SELECT id, name, email, oauth_provider, oauth_id, created_at, updated_at
			  FROM users WHERE oauth_provider = ? AND oauth_id = ?`

	var user models.User
	err := r.db.QueryRowContext(ctx, query, provider, id).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.OAuthProvider,
		&user.OAuthID,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found with OAuth provider: %s, id: %s", provider, id)
		}
		return nil, fmt.Errorf("failed to get user by OAuth: %w", err)
	}

	return &user, nil
}

// UpdateUser updates an existing user
func (r *TursoRepository) UpdateUser(ctx context.Context, user *models.User) error {
	query := `UPDATE users
			  SET name = ?, email = ?, oauth_provider = ?, oauth_id = ?, updated_at = ?
			  WHERE id = ?`

	user.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(
		ctx,
		query,
		user.Name,
		user.Email,
		user.OAuthProvider,
		user.OAuthID,
		user.UpdatedAt,
		user.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// DeleteUser deletes a user
func (r *TursoRepository) DeleteUser(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// Schedule methods

// CreateSchedule creates a new schedule
func (r *TursoRepository) CreateSchedule(ctx context.Context, schedule *models.Schedule) error {
	query := `INSERT INTO schedules (id, project_id, name, cron_expression, url, headers, body, timeout, enabled, created_at, updated_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	headersJSON, err := json.Marshal(schedule.Headers)
	if err != nil {
		return fmt.Errorf("failed to marshal headers: %w", err)
	}

	bodyJSON, err := json.Marshal(schedule.Body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	_, err = r.db.ExecContext(
		ctx,
		query,
		schedule.ID,
		schedule.ProjectID,
		schedule.Name,
		schedule.CronExpression,
		schedule.URL,
		string(headersJSON),
		string(bodyJSON),
		schedule.Timeout,
		schedule.Enabled,
		schedule.CreatedAt,
		schedule.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create schedule: %w", err)
	}

	return nil
}

// GetSchedule gets a schedule by ID
func (r *TursoRepository) GetSchedule(ctx context.Context, id string) (*models.Schedule, error) {
	query := `SELECT id, project_id, name, cron_expression, url, headers, body, timeout, enabled, created_at, updated_at
			  FROM schedules WHERE id = ?`

	var schedule models.Schedule
	var headersStr, bodyStr string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&schedule.ID,
		&schedule.ProjectID,
		&schedule.Name,
		&schedule.CronExpression,
		&schedule.URL,
		&headersStr,
		&bodyStr,
		&schedule.Timeout,
		&schedule.Enabled,
		&schedule.CreatedAt,
		&schedule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("schedule not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get schedule: %w", err)
	}

	schedule.Headers = json.RawMessage(headersStr)
	schedule.Body = json.RawMessage(bodyStr)

	return &schedule, nil
}

// UpdateSchedule updates an existing schedule
func (r *TursoRepository) UpdateSchedule(ctx context.Context, schedule *models.Schedule) error {
	query := `UPDATE schedules
			  SET project_id = ?, name = ?, cron_expression = ?, url = ?, headers = ?, body = ?, timeout = ?, enabled = ?, updated_at = ?
			  WHERE id = ?`

	headersJSON, err := json.Marshal(schedule.Headers)
	if err != nil {
		return fmt.Errorf("failed to marshal headers: %w", err)
	}

	bodyJSON, err := json.Marshal(schedule.Body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	schedule.UpdatedAt = time.Now()
	_, err = r.db.ExecContext(
		ctx,
		query,
		schedule.ProjectID,
		schedule.Name,
		schedule.CronExpression,
		schedule.URL,
		string(headersJSON),
		string(bodyJSON),
		schedule.Timeout,
		schedule.Enabled,
		schedule.UpdatedAt,
		schedule.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update schedule: %w", err)
	}

	return nil
}

// DeleteSchedule deletes a schedule
func (r *TursoRepository) DeleteSchedule(ctx context.Context, id string) error {
	query := `DELETE FROM schedules WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete schedule: %w", err)
	}

	return nil
}

// ListSchedulesByProject lists all schedules for a project
func (r *TursoRepository) ListSchedulesByProject(ctx context.Context, projectID string) ([]*models.Schedule, error) {
	query := `SELECT id, project_id, name, cron_expression, url, headers, body, timeout, enabled, created_at, updated_at
			  FROM schedules WHERE project_id = ? ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*models.Schedule
	for rows.Next() {
		var schedule models.Schedule
		var headersStr, bodyStr string
		err := rows.Scan(
			&schedule.ID,
			&schedule.ProjectID,
			&schedule.Name,
			&schedule.CronExpression,
			&schedule.URL,
			&headersStr,
			&bodyStr,
			&schedule.Timeout,
			&schedule.Enabled,
			&schedule.CreatedAt,
			&schedule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan schedule: %w", err)
		}
		schedule.Headers = json.RawMessage(headersStr)
		schedule.Body = json.RawMessage(bodyStr)
		schedules = append(schedules, &schedule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating schedules: %w", err)
	}

	return schedules, nil
}

// GetEnabledSchedules gets all enabled schedules
func (r *TursoRepository) GetEnabledSchedules(ctx context.Context) ([]*models.Schedule, error) {
	query := `SELECT s.id, s.project_id, s.name, s.cron_expression, s.url, s.headers, s.body, s.timeout, s.enabled, s.created_at, s.updated_at
			  FROM schedules s
			  JOIN projects p ON s.project_id = p.id
			  WHERE s.enabled = true AND p.paused = false
			  ORDER BY s.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*models.Schedule
	for rows.Next() {
		var schedule models.Schedule
		var headersStr, bodyStr string
		err := rows.Scan(
			&schedule.ID,
			&schedule.ProjectID,
			&schedule.Name,
			&schedule.CronExpression,
			&schedule.URL,
			&headersStr,
			&bodyStr,
			&schedule.Timeout,
			&schedule.Enabled,
			&schedule.CreatedAt,
			&schedule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan schedule: %w", err)
		}
		schedule.Headers = json.RawMessage(headersStr)
		schedule.Body = json.RawMessage(bodyStr)
		schedules = append(schedules, &schedule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating schedules: %w", err)
	}

	return schedules, nil
}

// Execution log methods

// CreateExecutionLog creates a new execution log
func (r *TursoRepository) CreateExecutionLog(ctx context.Context, log *models.ExecutionLog) error {
	query := `INSERT INTO execution_logs (id, schedule_id, start_time, end_time, status, response_code, response_body, error)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(
		ctx,
		query,
		log.ID,
		log.ScheduleID,
		log.StartTime,
		log.EndTime,
		log.Status,
		log.ResponseCode,
		log.ResponseBody,
		log.Error,
	)

	if err != nil {
		return fmt.Errorf("failed to create execution log: %w", err)
	}

	return nil
}

// UpdateExecutionLog updates an existing execution log
func (r *TursoRepository) UpdateExecutionLog(ctx context.Context, log *models.ExecutionLog) error {
	query := `UPDATE execution_logs
			  SET end_time = ?, status = ?, response_code = ?, response_body = ?, error = ?
			  WHERE id = ?`

	_, err := r.db.ExecContext(
		ctx,
		query,
		log.EndTime,
		log.Status,
		log.ResponseCode,
		log.ResponseBody,
		log.Error,
		log.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update execution log: %w", err)
	}

	return nil
}

// ListExecutionLogsBySchedule lists all execution logs for a schedule
func (r *TursoRepository) ListExecutionLogsBySchedule(ctx context.Context, scheduleID string, limit, offset int) ([]*models.ExecutionLog, error) {
	query := `SELECT id, schedule_id, start_time, end_time, status, response_code, response_body, error
			  FROM execution_logs
			  WHERE schedule_id = ?
			  ORDER BY start_time DESC
			  LIMIT ? OFFSET ?`

	rows, err := r.db.QueryContext(ctx, query, scheduleID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution logs: %w", err)
	}
	defer rows.Close()

	var logs []*models.ExecutionLog
	for rows.Next() {
		var log models.ExecutionLog
		err := rows.Scan(
			&log.ID,
			&log.ScheduleID,
			&log.StartTime,
			&log.EndTime,
			&log.Status,
			&log.ResponseCode,
			&log.ResponseBody,
			&log.Error,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution log: %w", err)
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating execution logs: %w", err)
	}

	return logs, nil
}
