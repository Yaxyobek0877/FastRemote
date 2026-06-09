import { useState, useEffect, useRef, useCallback } from 'react';
import { createSessionSocket, getUsername, logout, getSettings, updateSettings, changePassword as apiChangePassword, addUser as apiAddUser, deleteUser as apiDeleteUser, resetUserPassword as apiResetPassword, getServerInfo, getDeviceInfo } from '../utils/api';
import ScreenViewer from '../components/ScreenViewer';
import TerminalPanel from '../components/TerminalPanel';
import FileTransferPanel from '../components/FileTransferPanel';

const QUALITY_PRESETS = {
  low:    { fps: 10, quality: 50, maxWidth: 1920, maxHeight: 1080, label: 'Low' },
  medium: { fps: 15, quality: 70, maxWidth: 2560, maxHeight: 1440, label: 'Medium' },
  high:   { fps: 15, quality: 85, maxWidth: 3840, maxHeight: 2160, label: 'High' },
  ultra:  { fps: 25, quality: 95, maxWidth: 3840, maxHeight: 2160, label: 'Ultra' },
};

const I = {
  screen: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>,
  terminal: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>,
  folder: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>,
  fullscreen: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>,
  settings: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>,
  logout: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>,
  plus: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>,
  close: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>,
  bolt: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>,
  sliders: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></svg>,
  check: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>,
  lock: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>,
  edit: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>,
  users: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>,
  trash: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>,
  key: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>,
  server: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>,
  clock: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>,
  home: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>,
  cpu: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>,
  globe: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>,
};

// soniyalarni "2d 4h 13m" ko'rinishiga o'tkazadi (device uptime uchun)
function formatSeconds(sec) {
  if (sec == null || isNaN(sec)) return '—';
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  const parts = [];
  if (d) parts.push(`${d}d`);
  if (h) parts.push(`${h}h`);
  if (m) parts.push(`${m}m`);
  if (!d && !h) parts.push(`${s}s`);
  return parts.join(' ');
}

function formatUptime(ms) {
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  if (h > 0) return `${h}h ${m % 60}m`;
  if (m > 0) return `${m}m ${s % 60}s`;
  return `${s}s`;
}

// ====================== SETTINGS PAGE ======================
function SettingsPage() {
  const [dName, setDName] = useState('');
  const [editingName, setEditingName] = useState(false);
  const [users, setUsers] = useState([]);
  const [curPass, setCurPass] = useState('');
  const [newPass, setNewPass] = useState('');
  const [confirmPass, setConfirmPass] = useState('');
  const [msg, setMsg] = useState({ text: '', type: '' });
  const [saving, setSaving] = useState(false);
  const [showAddUser, setShowAddUser] = useState(false);
  const [newUser, setNewUser] = useState({ username: '', password: '', role: 'user' });
  const [addMsg, setAddMsg] = useState({ text: '', type: '' });
  const [resetModal, setResetModal] = useState(null); // { username }
  const [resetPass, setResetPass] = useState('');
  const [serverInfo, setServerInfo] = useState(null);

  const loadSettings = async () => {
    try {
      const d = await getSettings();
      if (d.deviceName) setDName(d.deviceName);
      if (d.users) setUsers(d.users);
    } catch {}
  };

  useEffect(() => {
    loadSettings();
    getServerInfo().then(setServerInfo).catch(() => {});
  }, []);

  const saveName = async () => {
    setSaving(true); setMsg({ text: '', type: '' });
    try {
      await updateSettings({ deviceName: dName });
      setMsg({ text: 'Device name saved!', type: 'success' });
      setEditingName(false);
    } catch { setMsg({ text: 'Failed to save', type: 'error' }); }
    setSaving(false);
  };

  const passChecks = (p) => [
    { ok: p.length >= 8, text: 'At least 8 characters' },
    { ok: /[a-zA-Z]/.test(p), text: 'Contains a letter' },
    { ok: /\d/.test(p), text: 'Contains a number' },
    { ok: /[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?~`]/.test(p), text: 'Contains a symbol' },
  ];

  const handleChangePass = async (e) => {
    e.preventDefault();
    if (newPass !== confirmPass) { setMsg({ text: 'Passwords do not match', type: 'error' }); return; }
    setSaving(true); setMsg({ text: '', type: '' });
    try {
      await apiChangePassword(curPass, newPass);
      setMsg({ text: 'Password changed!', type: 'success' });
      setCurPass(''); setNewPass(''); setConfirmPass('');
    } catch (err) { setMsg({ text: err.message, type: 'error' }); }
    setSaving(false);
  };

  const handleAddUser = async (e) => {
    e.preventDefault();
    setAddMsg({ text: '', type: '' });
    try {
      await apiAddUser(newUser.username, newUser.password, newUser.role);
      setAddMsg({ text: `User "${newUser.username}" created!`, type: 'success' });
      setNewUser({ username: '', password: '', role: 'user' });
      setShowAddUser(false);
      loadSettings();
    } catch (err) { setAddMsg({ text: err.message, type: 'error' }); }
  };

  const handleDeleteUser = async (username) => {
    if (!confirm(`Delete user "${username}"?`)) return;
    try {
      await apiDeleteUser(username);
      loadSettings();
    } catch (err) { setMsg({ text: err.message, type: 'error' }); }
  };

  const handleResetPassword = async () => {
    if (!resetModal) return;
    try {
      await apiResetPassword(resetModal.username, resetPass);
      setMsg({ text: `Password reset for "${resetModal.username}"!`, type: 'success' });
      setResetModal(null);
      setResetPass('');
    } catch (err) { setMsg({ text: err.message, type: 'error' }); }
  };

  const checks = passChecks(newPass);
  const allValid = checks.every(c => c.ok) && newPass === confirmPass && curPass.length > 0;

  return (
    <div className="rd-settings-page">
      <div className="rd-settings-header">
        <h1>{I.settings} Settings</h1>
      </div>

      {msg.text && <div className={`rd-settings-msg ${msg.type}`}>{msg.text}</div>}

      {/* Server Info */}
      {serverInfo && (
        <div className="rd-settings-card">
          <h3 className="rd-settings-card-title">{I.server} Server</h3>
          <div className="rd-server-info-grid">
            <div className="rd-server-info-item">
              <span className="rd-server-label">Port</span>
              <span className="rd-server-value">{serverInfo.port}</span>
            </div>
            <div className="rd-server-info-item">
              <span className="rd-server-label">Screen</span>
              <span className="rd-server-value">{serverInfo.screenW}×{serverInfo.screenH}</span>
            </div>
            <div className="rd-server-info-item">
              <span className="rd-server-label">Hostname</span>
              <span className="rd-server-value">{serverInfo.hostname}</span>
            </div>
          </div>
        </div>
      )}

      {/* Device Section */}
      <div className="rd-settings-card">
        <h3 className="rd-settings-card-title">{I.screen} Device</h3>
        <div className="rd-settings-field">
          <label>Device Name</label>
          <div className="rd-settings-row">
            {editingName ? (<>
              <input className="rd-settings-input" value={dName} onChange={e => setDName(e.target.value)}
                autoFocus onKeyDown={e => e.key === 'Enter' && saveName()} />
              <button className="rd-settings-save" onClick={saveName} disabled={saving}>{I.check}</button>
              <button className="rd-settings-cancel" onClick={() => setEditingName(false)}>{I.close}</button>
            </>) : (<>
              <span className="rd-settings-value">{dName}</span>
              <button className="rd-settings-edit" onClick={() => setEditingName(true)}>{I.edit} Edit</button>
            </>)}
          </div>
        </div>
      </div>

      {/* Users Section */}
      <div className="rd-settings-card">
        <div className="rd-settings-card-header">
          <h3 className="rd-settings-card-title">{I.users} Users</h3>
          <button className="rd-settings-add-btn" onClick={() => setShowAddUser(!showAddUser)}>
            {I.plus} Add User
          </button>
        </div>

        {addMsg.text && <div className={`rd-settings-msg ${addMsg.type}`}>{addMsg.text}</div>}

        {showAddUser && (
          <form onSubmit={handleAddUser} className="rd-add-user-form">
            <input className="rd-settings-input" placeholder="Username (min 2 chars)" value={newUser.username}
              onChange={e => setNewUser({...newUser, username: e.target.value})} required />
            <input className="rd-settings-input" type="password" placeholder="Password (8+ chars)"
              value={newUser.password} onChange={e => setNewUser({...newUser, password: e.target.value})} required />
            <select className="rd-settings-input" value={newUser.role}
              onChange={e => setNewUser({...newUser, role: e.target.value})}>
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
            <button type="submit" className="rd-settings-submit">Create</button>
          </form>
        )}

        <div className="rd-users-list">
          {users.map(u => (
            <div key={u.username} className="rd-user-row">
              <div className="rd-user-row-info">
                <div className="rd-user-row-avatar">{u.username.charAt(0).toUpperCase()}</div>
                <div>
                  <div className="rd-user-row-name">{u.username}</div>
                  <div className="rd-user-row-role">{u.role}</div>
                </div>
              </div>
              <div className="rd-user-row-actions">
                <button className="rd-user-action-btn" onClick={() => { setResetModal({ username: u.username }); setResetPass(''); }}
                  title="Reset password">{I.key}</button>
                <button className="rd-user-delete" onClick={() => handleDeleteUser(u.username)}
                  title="Delete user">{I.trash}</button>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Reset Password Modal */}
      {resetModal && (
        <div className="rd-modal-overlay" onClick={() => setResetModal(null)}>
          <div className="rd-modal" onClick={e => e.stopPropagation()}>
            <h3>{I.key} Reset Password</h3>
            <p>Set new password for <strong>{resetModal.username}</strong></p>
            <input className="rd-settings-input" type="password" placeholder="New password (8+ chars)"
              value={resetPass} onChange={e => setResetPass(e.target.value)} autoFocus />
            <div className="rd-modal-actions">
              <button className="rd-settings-cancel" onClick={() => setResetModal(null)}>Cancel</button>
              <button className="rd-settings-submit" onClick={handleResetPassword}
                disabled={resetPass.length < 8}>Reset</button>
            </div>
          </div>
        </div>
      )}

      {/* Password Section */}
      <div className="rd-settings-card">
        <h3 className="rd-settings-card-title">{I.lock} Change My Password</h3>
        <form onSubmit={handleChangePass} className="rd-settings-form">
          <input className="rd-settings-input" type="password" placeholder="Current password"
            value={curPass} onChange={e => setCurPass(e.target.value)} required />
          <input className="rd-settings-input" type="password" placeholder="New password"
            value={newPass} onChange={e => setNewPass(e.target.value)} required />
          <input className="rd-settings-input" type="password" placeholder="Confirm new password"
            value={confirmPass} onChange={e => setConfirmPass(e.target.value)} required />
          {newPass && (
            <div className="rd-pass-checks">
              {checks.map((c, i) => (
                <div key={i} className={`rd-pass-check ${c.ok ? 'ok' : ''}`}>
                  <span className="rd-check-dot" /> {c.text}
                </div>
              ))}
              {confirmPass && (
                <div className={`rd-pass-check ${newPass === confirmPass ? 'ok' : ''}`}>
                  <span className="rd-check-dot" /> Passwords match
                </div>
              )}
            </div>
          )}
          <button type="submit" className="rd-settings-submit" disabled={!allValid || saving}>
            {saving ? 'Saving...' : 'Update Password'}
          </button>
        </form>
      </div>
    </div>
  );
}

// ====================== HOME PANEL (Bosh sahifa) ======================
function HomePanel({ connectionStatus, sessionUptime }) {
  const [info, setInfo] = useState(null);
  const [err, setErr] = useState(false);

  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const d = await getDeviceInfo();
        if (active) { setInfo(d); setErr(false); }
      } catch {
        if (active) setErr(true);
      }
    };
    load();
    const t = setInterval(load, 5000); // jonli holat: har 5s
    return () => { active = false; clearInterval(t); };
  }, []);

  const online = connectionStatus === 'connected' && !err;
  const stats = [
    { label: 'Operatsion tizim', icon: I.screen, value: info ? `${info.os} (${info.arch})` : '—' },
    { label: 'Protsessor yadrolari', icon: I.cpu, value: info ? `${info.cpuCores} ta` : '—' },
    { label: "Ekran o'lchami", icon: I.screen, value: info?.screenWidth ? `${info.screenWidth} × ${info.screenHeight}` : '—' },
    { label: 'IP manzil', icon: I.globe, value: info?.ip || '—' },
    { label: 'Qurilma ishlash vaqti', icon: I.clock, value: formatSeconds(info?.uptimeSeconds) },
    { label: 'Faol ulanishlar', icon: I.users, value: info ? `${info.viewers ?? 0} ta` : '—' },
  ];

  return (
    <div className="rd-settings-page">
      <div className="rd-settings-header">
        <h1>{I.home} Bosh sahifa</h1>
      </div>

      {/* Qurilma holati */}
      <div className="rd-settings-card">
        <div className="rd-settings-card-header">
          <h3 className="rd-settings-card-title">{I.server} {info?.deviceName || 'Qurilma'}</h3>
          <div className={`home-status-pill ${online ? 'is-online' : 'is-offline'}`}>
            <span className="rd-status-dot" />
            {online ? 'Onlayn' : 'Oflayn'}
          </div>
        </div>
        {info?.hostname && (
          <div className="rd-settings-field">
            <span className="home-host-line">{I.server} {info.hostname} · sessiya: {sessionUptime}</span>
          </div>
        )}
        {err && <div className="rd-settings-msg error">Qurilma holatini olishda xatolik.</div>}

        <div className="home-stats-grid">
          {stats.map((s, i) => (
            <div className="home-stat" key={i}>
              <div className="home-stat-icon">{s.icon}</div>
              <div className="home-stat-body">
                <div className="home-stat-label">{s.label}</div>
                <div className="home-stat-value">{s.value}</div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// ====================== MAIN SESSION PAGE ======================
export default function SessionPage() {
  const [activePanel, setActivePanel] = useState(() => localStorage.getItem('rd_panel') || 'home');
  const [ws, setWs] = useState(null);
  const [isConnected, setIsConnected] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState('connecting');
  const [deviceInfo, setDeviceInfo] = useState({ name: 'FastRemote', os: '' });
  const [sidebarOpen, setSidebarOpen] = useState(() => localStorage.getItem('rd_sidebar') !== 'closed');
  const [qualityPreset, setQualityPreset] = useState('high');
  const [customQuality, setCustomQuality] = useState({ fps: 15, quality: 80, maxWidth: 3840, maxHeight: 2160 });
  const [showQualityMenu, setShowQualityMenu] = useState(false);
  const [terminalTabs, setTerminalTabs] = useState([{ id: 1, label: 'Shell 1' }]);
  const [activeTerminalTab, setActiveTerminalTab] = useState(1);
  const [nextTabId, setNextTabId] = useState(2);
  const [sessionStart] = useState(Date.now());
  const [uptime, setUptime] = useState('0s');
  const wsRef = useRef(null);
  const username = getUsername();

  // Session timer
  useEffect(() => {
    const t = setInterval(() => setUptime(formatUptime(Date.now() - sessionStart)), 1000);
    return () => clearInterval(t);
  }, [sessionStart]);

  // Persist state
  useEffect(() => { localStorage.setItem('rd_panel', activePanel); }, [activePanel]);
  useEffect(() => { localStorage.setItem('rd_sidebar', sidebarOpen ? 'open' : 'closed'); }, [sidebarOpen]);

  // WebSocket with exponential backoff reconnect
  useEffect(() => {
    let cancelled = false;
    let reconnectTimer = null;
    let retryCount = 0;
    let everConnected = false;

    function connect() {
      if (cancelled) return;
      const socket = createSessionSocket();
      socket.binaryType = 'arraybuffer';
      const openTime = Date.now();

      socket.onopen = () => {
        retryCount = 0;
        everConnected = true;
        setIsConnected(true);
        setConnectionStatus('connected');
      };
      socket.onmessage = (event) => {
        if (typeof event.data === 'string') {
          try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'system_info') {
              setDeviceInfo({ name: msg.payload.deviceName || msg.payload.hostname, os: msg.payload.os });
            }
          } catch (e) {}
        }
      };
      socket.onclose = (e) => {
        setIsConnected(false);
        if (e.code === 4001) { logout(); return; }
        const lifespan = Date.now() - openTime;
        if (!everConnected && lifespan < 2000) {
          retryCount++;
          if (retryCount >= 3) { logout(); return; }
        }
        const delay = Math.min(2000 * Math.pow(1.5, retryCount), 30000);
        retryCount++;
        if (retryCount > 15) { setConnectionStatus('failed'); return; }
        setConnectionStatus('reconnecting');
        if (!cancelled) { reconnectTimer = setTimeout(connect, delay); }
      };
      socket.onerror = () => {};
      wsRef.current = socket;
      setWs(socket);
    }

    connect();
    return () => {
      cancelled = true;
      clearTimeout(reconnectTimer);
      if (wsRef.current) { wsRef.current.close(); wsRef.current = null; }
    };
  }, []);

  const changeQuality = useCallback((preset) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const s = QUALITY_PRESETS[preset];
    if (!s) return;
    setQualityPreset(preset);
    ws.send(JSON.stringify({ type: 'quality_settings', payload: { fps: s.fps, quality: s.quality, maxWidth: s.maxWidth, maxHeight: s.maxHeight } }));
    setShowQualityMenu(false);
  }, [ws]);

  const applyCustomQuality = useCallback(() => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    setQualityPreset('manual');
    ws.send(JSON.stringify({ type: 'quality_settings', payload: customQuality }));
    setShowQualityMenu(false);
  }, [ws, customQuality]);

  const addTerminalTab = useCallback(() => {
    const newId = nextTabId;
    setTerminalTabs(prev => [...prev, { id: newId, label: `Shell ${newId}` }]);
    setActiveTerminalTab(newId);
    setNextTabId(prev => prev + 1);
    setActivePanel('terminal');
  }, [nextTabId]);

  const closeTerminalTab = useCallback((tabId) => {
    setTerminalTabs(prev => {
      const newTabs = prev.filter(t => t.id !== tabId);
      if (newTabs.length === 0) {
        const nid = nextTabId;
        setNextTabId(p => p + 1);
        setActiveTerminalTab(nid);
        return [{ id: nid, label: `Shell ${nid}` }];
      }
      if (activeTerminalTab === tabId) setActiveTerminalTab(newTabs[newTabs.length - 1].id);
      return newTabs;
    });
  }, [activeTerminalTab, nextTabId]);

  const toggleFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen();
    else document.documentElement.requestFullscreen();
  };

  const statusDot = connectionStatus === 'connected' ? '#10b981' : connectionStatus === 'reconnecting' ? '#f59e0b' : '#ef4444';

  return (
    <div className="rd-layout">
      {sidebarOpen && (
        <div className="rd-sidebar">
          <div className="rd-sidebar-brand">
            <div className="rd-brand-icon">{I.bolt}</div>
            <div className="rd-brand-info">
              <span className="rd-brand-name">{deviceInfo.name}</span>
              <span className="rd-brand-status">
                <span className="rd-status-dot" style={{ background: statusDot }} />
                {connectionStatus === 'connected' ? 'Online' : connectionStatus === 'reconnecting' ? 'Reconnecting...' : connectionStatus}
                {connectionStatus === 'connected' && <span className="rd-uptime"> · {uptime}</span>}
              </span>
            </div>
          </div>
          <div className="rd-sidebar-divider" />

          <div className="rd-sidebar-nav">
            {[
              { id: 'home', icon: I.home, label: 'Bosh sahifa' },
              { id: 'screen', icon: I.screen, label: 'Screen' },
              { id: 'terminal', icon: I.terminal, label: 'Terminal', badge: terminalTabs.length > 1 ? terminalTabs.length : null },
              { id: 'files', icon: I.folder, label: 'Files' },
              { id: 'settings', icon: I.settings, label: 'Settings' },
            ].map(item => (
              <button key={item.id} className={`rd-sidebar-btn ${activePanel === item.id ? 'active' : ''}`}
                onClick={() => setActivePanel(item.id)} id={`nav-${item.id}`}>
                <span className="rd-btn-icon">{item.icon}</span>
                <span className="rd-btn-label">{item.label}</span>
                {item.badge && <span className="rd-btn-badge">{item.badge}</span>}
              </button>
            ))}
          </div>

          <div className="rd-sidebar-divider" />

          <div className="rd-sidebar-quality">
            <button className={`rd-sidebar-btn ${showQualityMenu ? 'active' : ''}`}
              onClick={() => setShowQualityMenu(!showQualityMenu)}>
              <span className="rd-btn-icon">{I.sliders}</span>
              <span className="rd-btn-label">Quality: {QUALITY_PRESETS[qualityPreset]?.label || 'Manual'}</span>
            </button>
            {showQualityMenu && (
              <div className="rd-quality-dropdown">
                {Object.entries(QUALITY_PRESETS).map(([key, preset]) => (
                  <button key={key} className={`rd-quality-item ${qualityPreset === key ? 'active' : ''}`}
                    onClick={() => changeQuality(key)}>
                    <span className="rd-quality-name">{preset.label}</span>
                    <span className="rd-quality-detail">{preset.fps}fps · q{preset.quality}</span>
                  </button>
                ))}
                
                <div style={{ padding: '10px', display: 'flex', flexDirection: 'column', gap: '8px', borderTop: '1px solid rgba(255,255,255,0.1)', marginTop: '5px' }}>
                   <span style={{ fontSize: '0.8rem', color: '#9ca3af', fontWeight: 'bold' }}>MANUAL CONFIG</span>
                   <div style={{ display: 'flex', gap: '10px' }}>
                      <input type="number" className="rd-settings-input" style={{ padding: '4px', fontSize: '0.8rem', minHeight: 'unset' }} title="FPS" placeholder="FPS" value={customQuality.fps} onChange={e=>setCustomQuality({...customQuality, fps: parseInt(e.target.value)||1})} />
                      <input type="number" className="rd-settings-input" style={{ padding: '4px', fontSize: '0.8rem', minHeight: 'unset' }} title="Quality" placeholder="Quality%" value={customQuality.quality} onChange={e=>setCustomQuality({...customQuality, quality: parseInt(e.target.value)||1})} />
                   </div>
                   <button className="rd-settings-submit" style={{ padding: '5px', fontSize: '0.8rem' }} onClick={applyCustomQuality}>Apply Manual Settings</button>
                </div>
              </div>
            )}
          </div>

          <div className="rd-sidebar-actions">
            <button className="rd-sidebar-btn" onClick={toggleFullscreen}>
              <span className="rd-btn-icon">{I.fullscreen}</span>
              <span className="rd-btn-label">Fullscreen</span>
            </button>
            <button className="rd-sidebar-btn danger" onClick={logout}>
              <span className="rd-btn-icon">{I.logout}</span>
              <span className="rd-btn-label">Disconnect</span>
            </button>
          </div>

          <div className="rd-sidebar-user">
            <div className="rd-user-avatar">{username.charAt(0).toUpperCase()}</div>
            <span className="rd-user-name">{username}</span>
          </div>

          <button className="rd-collapse-btn" onClick={() => setSidebarOpen(false)} title="Hide sidebar">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="11 17 6 12 11 7"/><polyline points="18 17 13 12 18 7"/></svg>
          </button>
        </div>
      )}

      {!sidebarOpen && (
        <button className="rd-expand-handle" onClick={() => setSidebarOpen(true)} title="Show sidebar">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="13 17 18 12 13 7"/><polyline points="6 17 11 12 6 7"/>
          </svg>
        </button>
      )}

      <div className="rd-content">
        {activePanel === 'home' && <HomePanel connectionStatus={connectionStatus} sessionUptime={uptime} />}
        {activePanel === 'screen' && <ScreenViewer ws={ws} isConnected={isConnected} />}
        {activePanel === 'terminal' && (
          <div className="rd-terminal-area">
            <div className="rd-terminal-tabs">
              {terminalTabs.map(tab => (
                <div key={tab.id} className={`rd-term-tab ${activeTerminalTab === tab.id ? 'active' : ''}`}
                  onClick={() => setActiveTerminalTab(tab.id)}>
                  <span className="rd-term-tab-icon">{I.terminal}</span>
                  <span className="rd-term-tab-label">{tab.label}</span>
                  <button className="rd-term-tab-close"
                    onClick={(e) => { e.stopPropagation(); closeTerminalTab(tab.id); }}>{I.close}</button>
                </div>
              ))}
              <button className="rd-term-tab-add" onClick={addTerminalTab} title="New terminal">{I.plus}</button>
            </div>
            {terminalTabs.map(tab => (
              <div key={tab.id} className="rd-terminal-panel-wrapper"
                style={{ display: activeTerminalTab === tab.id ? 'flex' : 'none' }}>
                <TerminalPanel ws={ws} isConnected={isConnected} sessionId={tab.id}
                  isVisible={activeTerminalTab === tab.id} />
              </div>
            ))}
          </div>
        )}
        {activePanel === 'files' && <FileTransferPanel ws={ws} isConnected={isConnected} />}
        {activePanel === 'settings' && <SettingsPage />}
      </div>
    </div>
  );
}
