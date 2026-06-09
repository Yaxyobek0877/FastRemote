import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { getDeviceInfo, isAuthenticated } from '../utils/api';

// Uptime soniyalarini "2d 4h 13m" ko'rinishiga o'tkazadi
function formatUptime(sec) {
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

const StatIcon = ({ d }) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
    strokeLinecap="round" strokeLinejoin="round" width="20" height="20">
    {d}
  </svg>
);

export default function HomePage() {
  const [info, setInfo] = useState(null);
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const data = await getDeviceInfo();
        if (active) { setInfo(data); setError(false); }
      } catch {
        if (active) setError(true);
      } finally {
        if (active) setLoading(false);
      }
    };
    load();
    const t = setInterval(load, 5000); // jonli holat: har 5s
    return () => { active = false; clearInterval(t); };
  }, []);

  const online = !!info?.online && !error;

  const stats = [
    { label: 'Operatsion tizim', value: info ? `${info.os} (${info.arch})` : '—',
      icon: <StatIcon d={<><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></>} /> },
    { label: 'Protsessor yadrolari', value: info ? `${info.cpuCores} ta` : '—',
      icon: <StatIcon d={<><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></>} /> },
    { label: 'Ekran o\'lchami', value: info?.screenWidth ? `${info.screenWidth} × ${info.screenHeight}` : '—',
      icon: <StatIcon d={<><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></>} /> },
    { label: 'IP manzil', value: info?.ip || '—',
      icon: <StatIcon d={<><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></>} /> },
    { label: 'Ishlash vaqti (uptime)', value: formatUptime(info?.uptimeSeconds),
      icon: <StatIcon d={<><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></>} /> },
    { label: 'Faol ulanishlar', value: info ? `${info.viewers ?? 0} ta` : '—',
      icon: <StatIcon d={<><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></>} /> },
  ];

  return (
    <div className="login-page home-page">
      <div className="login-bg" />
      <div className="login-grid" />

      <div className="home-container">
        {/* Sarlavha */}
        <div className="home-header">
          <div className="login-logo-icon home-logo-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" width="34" height="34">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
            </svg>
          </div>
          <h1 className="home-title">FastRemote</h1>
          <p className="home-subtitle">Qurilma holati paneli</p>
        </div>

        {/* Qurilma kartasi */}
        <div className="home-device-card">
          <div className="home-device-top">
            <div className="home-device-id">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" width="22" height="22">
                <rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>
              </svg>
              <div>
                <div className="home-device-name">{info?.deviceName || (loading ? 'Yuklanmoqda…' : 'FastRemote')}</div>
                {info?.hostname && <div className="home-device-host">{info.hostname}</div>}
              </div>
            </div>
            <div className={`home-status-pill ${online ? 'is-online' : 'is-offline'}`}>
              <span className="rd-status-dot" />
              {online ? 'Onlayn' : (loading ? 'Tekshirilmoqda…' : 'Oflayn')}
            </div>
          </div>

          {error && (
            <div className="home-error">
              Qurilmaga ulanib bo'lmadi. Agent ishlayotganini tekshiring.
            </div>
          )}

          {/* Holat to'ri */}
          <div className="home-stats-grid">
            {stats.map((s) => (
              <div className="home-stat" key={s.label}>
                <div className="home-stat-icon">{s.icon}</div>
                <div className="home-stat-body">
                  <div className="home-stat-label">{s.label}</div>
                  <div className="home-stat-value">{s.value}</div>
                </div>
              </div>
            ))}
          </div>

          {/* Boshqarish tugmasi */}
          <button
            className="btn btn-primary home-connect-btn"
            onClick={() => navigate(isAuthenticated() ? '/devices' : '/login')}
            disabled={!online}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" width="18" height="18">
              <path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><polyline points="10 17 15 12 10 7"/><line x1="15" y1="12" x2="3" y2="12"/>
            </svg>
            {isAuthenticated() ? 'Boshqaruvga o\'tish' : 'Masofadan boshqarish'}
          </button>
        </div>

        <div className="home-footer">
          <span>FastRemote v{info?.version || '1.0.0'}</span>
          <span className="home-footer-sep">•</span>
          <span>End-to-end shifrlangan</span>
        </div>
      </div>
    </div>
  );
}
