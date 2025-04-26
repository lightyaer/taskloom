package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lightyaer/taskloom/pkg/database"
	"github.com/lightyaer/taskloom/pkg/models"
	"github.com/lightyaer/taskloom/pkg/scheduler"
)

// Handler contains all HTTP handlers for the API
type Handler struct {
	repo      database.Repository
	scheduler *scheduler.Scheduler
}

// NewHandler creates a new handler
func NewHandler(repo database.Repository, scheduler *scheduler.Scheduler) *Handler {
	return &Handler{
		repo:      repo,
		scheduler: scheduler,
	}
}

// SetupRoutes sets up the routes for the handler
func (h *Handler) SetupRoutes(r chi.Router) {
	// Projects
	r.Route("/api/projects", func(r chi.Router) {
		r.Get("/", h.ListProjects)
		r.Post("/", h.CreateProject)
		r.Get("/{id}", h.GetProject)
		r.Put("/{id}", h.UpdateProject)
		r.Delete("/{id}", h.DeleteProject)
	})

	// Schedules
	r.Route("/api/schedules", func(r chi.Router) {
		r.Get("/", h.ListSchedules)
		r.Post("/", h.CreateSchedule)
		r.Get("/{id}", h.GetSchedule)
		r.Put("/{id}", h.UpdateSchedule)
		r.Delete("/{id}", h.DeleteSchedule)
	})

	// Execution logs
	r.Route("/api/logs", func(r chi.Router) {
		r.Get("/schedule/{scheduleId}", h.ListLogsBySchedule)
	})

	// Health check
	r.Get("/health", HealthCheckHandler)
}

// Response helpers

// respondJSON responds with JSON
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
	}
}

// respondError responds with an error
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// Project handlers

// ListProjects lists all projects
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projects, err := h.repo.ListProjects(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, projects)
}

// CreateProject creates a new project
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	project := models.NewProject(req.Name, req.Description)
	ctx := r.Context()
	if err := h.repo.CreateProject(ctx, project); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Initialize rate limiter for the new project
	h.scheduler.UpdateProjectRateLimit(project.ID, project.TaskRate)

	respondJSON(w, http.StatusCreated, project)
}

// GetProject gets a project by ID
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	project, err := h.repo.GetProject(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, project)
}

// UpdateProject updates a project
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	project, err := h.repo.GetProject(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		APIKey      string `json:"api_key"`
		Paused      *bool  `json:"paused"`
		RetryFailed *bool  `json:"retry_failed"`
		TaskRate    *int   `json:"task_rate"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != "" {
		project.Name = req.Name
	}
	if req.Description != "" {
		project.Description = req.Description
	}
	if req.APIKey != "" {
		project.APIKey = req.APIKey
	}
	if req.Paused != nil {
		project.Paused = *req.Paused
	}
	if req.RetryFailed != nil {
		project.RetryFailed = *req.RetryFailed
	}
	if req.TaskRate != nil {
		project.TaskRate = *req.TaskRate
		// Update rate limiter
		h.scheduler.UpdateProjectRateLimit(project.ID, project.TaskRate)
	}

	if err := h.repo.UpdateProject(ctx, project); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, project)
}

// DeleteProject deletes a project
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	// First, get all schedules for this project
	schedules, err := h.repo.ListSchedulesByProject(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Remove schedules from the scheduler
	for _, schedule := range schedules {
		h.scheduler.RemoveSchedule(schedule.ID)
	}

	// Delete the project
	if err := h.repo.DeleteProject(ctx, id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Project deleted successfully"})
}

// Schedule handlers

// ListSchedules lists all schedules for a project
func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		respondError(w, http.StatusBadRequest, "project_id query parameter is required")
		return
	}

	ctx := r.Context()
	schedules, err := h.repo.ListSchedulesByProject(ctx, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, schedules)
}

// CreateSchedule creates a new schedule
func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID      string          `json:"project_id"`
		Name           string          `json:"name"`
		CronExpression string          `json:"cron_expression"`
		URL            string          `json:"url"`
		Headers        json.RawMessage `json:"headers"`
		Body           json.RawMessage `json:"body"`
		Timeout        int             `json:"timeout"`
		Enabled        *bool           `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ProjectID == "" || req.Name == "" || req.CronExpression == "" || req.URL == "" {
		respondError(w, http.StatusBadRequest, "project_id, name, cron_expression, and url are required")
		return
	}

	timeout := 30 // Default timeout
	if req.Timeout > 0 {
		timeout = req.Timeout
	}

	schedule := models.NewSchedule(req.ProjectID, req.Name, req.CronExpression, req.URL, req.Headers, req.Body, timeout)
	if req.Enabled != nil {
		schedule.Enabled = *req.Enabled
	}

	ctx := r.Context()
	if err := h.repo.CreateSchedule(ctx, schedule); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Add schedule to the scheduler if enabled
	if schedule.Enabled {
		if err := h.scheduler.RefreshSchedule(ctx, schedule.ID); err != nil {
			log.Printf("Error scheduling task: %v", err)
		}
	}

	respondJSON(w, http.StatusCreated, schedule)
}

// GetSchedule gets a schedule by ID
func (h *Handler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	schedule, err := h.repo.GetSchedule(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, schedule)
}

// UpdateSchedule updates a schedule
func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	schedule, err := h.repo.GetSchedule(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	var req struct {
		Name           string          `json:"name"`
		CronExpression string          `json:"cron_expression"`
		URL            string          `json:"url"`
		Headers        json.RawMessage `json:"headers"`
		Body           json.RawMessage `json:"body"`
		Timeout        int             `json:"timeout"`
		Enabled        *bool           `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != "" {
		schedule.Name = req.Name
	}
	if req.CronExpression != "" {
		schedule.CronExpression = req.CronExpression
	}
	if req.URL != "" {
		schedule.URL = req.URL
	}
	if len(req.Headers) > 0 {
		schedule.Headers = req.Headers
	}
	if len(req.Body) > 0 {
		schedule.Body = req.Body
	}
	if req.Timeout > 0 {
		schedule.Timeout = req.Timeout
	}
	if req.Enabled != nil {
		schedule.Enabled = *req.Enabled
	}

	if err := h.repo.UpdateSchedule(ctx, schedule); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Refresh the schedule in the scheduler
	if err := h.scheduler.RefreshSchedule(ctx, schedule.ID); err != nil {
		log.Printf("Error refreshing schedule: %v", err)
	}

	respondJSON(w, http.StatusOK, schedule)
}

// DeleteSchedule deletes a schedule
func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	// Remove schedule from the scheduler
	h.scheduler.RemoveSchedule(id)

	// Delete the schedule
	if err := h.repo.DeleteSchedule(ctx, id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Schedule deleted successfully"})
}

// Execution log handlers

// ListLogsBySchedule lists all execution logs for a schedule
func (h *Handler) ListLogsBySchedule(w http.ResponseWriter, r *http.Request) {
	scheduleID := chi.URLParam(r, "scheduleId")
	limit, offset := getPagination(r)

	ctx := r.Context()
	logs, err := h.repo.ListExecutionLogsBySchedule(ctx, scheduleID, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, logs)
}

// Helper functions

// getPagination gets the limit and offset from query parameters
func getPagination(r *http.Request) (int, int) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50 // Default limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0 // Default offset
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	return limit, offset
}

// HealthCheckHandler handles health check requests
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": "1.0.0",
		"time":    time.Now(),
	})
} 