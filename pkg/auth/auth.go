package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/jwtauth"
	"github.com/lightyaer/taskloom/pkg/database"
	"github.com/lightyaer/taskloom/pkg/models"
)

// AuthService handles authentication
type AuthService struct {
	repo      database.Repository
	tokenAuth *jwtauth.JWTAuth
}

// NewAuthService creates a new auth service
func NewAuthService(repo database.Repository, jwtSecret string) *AuthService {
	return &AuthService{
		repo:      repo,
		tokenAuth: jwtauth.New("HS256", []byte(jwtSecret), nil),
	}
}

// GetTokenAuth returns the JWT token auth
func (s *AuthService) GetTokenAuth() *jwtauth.JWTAuth {
	return s.tokenAuth
}

// LoginWithEmail generates a magic login token and sends an email
func (s *AuthService) LoginWithEmail(ctx context.Context, email string) error {
	// In a real implementation, generate a one-time token and send it via email
	// For now, we'll just ensure the user exists
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		// If user doesn't exist, create a new one
		user = models.NewUser("New User", email, "email", "")
		if err := s.repo.CreateUser(ctx, user); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
	}

	// In a real implementation, send an email with a login link
	// For simplicity, we just return success
	return nil
}

// VerifyEmailToken verifies a magic login token and returns a JWT token
func (s *AuthService) VerifyEmailToken(ctx context.Context, token string) (string, error) {
	// In a real implementation, verify the one-time token
	// For now, we'll just use the token as an email and find the user

	user, err := s.repo.GetUserByEmail(ctx, token)
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	// Generate JWT token
	jwtToken, err := s.generateJWT(user)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	return jwtToken, nil
}

// LoginWithOAuth handles OAuth login
func (s *AuthService) LoginWithOAuth(ctx context.Context, provider, providerID, email, name string) (string, error) {
	// Try to find user by OAuth provider and ID
	user, err := s.repo.GetUserByOAuth(ctx, provider, providerID)
	if err != nil {
		// Try to find user by email
		user, err = s.repo.GetUserByEmail(ctx, email)
		if err != nil {
			// Create new user
			user = models.NewUser(name, email, provider, providerID)
			if err := s.repo.CreateUser(ctx, user); err != nil {
				return "", fmt.Errorf("failed to create user: %w", err)
			}
		} else {
			// Update existing user with OAuth details
			user.OAuthProvider = provider
			user.OAuthID = providerID
			if err := s.repo.UpdateUser(ctx, user); err != nil {
				return "", fmt.Errorf("failed to update user: %w", err)
			}
		}
	}

	// Generate JWT token
	jwtToken, err := s.generateJWT(user)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	return jwtToken, nil
}

// generateJWT generates a JWT token for a user
func (s *AuthService) generateJWT(user *models.User) (string, error) {
	// Set token expiration to 24 hours
	exp := time.Now().Add(24 * time.Hour)

	// Create claims
	claims := map[string]interface{}{
		"user_id": user.ID,
		"email":   user.Email,
		"name":    user.Name,
		"exp":     exp.Unix(),
	}

	// Generate JWT token
	_, tokenString, err := s.tokenAuth.Encode(claims)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// GetUserFromRequest gets the user from the request context
func (s *AuthService) GetUserFromRequest(ctx context.Context, r *http.Request) (*models.User, error) {
	// Get claims from context
	_, claims, err := jwtauth.FromContext(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get claims from context: %w", err)
	}

	// Get user ID from claims
	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("user_id not found in claims")
	}

	// Get user from database
	user, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
} 