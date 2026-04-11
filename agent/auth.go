package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecret  []byte
	deviceName string // mutable device name
)

const jwtSecretFile = ".jwt_secret"

// initJWTSecret loads or creates a persistent JWT secret
func initJWTSecret() {
	// Check env first
	envSecret := os.Getenv("JWT_SECRET")
	if envSecret != "" {
		jwtSecret = []byte(envSecret)
		log.Println("[Auth] Using JWT secret from environment")
		return
	}

	// Try to load from file
	data, err := os.ReadFile(jwtSecretFile)
	if err == nil && len(data) > 0 {
		jwtSecret = []byte(strings.TrimSpace(string(data)))
		log.Println("[Auth] Loaded JWT secret from file")
		return
	}

	// Generate and save new secret
	b := make([]byte, 32)
	rand.Read(b)
	secret := base64.StdEncoding.EncodeToString(b)
	jwtSecret = []byte(secret)

	if err := os.WriteFile(jwtSecretFile, []byte(secret), 0600); err != nil {
		log.Printf("[Auth] Warning: could not persist JWT secret: %v", err)
	} else {
		log.Println("[Auth] Generated and saved new JWT secret")
	}
}

func generateToken(username, role string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,
		"role":     role,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
		"iss":      "fastremote-agent",
	})
	return token.SignedString(jwtSecret)
}

func validateToken(tokenString string) (string, string, bool) {
	if strings.HasPrefix(tokenString, "Bearer ") {
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", "", false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", false
	}

	username, _ := claims["username"].(string)
	role, _ := claims["role"].(string)
	return username, role, true
}
