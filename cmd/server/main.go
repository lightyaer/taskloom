package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/jwtauth"
	"github.com/joho/godotenv"
	"github.com/lightyaer/taskloom/pkg/auth"
	"github.com/lightyaer/taskloom/pkg/database"
	"github.com/lightyaer/taskloom/pkg/handlers"
	"github.com/lightyaer/taskloom/pkg/scheduler"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Parse command line flags
	var (
		port    = flag.String("port", getEnv("PORT", "8080"), "Port to listen on")
		dbURL   = flag.String("db-url", getEnv("TURSO_URL", "file:taskloom.db"), "Turso database URL")
		dbToken = flag.String("db-token", getEnv("TURSO_TOKEN", ""), "Turso database auth token")
		jwtKey  = flag.String("jwt-key", getEnv("JWT_SECRET", "your-secret-key-for-jwt"), "JWT secret key")
	)
	flag.Parse()

	// Initialize the repository
	repo, err := database.NewTursoRepository(*dbURL, *dbToken)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer repo.Close()

	// Initialize the database schema
	ctx := context.Background()
	if err := repo.InitDB(ctx); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize the scheduler
	sched := scheduler.NewScheduler(repo)
	if err := sched.Start(ctx); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}
	defer sched.Stop()

	// Initialize auth service
	authService := auth.NewAuthService(repo, *jwtKey)
	tokenAuth := authService.GetTokenAuth()

	// Create a new router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-API-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public routes
	r.Group(func(r chi.Router) {
		r.Get("/health", handlers.HealthCheckHandler)
		r.Get("/api/docs", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "api-docs.html")
		})

		// Set up auth routes
		authHandler := handlers.NewAuthHandler(authService)
		authHandler.SetupAuthRoutes(r)
	})

	// Protected routes
	r.Group(func(r chi.Router) {
		// Use JWT auth for all routes in this group
		r.Use(jwtauth.Verifier(tokenAuth))
		r.Use(jwtauth.Authenticator)

		// Set up API routes
		h := handlers.NewHandler(repo, sched)
		h.SetupRoutes(r)
	})

	// Create a new HTTP server
	srv := &http.Server{
		Addr:         ":" + *port,
		Handler:      r,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start the server in a goroutine
	go func() {
		log.Printf("Server listening on port %s", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Create a deadline to wait for
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shut down the server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server gracefully stopped")
}

// getEnv gets an environment variable or a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
} 