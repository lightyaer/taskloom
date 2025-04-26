package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lightyaer/taskloom/pkg/models"
)

// MockRepository is a mock implementation of the database.Repository interface for testing
type MockRepository struct {
	projects      map[string]*models.Project
	schedules     map[string]*models.Schedule
	executionLogs map[string]*models.ExecutionLog
	mutex         sync.Mutex
}

// NewMockRepository creates a new mock repository for testing
func NewMockRepository() *MockRepository {
	return &MockRepository{
		projects:      make(map[string]*models.Project),
		schedules:     make(map[string]*models.Schedule),
		executionLogs: make(map[string]*models.ExecutionLog),
	}
}

// Project methods
func (r *MockRepository) CreateProject(ctx context.Context, project *models.Project) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.projects[project.ID] = project
	return nil
}

func (r *MockRepository) GetProject(ctx context.Context, id string) (*models.Project, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	project, ok := r.projects[id]
	if !ok {
		return nil, nil
	}
	return project, nil
}

func (r *MockRepository) UpdateProject(ctx context.Context, project *models.Project) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.projects[project.ID] = project
	return nil
}

func (r *MockRepository) DeleteProject(ctx context.Context, id string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.projects, id)
	return nil
}

func (r *MockRepository) ListProjects(ctx context.Context) ([]*models.Project, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	projects := make([]*models.Project, 0, len(r.projects))
	for _, project := range r.projects {
		projects = append(projects, project)
	}
	return projects, nil
}

// User methods (empty implementations for interface compliance)
func (r *MockRepository) CreateUser(ctx context.Context, user *models.User) error {
	return nil
}

func (r *MockRepository) GetUser(ctx context.Context, id string) (*models.User, error) {
	return nil, nil
}

func (r *MockRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return nil, nil
}

func (r *MockRepository) GetUserByOAuth(ctx context.Context, provider, id string) (*models.User, error) {
	return nil, nil
}

func (r *MockRepository) UpdateUser(ctx context.Context, user *models.User) error {
	return nil
}

func (r *MockRepository) DeleteUser(ctx context.Context, id string) error {
	return nil
}

// Schedule methods
func (r *MockRepository) CreateSchedule(ctx context.Context, schedule *models.Schedule) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.schedules[schedule.ID] = schedule
	return nil
}

func (r *MockRepository) GetSchedule(ctx context.Context, id string) (*models.Schedule, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	schedule, ok := r.schedules[id]
	if !ok {
		return nil, nil
	}
	return schedule, nil
}

func (r *MockRepository) UpdateSchedule(ctx context.Context, schedule *models.Schedule) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.schedules[schedule.ID] = schedule
	return nil
}

func (r *MockRepository) DeleteSchedule(ctx context.Context, id string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.schedules, id)
	return nil
}

func (r *MockRepository) ListSchedulesByProject(ctx context.Context, projectID string) ([]*models.Schedule, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	schedules := make([]*models.Schedule, 0)
	for _, schedule := range r.schedules {
		if schedule.ProjectID == projectID {
			schedules = append(schedules, schedule)
		}
	}
	return schedules, nil
}

func (r *MockRepository) GetEnabledSchedules(ctx context.Context) ([]*models.Schedule, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	schedules := make([]*models.Schedule, 0)
	for _, schedule := range r.schedules {
		if schedule.Enabled {
			project, ok := r.projects[schedule.ProjectID]
			if ok && !project.Paused {
				schedules = append(schedules, schedule)
			}
		}
	}
	return schedules, nil
}

// Execution log methods
func (r *MockRepository) CreateExecutionLog(ctx context.Context, log *models.ExecutionLog) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.executionLogs[log.ID] = log
	return nil
}

func (r *MockRepository) UpdateExecutionLog(ctx context.Context, log *models.ExecutionLog) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.executionLogs[log.ID] = log
	return nil
}

func (r *MockRepository) ListExecutionLogsBySchedule(ctx context.Context, scheduleID string, limit, offset int) ([]*models.ExecutionLog, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	logs := make([]*models.ExecutionLog, 0)
	for _, log := range r.executionLogs {
		if log.ScheduleID == scheduleID {
			logs = append(logs, log)
		}
	}
	return logs, nil
}

// Database setup
func (r *MockRepository) InitDB(ctx context.Context) error {
	return nil
}

func (r *MockRepository) Close() error {
	return nil
}

// Tests for RateLimiter
func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(10, 10)
	
	// Should be able to take 10 tokens immediately
	for i := 0; i < 10; i++ {
		if !limiter.TakeToken() {
			t.Errorf("Failed to take token %d", i)
		}
	}
	
	// Should not be able to take more tokens
	if limiter.TakeToken() {
		t.Errorf("Should not be able to take more tokens")
	}
	
	// Wait for refill
	time.Sleep(1 * time.Second)
	
	// Should be able to take some more tokens
	if !limiter.TakeToken() {
		t.Errorf("Failed to take token after refill")
	}
}

// Tests for the Scheduler
func TestSchedulerExecuteTask(t *testing.T) {
	// Create an HTTP test server to receive the task execution
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer testServer.Close()
	
	// Create repository and scheduler
	repo := NewMockRepository()
	ctx := context.Background()
	scheduler := NewScheduler(repo)
	
	// Create project
	project := models.NewProject("Test Project", "Test description")
	repo.CreateProject(ctx, project)
	
	// Create schedule with task details
	headers, _ := json.Marshal(map[string]string{"Content-Type": "application/json"})
	body, _ := json.Marshal(map[string]string{"test": "value"})
	schedule := models.NewSchedule(project.ID, "Test Schedule", "* * * * *", testServer.URL, headers, body, 30)
	repo.CreateSchedule(ctx, schedule)
	
	// Start scheduler
	scheduler.Start(ctx)
	defer scheduler.Stop()
	
	// Manually execute the task (since we don't want to wait for the cron schedule)
	scheduler.executeTask(ctx, schedule, project)
	
	// Wait for task to complete
	time.Sleep(100 * time.Millisecond)
	
	// Check execution log was created
	logs, _ := repo.ListExecutionLogsBySchedule(ctx, schedule.ID, 10, 0)
	
	found := false
	for _, log := range logs {
		if log.ScheduleID == schedule.ID && log.Status == "success" {
			found = true
			break
		}
	}
	
	if !found {
		t.Error("Could not find successful execution log for schedule")
	}
}

func TestSchedulerRateLimiting(t *testing.T) {
	// Create repository and scheduler
	repo := NewMockRepository()
	ctx := context.Background()
	scheduler := NewScheduler(repo)
	
	// Create project with low rate limit
	project := models.NewProject("Rate Limited Project", "Test rate limiting")
	project.TaskRate = 2 // Only 2 tasks per minute
	repo.CreateProject(ctx, project)
	
	// Create schedule
	headers, _ := json.Marshal(map[string]string{"Content-Type": "application/json"})
	body, _ := json.Marshal(map[string]string{"test": "value"})
	schedule := models.NewSchedule(project.ID, "Test Schedule", "* * * * *", "http://example.com", headers, body, 30)
	repo.CreateSchedule(ctx, schedule)
	
	// Create a test server that sleeps a bit to simulate processing time
	requestCount := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		requestCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer testServer.Close()
	
	// Initialize rate limiter manually for testing
	scheduler.rateLimiter[project.ID] = NewRateLimiter(project.TaskRate, project.TaskRate)
	
	// Create multiple schedules with the same settings
	schedules := make([]*models.Schedule, 5)
	for i := 0; i < 5; i++ {
		schedule := models.NewSchedule(
			project.ID,
			"Schedule "+string(rune('A'+i)),
			"* * * * *",
			testServer.URL,
			headers,
			body,
			30,
		)
		repo.CreateSchedule(ctx, schedule)
		schedules[i] = schedule
	}
	
	// Execute all schedules at once
	for _, s := range schedules {
		go func(sch *models.Schedule) {
			scheduler.executeTask(ctx, sch, project)
		}(s)
	}
	
	// Wait a bit
	time.Sleep(100 * time.Millisecond)
	
	// Should only have processed 2 tasks due to rate limiting
	if requestCount > 2 {
		t.Errorf("Rate limiting failed: processed %d requests, expected <= 2", requestCount)
	}
}

func TestSchedulerRetryFailed(t *testing.T) {
	// Create repository and scheduler
	repo := NewMockRepository()
	ctx := context.Background()
	scheduler := NewScheduler(repo)
	
	// Create project with retries enabled
	project := models.NewProject("Retry Project", "Test retries")
	project.RetryFailed = true
	repo.CreateProject(ctx, project)
	
	// Create a failing server first, then succeed on retry
	failCount := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failCount == 0 {
			failCount++
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "test failure"}`))
			return
		}
		
		// Success on retry
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer testServer.Close()
	
	// Create schedule with failing task details
	headers, _ := json.Marshal(map[string]string{"Content-Type": "application/json"})
	body, _ := json.Marshal(map[string]string{"test": "value"})
	schedule := models.NewSchedule(project.ID, "Failing Schedule", "* * * * *", testServer.URL, headers, body, 30)
	repo.CreateSchedule(ctx, schedule)
	
	// Execute the task manually
	scheduler.executeTask(ctx, schedule, project)
	
	// Wait for execution and retry
	time.Sleep(6 * time.Second)
	
	// Check both failed execution and successful retry logs
	logs, _ := repo.ListExecutionLogsBySchedule(ctx, schedule.ID, 10, 0)
	
	if len(logs) < 2 {
		t.Errorf("Expected at least 2 logs (original + retry), got %d", len(logs))
	}
	
	// Should have at least one success
	hasSuccess := false
	for _, log := range logs {
		if log.Status == "success" {
			hasSuccess = true
			break
		}
	}
	
	if !hasSuccess {
		t.Error("No successful execution log found after retry")
	}
}

func TestSchedulerPaused(t *testing.T) {
	// Create repository and scheduler
	repo := NewMockRepository()
	ctx := context.Background()
	scheduler := NewScheduler(repo)
	
	// Create project that is paused
	project := models.NewProject("Paused Project", "Test paused functionality")
	project.Paused = true
	repo.CreateProject(ctx, project)
	
	// Create schedule
	headers, _ := json.Marshal(map[string]string{"Content-Type": "application/json"})
	body, _ := json.Marshal(map[string]string{"test": "value"})
	schedule := models.NewSchedule(project.ID, "Test Schedule", "* * * * *", "http://example.com", headers, body, 30)
	repo.CreateSchedule(ctx, schedule)
	
	// Try to schedule the task - should not be scheduled due to paused project
	err := scheduler.scheduleTask(ctx, schedule)
	if err != nil {
		t.Errorf("Unexpected error scheduling task: %v", err)
	}
	
	// Should not be in the scheduler's entryIDs map
	scheduler.mu.Lock()
	_, exists := scheduler.entryIDs[schedule.ID]
	scheduler.mu.Unlock()
	
	if exists {
		t.Error("Schedule was added to scheduler despite project being paused")
	}
}

func TestSchedulerRefreshSchedule(t *testing.T) {
	// Create repository and scheduler
	repo := NewMockRepository()
	ctx := context.Background()
	scheduler := NewScheduler(repo)
	
	// Create project
	project := models.NewProject("Refresh Project", "Test refresh functionality")
	repo.CreateProject(ctx, project)
	
	// Create schedule
	headers, _ := json.Marshal(map[string]string{"Content-Type": "application/json"})
	body, _ := json.Marshal(map[string]string{"test": "value"})
	schedule := models.NewSchedule(project.ID, "Test Schedule", "* * * * *", "http://example.com", headers, body, 30)
	repo.CreateSchedule(ctx, schedule)
	
	// Start the scheduler
	scheduler.Start(ctx)
	defer scheduler.Stop()
	
	// Schedule the task
	err := scheduler.scheduleTask(ctx, schedule)
	if err != nil {
		t.Errorf("Failed to schedule task: %v", err)
		return
	}
	
	// Check that it was scheduled
	scheduler.mu.Lock()
	entryID1, exists := scheduler.entryIDs[schedule.ID]
	scheduler.mu.Unlock()
	
	if !exists {
		t.Error("Schedule was not added to entryIDs map")
		return
	}
	
	// Update the schedule and refresh
	schedule.CronExpression = "*/30 * * * *" // Every 30 minutes
	repo.UpdateSchedule(ctx, schedule)
	
	// Refresh the schedule
	err = scheduler.RefreshSchedule(ctx, schedule.ID)
	if err != nil {
		t.Errorf("Failed to refresh schedule: %v", err)
		return
	}
	
	// Check that the entry ID changed
	scheduler.mu.Lock()
	entryID2, exists := scheduler.entryIDs[schedule.ID]
	scheduler.mu.Unlock()
	
	if !exists {
		t.Error("Schedule was not in entryIDs map after refresh")
		return
	}
	
	if entryID1 == entryID2 {
		t.Error("Entry ID did not change after refresh, suggesting the schedule was not updated")
	}
} 