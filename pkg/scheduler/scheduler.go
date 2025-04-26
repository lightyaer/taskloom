package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/lightyaer/taskloom/pkg/database"
	"github.com/lightyaer/taskloom/pkg/models"
	"github.com/robfig/cron/v3"
)

const (
	// Database retry configuration
	maxRetries   = 5
	retryBackoff = 20 * time.Millisecond
)

// Scheduler manages and executes scheduled tasks
type Scheduler struct {
	repo        database.Repository
	cron        *cron.Cron
	entryIDs    map[string]cron.EntryID
	rateLimiter map[string]*RateLimiter
	mu          sync.Mutex
}

// RateLimiter is a simple rate limiter for tasks based on project rate limits
type RateLimiter struct {
	tokens         int
	maxTokens      int
	refillRate     int // tokens per minute
	lastRefillTime time.Time
	mu             sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxTokens, refillRate int) *RateLimiter {
	return &RateLimiter{
		tokens:         maxTokens,
		maxTokens:      maxTokens,
		refillRate:     refillRate,
		lastRefillTime: time.Now(),
		mu:             sync.Mutex{},
	}
}

// TakeToken attempts to take a token from the rate limiter
func (rl *RateLimiter) TakeToken() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(rl.lastRefillTime)
	minutesElapsed := int(elapsed.Minutes())

	if minutesElapsed > 0 {
		tokensToAdd := minutesElapsed * rl.refillRate
		rl.tokens = min(rl.maxTokens, rl.tokens+tokensToAdd)
		rl.lastRefillTime = now
	}

	// Check if a token is available
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// withRetry executes a database operation with retry logic for handling database locks
func withRetry(operation func() error) error {
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		// Check if the error is a database lock error
		if err.Error() != "database is locked" && !contains(err.Error(), "SQLITE_BUSY") {
			return err // Not a locking error, return immediately
		}

		// Exponential backoff for retries
		backoff := retryBackoff * time.Duration(attempt)
		log.Printf("Database locked, retrying in %v (attempt %d/%d)", backoff, attempt, maxRetries)
		time.Sleep(backoff)
	}
	return fmt.Errorf("max retries exceeded: %w", err)
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	// Check if s is long enough to contain substr
	if len(s) < len(substr) {
		return false
	}

	// Simple string search
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// NewScheduler creates a new scheduler
func NewScheduler(repo database.Repository) *Scheduler {
	return &Scheduler{
		repo:        repo,
		cron:        cron.New(cron.WithSeconds()),
		entryIDs:    make(map[string]cron.EntryID),
		rateLimiter: make(map[string]*RateLimiter),
		mu:          sync.Mutex{},
	}
}

// Start starts the scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	log.Printf("Starting scheduler...")

	// Load all projects to set up rate limiters
	var projects []*models.Project
	fetchProjectsErr := withRetry(func() error {
		var err error
		projects, err = s.repo.ListProjects(ctx)
		return err
	})

	if fetchProjectsErr != nil {
		log.Printf("Failed to list projects: %v", fetchProjectsErr)
		return fmt.Errorf("failed to list projects: %w", fetchProjectsErr)
	}

	log.Printf("Loaded %d projects", len(projects))

	// Initialize rate limiters for each project
	for _, project := range projects {
		s.rateLimiter[project.ID] = NewRateLimiter(project.TaskRate, project.TaskRate)
		log.Printf("Initialized rate limiter for project %s with rate %d", project.ID, project.TaskRate)
	}

	// Load all enabled schedules
	var schedules []*models.Schedule
	fetchSchedulesErr := withRetry(func() error {
		var err error
		schedules, err = s.repo.GetEnabledSchedules(ctx)
		return err
	})

	if fetchSchedulesErr != nil {
		log.Printf("Failed to get enabled schedules: %v", fetchSchedulesErr)
		return fmt.Errorf("failed to get enabled schedules: %w", fetchSchedulesErr)
	}

	log.Printf("Loaded %d enabled schedules", len(schedules))

	// Schedule each task
	for _, schedule := range schedules {
		if err := s.scheduleTask(ctx, schedule); err != nil {
			log.Printf("Failed to schedule task for schedule %s: %v", schedule.ID, err)
		}
	}

	// Start the cron scheduler
	s.cron.Start()
	log.Printf("Scheduler started successfully")

	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	log.Printf("Stopping scheduler...")
	s.cron.Stop()
	log.Printf("Scheduler stopped")
}

// scheduleTask schedules a task for execution
func (s *Scheduler) scheduleTask(ctx context.Context, schedule *models.Schedule) error {
	log.Printf("Scheduling task for schedule ID: %s with cron expression: %s", schedule.ID, schedule.CronExpression)

	// Get the project for rate limiting
	var project *models.Project
	fetchErr := withRetry(func() error {
		var err error
		project, err = s.repo.GetProject(ctx, schedule.ProjectID)
		return err
	})

	if fetchErr != nil {
		log.Printf("Failed to get project %s for schedule %s: %v", schedule.ProjectID, schedule.ID, fetchErr)
		return fmt.Errorf("failed to get project %s: %w", schedule.ProjectID, fetchErr)
	}

	// Check if the project is paused
	if project.Paused {
		log.Printf("Project %s is paused, not scheduling task for schedule %s", project.ID, schedule.ID)
		return nil
	}

	// Create a function to execute the task for this schedule
	executeFunc := func() {
		if ctx.Err() != nil {
			return
		}

		log.Printf("Cron triggered execution for schedule ID: %s", schedule.ID)

		// Check if the project is paused (real-time check)
		var project *models.Project
		fetchErr := withRetry(func() error {
			var err error
			project, err = s.repo.GetProject(ctx, schedule.ProjectID)
			return err
		})

		if fetchErr != nil {
			log.Printf("Failed to get project %s: %v", schedule.ProjectID, fetchErr)
			return
		}

		if project.Paused {
			log.Printf("Project %s is paused, skipping execution for schedule %s", project.ID, schedule.ID)
			return
		}

		// Check if rate limit allows execution
		limiter, ok := s.rateLimiter[project.ID]
		if !ok {
			limiter = NewRateLimiter(project.TaskRate, project.TaskRate)
			s.mu.Lock()
			s.rateLimiter[project.ID] = limiter
			s.mu.Unlock()
		}

		if !limiter.TakeToken() {
			log.Printf("Rate limit exceeded for project %s, schedule %s", project.ID, schedule.ID)
			return
		}

		// Execute the task for this schedule
		go s.executeTask(ctx, schedule, project)
	}

	// Add the job to the cron scheduler
	entryID, cronErr := s.cron.AddFunc(schedule.CronExpression, executeFunc)
	if cronErr != nil {
		log.Printf("Failed to add cron job for schedule %s: %v", schedule.ID, cronErr)
		return fmt.Errorf("failed to add cron job for schedule %s: %w", schedule.ID, cronErr)
	}

	// Store the entry ID for later removal/update
	s.mu.Lock()
	s.entryIDs[schedule.ID] = entryID
	s.mu.Unlock()

	log.Printf("Successfully scheduled task for schedule ID: %s", schedule.ID)
	return nil
}

// executeTask executes a single task
func (s *Scheduler) executeTask(ctx context.Context, schedule *models.Schedule, project *models.Project) {
	log.Printf("Starting task execution for schedule ID: %s, URL: %s", schedule.ID, schedule.URL)

	// Create execution log
	execLog := models.NewExecutionLog(schedule.ID)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(schedule.Timeout)*time.Second)
	defer cancel()

	// Store the initial execution log
	dbErr := withRetry(func() error {
		return s.repo.CreateExecutionLog(ctx, execLog)
	})

	if dbErr != nil {
		log.Printf("Failed to create execution log for schedule %s: %v", schedule.ID, dbErr)
		execLog.Error = fmt.Sprintf("Failed to create execution log: %v", dbErr)
		return
	}

	// Prepare the request
	var reqHeaders map[string]string
	var reqBody interface{}

	// Parse headers
	if len(schedule.Headers) > 0 {
		// Log the raw headers for debugging
		log.Printf("Raw headers for schedule %s: %s", schedule.ID, string(schedule.Headers))

		// Try to unmarshal as a map first
		err := json.Unmarshal(schedule.Headers, &reqHeaders)
		if err != nil {
			// If that fails, try to unmarshal as a string (which contains JSON)
			var headerStr string
			if jsonErr := json.Unmarshal(schedule.Headers, &headerStr); jsonErr == nil {
				// Now try to parse the string as JSON
				if headerStr != "" {
					if jsonErr := json.Unmarshal([]byte(headerStr), &reqHeaders); jsonErr != nil {
						log.Printf("Failed to parse headers string for schedule %s: %v", schedule.ID, jsonErr)
						execLog.Complete("failed", 0, "", fmt.Sprintf("Failed to parse headers string: %v", jsonErr))

						updateErr := withRetry(func() error {
							return s.repo.UpdateExecutionLog(ctx, execLog)
						})

						if updateErr != nil {
							fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
						}
						return
					}
				}
			} else {
				log.Printf("Failed to parse headers for schedule %s: %v", schedule.ID, err)
				execLog.Complete("failed", 0, "", fmt.Sprintf("Failed to parse headers: %v", err))

				updateErr := withRetry(func() error {
					return s.repo.UpdateExecutionLog(ctx, execLog)
				})

				if updateErr != nil {
					fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
				}
				return
			}
		}

		log.Printf("Parsed headers for schedule %s: %+v", schedule.ID, reqHeaders)
	}

	// Parse body
	if len(schedule.Body) > 0 {
		// Log the raw body for debugging
		log.Printf("Raw body for schedule %s: %s", schedule.ID, string(schedule.Body))

		// Try to unmarshal directly first
		err := json.Unmarshal(schedule.Body, &reqBody)
		if err != nil {
			// If that fails, try to unmarshal as a string (which contains JSON)
			var bodyStr string
			if jsonErr := json.Unmarshal(schedule.Body, &bodyStr); jsonErr == nil {
				// Now try to parse the string as JSON
				if bodyStr != "" {
					if jsonErr := json.Unmarshal([]byte(bodyStr), &reqBody); jsonErr != nil {
						log.Printf("Failed to parse body string for schedule %s: %v", schedule.ID, jsonErr)
						execLog.Complete("failed", 0, "", fmt.Sprintf("Failed to parse body string: %v", jsonErr))

						updateErr := withRetry(func() error {
							return s.repo.UpdateExecutionLog(ctx, execLog)
						})

						if updateErr != nil {
							fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
						}
						return
					}
				}
			} else {
				log.Printf("Failed to parse body for schedule %s: %v", schedule.ID, err)
				execLog.Complete("failed", 0, "", fmt.Sprintf("Failed to parse body: %v", err))

				updateErr := withRetry(func() error {
					return s.repo.UpdateExecutionLog(ctx, execLog)
				})

				if updateErr != nil {
					fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
				}
				return
			}
		}

		log.Printf("Parsed body for schedule %s: %+v", schedule.ID, reqBody)
	}

	// Marshal the body to JSON
	var reqBodyBytes []byte
	var marshallErr error
	if reqBody != nil {
		reqBodyBytes, marshallErr = json.Marshal(reqBody)
		if marshallErr != nil {
			log.Printf("Failed to marshal body for schedule %s: %v", schedule.ID, marshallErr)
			execLog.Complete("failed", 0, "", fmt.Sprintf("Failed to marshal body: %v", marshallErr))

			updateErr := withRetry(func() error {
				return s.repo.UpdateExecutionLog(ctx, execLog)
			})

			if updateErr != nil {
				fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
			}
			return
		}
	}

	// Create the HTTP request
	req, httpErr := http.NewRequestWithContext(ctx, "POST", schedule.URL, bytes.NewBuffer(reqBodyBytes))
	if httpErr != nil {
		log.Printf("Failed to create request for schedule %s: %v", schedule.ID, httpErr)
		execLog.Complete("failed", 0, "", fmt.Sprintf("Failed to create request: %v", httpErr))

		updateErr := withRetry(func() error {
			return s.repo.UpdateExecutionLog(ctx, execLog)
		})

		if updateErr != nil {
			fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
		}
		return
	}

	// Add headers
	for key, value := range reqHeaders {
		req.Header.Set(key, value)
	}

	// Add content type if not specified
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add API key to headers
	req.Header.Set("X-API-Key", project.APIKey)

	log.Printf("Sending HTTP request for schedule %s to URL: %s", schedule.ID, schedule.URL)

	// Execute the request
	client := &http.Client{}
	resp, reqErr := client.Do(req)
	if reqErr != nil {
		log.Printf("HTTP request failed for schedule %s: %v", schedule.ID, reqErr)
		execLog.Complete("failed", 0, "", fmt.Sprintf("Request failed: %v", reqErr))

		updateErr := withRetry(func() error {
			return s.repo.UpdateExecutionLog(ctx, execLog)
		})

		if updateErr != nil {
			fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
		}

		// Handle retry if enabled
		if project.RetryFailed {
			log.Printf("Scheduling retry for failed task with schedule ID: %s", schedule.ID)
			// Wait a bit before retrying
			time.Sleep(5 * time.Second)
			s.retryTask(ctx, schedule, project, execLog)
		}
		return
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Printf("Failed to read response body for schedule %s: %v", schedule.ID, readErr)
		execLog.Complete("failed", resp.StatusCode, "", fmt.Sprintf("Failed to read response body: %v", readErr))

		updateErr := withRetry(func() error {
			return s.repo.UpdateExecutionLog(ctx, execLog)
		})

		if updateErr != nil {
			fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
		}
		return
	}

	// Check if the response was successful
	status := "success"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "failed"
		log.Printf("Task execution failed with status code %d for schedule %s", resp.StatusCode, schedule.ID)

		// Handle retry if enabled and the request failed
		if project.RetryFailed {
			log.Printf("Scheduling retry for failed task with schedule ID: %s", schedule.ID)
			// Wait a bit before retrying
			time.Sleep(5 * time.Second)
			s.retryTask(ctx, schedule, project, execLog)
		}
	} else {
		log.Printf("Task execution succeeded with status code %d for schedule %s", resp.StatusCode, schedule.ID)
	}

	// Update the execution log
	execLog.Complete(status, resp.StatusCode, string(respBody), "")

	updateErr := withRetry(func() error {
		return s.repo.UpdateExecutionLog(ctx, execLog)
	})

	if updateErr != nil {
		fmt.Printf("Failed to update execution log %s: %v\n", execLog.ID, updateErr)
	}

	log.Printf("Completed task execution for schedule ID: %s with status: %s", schedule.ID, status)
}

// retryTask retries a failed task once
func (s *Scheduler) retryTask(ctx context.Context, schedule *models.Schedule, project *models.Project, prevLog *models.ExecutionLog) {
	log.Printf("Retrying failed task for schedule ID: %s", schedule.ID)

	// Create a new execution log for the retry
	execLog := models.NewExecutionLog(schedule.ID)
	execLog.Error = "Retry of failed task"

	dbErr := withRetry(func() error {
		return s.repo.CreateExecutionLog(ctx, execLog)
	})

	if dbErr != nil {
		log.Printf("Failed to create execution log for retry of schedule %s: %v", schedule.ID, dbErr)
		fmt.Printf("Failed to create execution log for retry: %v\n", dbErr)
		return
	}

	log.Printf("Starting retry execution for schedule ID: %s", schedule.ID)
	// Just call executeTask again
	s.executeTask(ctx, schedule, project)
}

// RefreshSchedule updates or adds a schedule
func (s *Scheduler) RefreshSchedule(ctx context.Context, scheduleID string) error {
	log.Printf("Refreshing schedule ID: %s", scheduleID)

	// Get the schedule
	var schedule *models.Schedule
	fetchErr := withRetry(func() error {
		var err error
		schedule, err = s.repo.GetSchedule(ctx, scheduleID)
		return err
	})

	if fetchErr != nil {
		log.Printf("Failed to get schedule %s: %v", scheduleID, fetchErr)
		return fmt.Errorf("failed to get schedule %s: %w", scheduleID, fetchErr)
	}

	// Remove existing schedule if it exists
	s.mu.Lock()
	if entryID, ok := s.entryIDs[scheduleID]; ok {
		log.Printf("Removing existing schedule ID: %s", scheduleID)
		s.cron.Remove(entryID)
		delete(s.entryIDs, scheduleID)
	}
	s.mu.Unlock()

	// Don't schedule if disabled
	if !schedule.Enabled {
		log.Printf("Schedule %s is disabled, not scheduling", scheduleID)
		return nil
	}

	// Schedule the task
	log.Printf("Re-scheduling task for schedule ID: %s", scheduleID)
	return s.scheduleTask(ctx, schedule)
}

// RemoveSchedule removes a schedule
func (s *Scheduler) RemoveSchedule(scheduleID string) {
	log.Printf("Removing schedule ID: %s", scheduleID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entryIDs[scheduleID]; ok {
		s.cron.Remove(entryID)
		delete(s.entryIDs, scheduleID)
		log.Printf("Schedule ID: %s removed successfully", scheduleID)
	} else {
		log.Printf("Schedule ID: %s was not found in active schedules", scheduleID)
	}
}

// UpdateProjectRateLimit updates the rate limit for a project
func (s *Scheduler) UpdateProjectRateLimit(projectID string, taskRate int) {
	log.Printf("Updating rate limit for project %s to %d tasks per minute", projectID, taskRate)

	s.mu.Lock()
	defer s.mu.Unlock()

	limiter, ok := s.rateLimiter[projectID]
	if !ok {
		log.Printf("Creating new rate limiter for project %s", projectID)
		s.rateLimiter[projectID] = NewRateLimiter(taskRate, taskRate)
	} else {
		log.Printf("Updating existing rate limiter for project %s", projectID)
		limiter.maxTokens = taskRate
		limiter.refillRate = taskRate
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
