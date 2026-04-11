const API_BASE = window.location.hostname === 'localhost' 
  ? 'http://localhost:9090' // Default standalone agent port
  : `${window.location.protocol}//${window.location.hostname}${window.location.port ? `:${window.location.port}` : ''}`;

const WS_BASE = window.location.hostname === 'localhost'
  ? 'ws://localhost:9090'
  : `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.hostname}${window.location.port ? `:${window.location.port}` : ''}`;

// Auth helpers
export function getToken() {
  return localStorage.getItem('fastremote_token');
}

export function setToken(token) {
  localStorage.setItem('fastremote_token', token);
}

export function removeToken() {
  localStorage.removeItem('fastremote_token');
}

export function getUsername() {
  return localStorage.getItem('fastremote_username') || '';
}

export function setUsername(username) {
  localStorage.setItem('fastremote_username', username);
}

export function isAuthenticated() {
  return !!getToken();
}

// API calls
export async function login(username, password) {
  const res = await fetch(`${API_BASE}/api/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });

  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || 'Login failed');
  }

  const data = await res.json();
  setToken(data.token);
  setUsername(data.username);
  return data;
}

export function createSessionSocket() {
  const token = getToken();
  const url = `${WS_BASE}/ws?token=${token}`;
  return new WebSocket(url);
}

export function logout() {
  removeToken();
  localStorage.removeItem('fastremote_username');
  window.location.href = '/login';
}

// Formatting helpers
export function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export function formatDate(dateStr) {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  const now = new Date();
  const diff = now - date;

  if (diff < 60000) return 'just now';
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
  return date.toLocaleDateString();
}

// Device info (public — no auth needed)
export async function getDeviceInfo() {
  const res = await fetch(`${API_BASE}/api/device-info`);
  return res.json();
}

// Settings (auth required)
export async function getSettings() {
  const res = await fetch(`${API_BASE}/api/settings`, {
    headers: { 'Authorization': `Bearer ${getToken()}` },
  });
  return res.json();
}

export async function updateSettings(data) {
  const res = await fetch(`${API_BASE}/api/settings`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${getToken()}` },
    body: JSON.stringify(data),
  });
  return res.json();
}

export async function changePassword(currentPassword, newPassword) {
  const res = await fetch(`${API_BASE}/api/change-password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${getToken()}` },
    body: JSON.stringify({ currentPassword, newPassword }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to change password');
  return data;
}

export async function addUser(username, password, role) {
  const res = await fetch(`${API_BASE}/api/users`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${getToken()}` },
    body: JSON.stringify({ username, password, role }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to add user');
  return data;
}

export async function deleteUser(username) {
  const res = await fetch(`${API_BASE}/api/users`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${getToken()}` },
    body: JSON.stringify({ username }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to delete user');
  return data;
}

export async function resetUserPassword(username, newPassword) {
  const res = await fetch(`${API_BASE}/api/users/reset-password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${getToken()}` },
    body: JSON.stringify({ username, newPassword }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to reset password');
  return data;
}

export async function getServerInfo() {
  const res = await fetch(`${API_BASE}/api/server-info`, {
    headers: { 'Authorization': `Bearer ${getToken()}` },
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to get server info');
  return data;
}
