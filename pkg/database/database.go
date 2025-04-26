package database

import (
	"context"

	"github.com/lightyaer/taskloom/pkg/models"
)

// Repository is an interface for database operations
type Repository interface {
	// Project methods
	CreateProject(ctx context.Context, project *models.Project) error
	GetProject(ctx context.Context, id string) (*models.Project, error)
	UpdateProject(ctx context.Context, project *models.Project) error
	DeleteProject(ctx context.Context, id string) error
	ListProjects(ctx context.Context) ([]*models.Project, error)

	// User methods
	CreateUser(ctx context.Context, user *models.User) error
	GetUser(ctx context.Context, id string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUserByOAuth(ctx context.Context, provider, id string) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	DeleteUser(ctx context.Context, id string) error

	// Schedule methods
	CreateSchedule(ctx context.Context, schedule *models.Schedule) error
	GetSchedule(ctx context.Context, id string) (*models.Schedule, error)
	UpdateSchedule(ctx context.Context, schedule *models.Schedule) error
	DeleteSchedule(ctx context.Context, id string) error
	ListSchedulesByProject(ctx context.Context, projectID string) ([]*models.Schedule, error)
	GetEnabledSchedules(ctx context.Context) ([]*models.Schedule, error)

	// Execution log methods
	CreateExecutionLog(ctx context.Context, log *models.ExecutionLog) error
	UpdateExecutionLog(ctx context.Context, log *models.ExecutionLog) error
	ListExecutionLogsBySchedule(ctx context.Context, scheduleID string, limit, offset int) ([]*models.ExecutionLog, error)

	// Database setup
	InitDB(ctx context.Context) error
	Close() error
} 