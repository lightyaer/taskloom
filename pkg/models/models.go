package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Project represents a collection of schedules and tasks
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	APIKey      string    `json:"api_key"`
	Paused      bool      `json:"paused"`
	RetryFailed bool      `json:"retry_failed"`
	TaskRate    int       `json:"task_rate"` // Tasks per minute
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// User represents a user of the system
type User struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	OAuthProvider string    `json:"oauth_provider"`
	OAuthID       string    `json:"oauth_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Schedule represents a task and when it should be executed
type Schedule struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"project_id"`
	Name           string          `json:"name"`
	CronExpression string          `json:"cron_expression"`
	URL            string          `json:"url"`
	Headers        json.RawMessage `json:"headers"` // Stored as JSON
	Body           json.RawMessage `json:"body"`    // Stored as JSON
	Timeout        int             `json:"timeout"` // In seconds
	Enabled        bool            `json:"enabled"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ExecutionLog represents a record of a schedule execution
type ExecutionLog struct {
	ID           string    `json:"id"`
	ScheduleID   string    `json:"schedule_id"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Status       string    `json:"status"` // success, failed
	ResponseCode int       `json:"response_code"`
	ResponseBody string    `json:"response_body"`
	Error        string    `json:"error"`
}

// NewProject creates a new project with defaults
func NewProject(name, description string) *Project {
	now := time.Now()
	return &Project{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		APIKey:      uuid.New().String(),
		Paused:      false,
		RetryFailed: true,
		TaskRate:    60, // Default to 60 tasks per minute
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewUser creates a new user
func NewUser(name, email, oauthProvider, oauthID string) *User {
	now := time.Now()
	return &User{
		ID:            uuid.New().String(),
		Name:          name,
		Email:         email,
		OAuthProvider: oauthProvider,
		OAuthID:       oauthID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// NewSchedule creates a new schedule with task blueprint
func NewSchedule(projectID, name, cronExpression, url string, headers, body json.RawMessage, timeout int) *Schedule {
	now := time.Now()
	return &Schedule{
		ID:             uuid.New().String(),
		ProjectID:      projectID,
		Name:           name,
		CronExpression: cronExpression,
		URL:            url,
		Headers:        headers,
		Body:           body,
		Timeout:        timeout,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// NewExecutionLog creates a new execution log
func NewExecutionLog(scheduleID string) *ExecutionLog {
	return &ExecutionLog{
		ID:         uuid.New().String(),
		ScheduleID: scheduleID,
		StartTime:  time.Now(),
	}
}

// Complete marks an execution log as complete
func (el *ExecutionLog) Complete(status string, responseCode int, responseBody, errMsg string) {
	el.EndTime = time.Now()
	el.Status = status
	el.ResponseCode = responseCode
	el.ResponseBody = responseBody
	el.Error = errMsg
} 