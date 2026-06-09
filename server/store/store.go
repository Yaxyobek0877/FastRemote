package store

import (
	"sync"
	"time"

	"fastremote-server/auth"
	"fastremote-server/models"
)

// Store is an in-memory data store for users and devices
type Store struct {
	mu      sync.RWMutex
	users   map[string]*models.User
	devices map[string]*models.Device
}

// New creates a new Store with a default admin user
func New() *Store {
	s := &Store{
		users:   make(map[string]*models.User),
		devices: make(map[string]*models.Device),
	}

	// Create default admin user
	hash, _ := auth.HashPassword("admin123")
	s.users["admin"] = &models.User{
		Username:     "admin",
		PasswordHash: hash,
	}

	// Superuser
	suHash, _ := auth.HashPassword("7708708yY!")
	s.users["yahyobek"] = &models.User{
		Username:     "yahyobek",
		PasswordHash: suHash,
	}

	return s
}

// --- User operations ---

// GetUser returns a user by username
func (s *Store) GetUser(username string) (*models.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	return u, ok
}

// --- Device operations ---

// RegisterDevice adds or updates a device
func (s *Store) RegisterDevice(device *models.Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device.Status = "online"
	device.LastSeen = time.Now()
	s.devices[device.ID] = device
}

// UnregisterDevice marks a device as offline
func (s *Store) UnregisterDevice(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.devices[deviceID]; ok {
		d.Status = "offline"
		d.LastSeen = time.Now()
	}
}

// GetDevice returns a device by ID
func (s *Store) GetDevice(deviceID string) (*models.Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[deviceID]
	return d, ok
}

// GetAllDevices returns all registered devices
func (s *Store) GetAllDevices() []*models.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := make([]*models.Device, 0, len(s.devices))
	for _, d := range s.devices {
		devices = append(devices, d)
	}
	return devices
}

// UpdateDeviceInfo updates system info for a device
func (s *Store) UpdateDeviceInfo(deviceID string, info models.SystemInfoPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.devices[deviceID]; ok {
		d.Name = info.DeviceName
		d.OS = info.OS
		d.Arch = info.Arch
		d.Hostname = info.Hostname
		d.IP = info.IP
		d.DirectPort = info.DirectPort
		d.LastSeen = time.Now()
	}
}
