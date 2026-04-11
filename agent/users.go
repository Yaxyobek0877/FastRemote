package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// User represents a system user
type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Role         string `json:"role"` // "admin" or "user"
}

// UserStore manages users with file persistence
type UserStore struct {
	mu    sync.RWMutex
	users map[string]*User
	path  string
}

func NewUserStore(path string) *UserStore {
	store := &UserStore{
		users: make(map[string]*User),
		path:  path,
	}
	store.load()
	return store
}

func (s *UserStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		// File doesn't exist — will be created on first save
		return
	}
	var users []*User
	if err := json.Unmarshal(data, &users); err != nil {
		log.Printf("[Users] Failed to parse %s: %v", s.path, err)
		return
	}
	for _, u := range users {
		s.users[u.Username] = u
	}
	log.Printf("[Users] Loaded %d users from %s", len(s.users), s.path)
}

// saveUnsafe writes users to disk. Caller MUST hold mu lock.
func (s *UserStore) saveUnsafe() error {
	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}

	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// EnsureDefaultAdmin creates admin user if no users exist
func (s *UserStore) EnsureDefaultAdmin(password string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.users) > 0 {
		return // Users already exist
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("[Users] Failed to hash default password: %v", err)
	}

	s.users["admin"] = &User{
		Username:     "admin",
		PasswordHash: string(hash),
		Role:         "admin",
	}
	s.saveUnsafe()
	log.Println("[Users] Created default admin user")
}

// Authenticate checks credentials
func (s *UserStore) Authenticate(username, password string) (*User, bool) {
	s.mu.RLock()
	user, exists := s.users[username]
	s.mu.RUnlock()

	if !exists {
		return nil, false
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	return user, err == nil
}

// AddUser creates a new user
func (s *UserStore) AddUser(username, password, role string) error {
	if username == "" || len(username) < 2 {
		return fmt.Errorf("Username must be at least 2 characters")
	}
	if err := validatePasswordStrength(password); err != nil {
		return err
	}
	if role != "admin" && role != "user" {
		role = "user"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; exists {
		return fmt.Errorf("User '%s' already exists", username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("Failed to hash password")
	}

	s.users[username] = &User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
	}
	return s.saveUnsafe()
}

// DeleteUser removes a user
func (s *UserStore) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; !exists {
		return fmt.Errorf("User '%s' not found", username)
	}

	// Prevent deleting the last admin
	if s.users[username].Role == "admin" {
		adminCount := 0
		for _, u := range s.users {
			if u.Role == "admin" {
				adminCount++
			}
		}
		if adminCount <= 1 {
			return fmt.Errorf("Cannot delete the last admin user")
		}
	}

	delete(s.users, username)
	return s.saveUnsafe()
}

// ChangePassword changes a user's password
func (s *UserStore) ChangePassword(username, currentPassword, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[username]
	if !exists {
		return fmt.Errorf("User not found")
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return fmt.Errorf("Current password is incorrect")
	}

	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("Failed to hash new password")
	}

	user.PasswordHash = string(hash)
	return s.saveUnsafe()
}

// AdminResetPassword lets admin reset any user's password
func (s *UserStore) AdminResetPassword(username, newPassword string) error {
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[username]
	if !exists {
		return fmt.Errorf("User '%s' not found", username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("Failed to hash password")
	}

	user.PasswordHash = string(hash)
	return s.saveUnsafe()
}

// ListUsers returns all users (without password hashes)
func (s *UserStore) ListUsers() []map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]map[string]string, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, map[string]string{
			"username": u.Username,
			"role":     u.Role,
		})
	}
	return list
}

// validatePasswordStrength checks: min 8 chars, at least 1 letter, 1 digit, 1 symbol
func validatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("Password must be at least 8 characters")
	}
	var hasLetter, hasDigit, hasSymbol bool
	for _, ch := range password {
		switch {
		case unicode.IsLetter(ch):
			hasLetter = true
		case unicode.IsDigit(ch):
			hasDigit = true
		case unicode.IsPunct(ch) || unicode.IsSymbol(ch) || ch == ' ':
			hasSymbol = true
		}
	}
	if !hasLetter {
		return fmt.Errorf("Password must contain at least one letter")
	}
	if !hasDigit {
		return fmt.Errorf("Password must contain at least one number")
	}
	if !hasSymbol {
		return fmt.Errorf("Password must contain at least one symbol (!@#$...)")
	}
	return nil
}
