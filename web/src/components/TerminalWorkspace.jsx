import { useCallback, useEffect, useRef, useState } from 'react';
import { restartTerminal, terminalRequest } from '../utils/api';
import TerminalPanel from './TerminalPanel';

export default function TerminalWorkspace() {
  const [sessions, setSessions] = useState([]), [active, setActive] = useState('');
  const activeRef = useRef(''), newButton = useRef(null);
  const [loaded, setLoaded] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [dialog, setDialog] = useState(null), [name, setName] = useState('');
  const [projects, setProjects] = useState([]), [custom, setCustom] = useState(false);
  const [cwd, setCwd] = useState(() => localStorage.getItem('terminal_project') || '');
  const close = () => { setDialog(null); setError(''); newButton.current?.focus(); };
  const refresh = useCallback(async () => {
    try {
      const data = await terminalRequest(); setSessions(data.sessions); setProjects(data.projects || []);
      if (!data.sessions.some(s => s.id === activeRef.current)) {
        const id = data.sessions.find(s => s.id === data.activeId)?.id || data.sessions[0]?.id || '';
        activeRef.current = id; setActive(id);
      }
      setLoaded(true);
    } catch (e) { setError(e.message); }
  }, []);
  useEffect(() => {
    refresh(); const timer = setInterval(() => { if (!document.hidden) refresh(); }, 5000);
    window.addEventListener('focus', refresh);
    return () => { clearInterval(timer); window.removeEventListener('focus', refresh); };
  }, [refresh]);
  const select = id => {
    activeRef.current = id; setActive(id);
    if (id !== 'legacy') terminalRequest('PATCH', { id, active: true }).catch(e => setError(e.message));
  };
  const createDialog = () => { setCustom(false); setError(''); setDialog({ type: 'create' }); };
  const restart = async session => {
    if (busy) return;
    setBusy(true); setError('');
    try {
      const restored = await restartTerminal(session.id);
      setSessions(current => current.map(s => s.id === restored.id ? restored : s));
      await refresh();
    } catch (e) { setError(e.message); } finally { setBusy(false); }
  };
  const submit = async event => {
    event.preventDefault(); if (busy) return; setBusy(true); setError('');
    try {
      if (dialog.type === 'create') {
        const session = await terminalRequest('POST', { cwd, title: projects.find(p => p.path === cwd)?.name || undefined });
        localStorage.setItem('terminal_project', cwd); activeRef.current = session.id; setActive(session.id);
      } else if (dialog.type === 'rename') await terminalRequest('PATCH', { id: dialog.session.id, title: name.trim() });
      else await terminalRequest('DELETE', { id: dialog.session.id });
      close(); await refresh();
    } catch (e) { setError(e.message); } finally { setBusy(false); }
  };
  const selected = sessions.find(s => s.id === active);
  const connectionState = ['stopped', 'exited'].includes(selected?.status) ? 'stopped' : 'live';
  return <div className="rd-terminal-area">
    <div className="terminal-session-bar">
      <div className="rd-terminal-tabs" role="tablist" aria-label="Terminal sessiyalari">
        {sessions.map(s => <button key={s.id} role="tab" aria-selected={s.id === active} aria-controls="active-terminal" className={`rd-term-tab ${s.id === active ? 'active' : ''}`} onClick={() => select(s.id)} title={`${s.title}${s.cwd ? ' · ' + s.cwd : ''}`}>
          <span className={`terminal-dot ${s.status}`} /><span className="rd-term-tab-label">{s.title}</span>
        </button>)}
        {!sessions.length && <strong className="terminal-bar-title">Terminallar</strong>}
      </div>
      <button ref={newButton} className="terminal-primary terminal-add" onClick={createDialog} disabled={busy} aria-label="Yangi terminal" title="Loyiha papkasida terminal ochish">＋ <span>Yangi terminal</span></button>
    </div>
    {error && !dialog && <div className="terminal-alert" role="alert">{error}<button onClick={() => { setError(''); refresh(); }}>Qayta urinish</button></div>}
    {selected ? <div className="rd-terminal-panel-wrapper" id="active-terminal" role="tabpanel">
      <TerminalPanel key={`${selected.id}:${selected.recoveredAt || ''}:${connectionState}`} session={selected} restarting={busy} onRestart={() => restart(selected)} onRename={() => { setName(selected.title); setDialog({ type: 'rename', session: selected }); }} onRemove={() => setDialog({ type: 'remove', session: selected })} />
    </div> : <div className="terminal-empty">
      <span className="terminal-empty-icon">&gt;_</span><h2>{loaded ? 'Ishingiz shu yerda davom etadi' : 'Terminallar yuklanmoqda…'}</h2>
      <p>Loyihani tanlang va terminal oching. Sessiyalar sahifa yopilganda ham serverda ishlashda davom etadi.</p>
      {loaded && <button className="terminal-primary" onClick={createDialog} disabled={busy}>＋ Terminal ochish</button>}
    </div>}
    {dialog && <div className="rd-modal-overlay" onKeyDown={e => {
      if (e.key === 'Escape' && !busy) close();
      if (e.key === 'Tab') {
        const nodes = [...e.currentTarget.querySelectorAll('button:not(:disabled), input, select')], first = nodes[0], last = nodes.at(-1);
        if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
      }
    }}><form className="rd-modal" onSubmit={submit} role="dialog" aria-modal="true" aria-labelledby="terminal-dialog-title">
      <h3 id="terminal-dialog-title">{dialog.type === 'create' ? 'Yangi terminal' : dialog.type === 'rename' ? 'Terminal nomi' : 'Terminalni tugatish'}</h3>
      {dialog.type === 'create' ? <>
        <p>Terminal tanlangan loyiha papkasida ochiladi.</p>
        <label className="terminal-field">Loyiha<select className="rd-settings-input" autoFocus value={custom ? '__custom' : cwd} onChange={e => { const v = e.target.value; setCustom(v === '__custom'); if (v !== '__custom') setCwd(v); }}>
          <option value="">Uy papkasi</option>{projects.map(p => <option key={p.path} value={p.path}>{p.name}</option>)}
          {cwd && !projects.some(p => p.path === cwd) && <option value={cwd}>{cwd}</option>}
          <option value="__custom">Boshqa papka…</option>
        </select></label>
        {custom ? <label className="terminal-field">Papka yo‘li<input className="rd-settings-input" value={cwd} onChange={e => setCwd(e.target.value)} placeholder="/home/user/projects/loyiha" required /></label> : <p className="terminal-project-path">{cwd || '~'}</p>}
      </> : dialog.type === 'rename' ? <input aria-label="Terminal nomi" className="rd-settings-input" value={name} onChange={e => setName(e.target.value)} maxLength={80} required autoFocus /> : <p><strong>{dialog.session.title}</strong> ichidagi dasturlar to‘xtaydi va tarix o‘chiriladi. Ishni saqlash uchun browserni yopishingiz mumkin.</p>}
      {error && <p role="alert" className="terminal-danger">{error}</p>}
      <div className="rd-modal-actions">
        <button type="button" className="terminal-button" onClick={close} disabled={busy} autoFocus={dialog.type === 'remove'}>Bekor qilish</button>
        <button className={dialog.type === 'remove' ? 'terminal-button terminal-danger' : 'terminal-primary'} disabled={busy || (dialog.type === 'rename' && !name.trim())}>{busy ? 'Kutilmoqda…' : dialog.type === 'create' ? 'Ochish' : dialog.type === 'rename' ? 'Saqlash' : 'Tugatish va o‘chirish'}</button>
      </div>
    </form></div>}
  </div>;
}
