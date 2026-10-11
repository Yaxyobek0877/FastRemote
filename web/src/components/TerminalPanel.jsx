import { useRef, useEffect, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { createTerminalSocket, uploadTerminalFiles } from '../utils/api';
import '@xterm/xterm/css/xterm.css';

const KEYS = [['Esc', '\x1b'], ['Tab', '\t'], ['Ctrl C', '\x03'], ['Ctrl D', '\x04'], ['Ctrl L', '\x0c'], ['↑', '\x1b[A'], ['↓', '\x1b[B'], ['←', '\x1b[D'], ['→', '\x1b[C'], ['Home', '\x1b[H'], ['End', '\x1b[F']];
const shellQuote = value => `'${value.replaceAll("'", "'\\''")}'`;

export default function TerminalPanel({ session, restarting, onRestart, onRename, onRemove }) {
  const container = useRef(null);
  const controls = useRef({});
  const fileInput = useRef(null);
  const [status, setStatus] = useState('connecting');
  const [notice, setNotice] = useState('');
  const [showKeys, setShowKeys] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const recoveryKey = session.recoveredAt ? `terminal_recovery_seen:${session.id}:${session.recoveredAt}` : '';
  const [recoveryDismissed, setRecoveryDismissed] = useState(() => {
    try { return !!recoveryKey && localStorage.getItem(recoveryKey) === '1'; } catch { return false; }
  });
  const [fontSize, setFontSize] = useState(() => {
    const saved = Number(localStorage.getItem('terminal_font_size'));
    return saved >= 10 && saved <= 22 ? saved : window.innerWidth < 600 ? 12 : 14;
  });
  const fontRef = useRef(fontSize);
  const id = session.id;
  const recoveryText = session.recoveryNote || 'Terminal qayta ochildi. Avvalgi dastur ishlamayapti; saqlangan tarix ko‘rsatilgan.';
  const recoveredDate = new Date(session.recoveredAt);
  const recoveryDetails = `${Number.isNaN(recoveredDate.getTime()) ? '' : recoveredDate.toLocaleString() + ' · '}${recoveryText}`;
  const showRecovery = !!session.recoveredAt && !recoveryDismissed;
  const dismissNotice = () => {
    if (notice) { setNotice(''); return; }
    setRecoveryDismissed(true);
    try { localStorage.setItem(recoveryKey, '1'); } catch { /* Still dismiss for this visit. */ }
  };
  const copyResumeCommand = async () => {
    try {
      await navigator.clipboard.writeText(session.resumeCommand);
      setNotice('Davom ettirish buyrug‘i nusxalandi. Uni terminalga joylab, o‘zingiz ishga tushiring.');
    } catch { setNotice(`Davom ettirish buyrug‘i: ${session.resumeCommand}`); }
  };

  useEffect(() => {
    let disposed = false, socket, retryTimer, fitTimer, redrawTimer, attempts = 0, stopped = false;
    let legacyRedrawPending = false, uploadPending = false;
    const term = new Terminal({
      cursorBlink: true, cursorStyle: 'bar', fontSize: fontRef.current,
      fontFamily: "'Cascadia Code', 'SF Mono', Menlo, Consolas, monospace",
      theme: { background: '#0a0e17', foreground: '#e2e8f0', cursor: '#60a5fa', selectionBackground: '#264570', black: '#1e293b', red: '#ef4444', green: '#10b981', yellow: '#f59e0b', blue: '#60a5fa', magenta: '#a78bfa', cyan: '#22d3ee', white: '#e2e8f0' },
      lineHeight: 1.15, scrollback: 10000, scrollOnUserInput: true,
      disableStdin: true, allowProposedApi: false,
    });
    const fit = new FitAddon(); term.loadAddon(fit); term.open(container.current);
    const send = (type, payload) => {
      if (socket?.readyState !== WebSocket.OPEN || stopped) return;
      if (type === 'input' && payload.data.length > 4096) {
        let chunk = '';
        for (const char of payload.data) { if (chunk.length + char.length > 4096) { send(type, { data: chunk }); chunk = ''; } chunk += char; }
        if (chunk) send(type, { data: chunk });
        return;
      }
      if (id === 'legacy') socket.send(JSON.stringify({ type: type === 'input' ? 'shell_input' : 'shell_resize', payload }));
      else socket.send(JSON.stringify({ type, ...payload }));
    };
    const uploadFiles = async selected => {
      const files = Array.from(selected || []);
      if (!files.length || disposed || uploadPending) return;
      if (term.options.disableStdin || stopped || socket?.readyState !== WebSocket.OPEN) {
        setNotice('Fayl joylash uchun terminal ulanishini kuting.'); return;
      }
      if (files.length > 10 || files.some(file => file.size > 25 * 1024 * 1024) || files.reduce((total, file) => total + file.size, 0) > 50 * 1024 * 1024) {
        setNotice('Ko‘pi bilan 10 ta fayl: har biri 25 MB, jami 50 MB gacha.'); return;
      }
      uploadPending = true; setUploading(true); setNotice(`${files.length} ta fayl yuklanmoqda…`);
      try {
        const result = await uploadTerminalFiles(id, files);
        if (disposed) return;
        const paths = result.files.map(file => shellQuote(file.path)).join(' ');
        if (term.options.disableStdin || stopped || socket?.readyState !== WebSocket.OPEN) {
          setNotice(`Fayl yuklandi. Qayta ulangach yo‘lni joylang: ${paths}`); return;
        }
        // Pasting never submits the command; the user decides when to press Enter.
        term.paste(`${paths} `); term.focus();
        setNotice(`${result.files.length} ta fayl yuklandi, yo‘li terminalga joylandi. Yuborish uchun Enter bosing.`);
      } catch (error) { if (!disposed) setNotice(error.message || 'Fayl yuklanmadi. Qayta urinib ko‘ring.'); }
      finally { uploadPending = false; if (!disposed) setUploading(false); }
    };
    const pasteClipboard = async () => {
      try {
        if (navigator.clipboard?.read) {
          const items = await navigator.clipboard.read();
          const files = [];
          let text = '';
          for (const item of items) {
            const mime = item.types.find(type => type.startsWith('image/'));
            if (mime) {
              const blob = await item.getType(mime);
              const suffix = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/gif': 'gif' }[mime] || 'img';
              files.push(new File([blob], `clipboard-${Date.now()}-${files.length + 1}.${suffix}`, {type:mime}));
            } else if (item.types.includes('text/plain')) text += await (await item.getType('text/plain')).text();
          }
          if (files.length) { await uploadFiles(files); return; }
          if (text) { term.paste(text); term.focus(); return; }
        }
        term.paste(await navigator.clipboard.readText()); term.focus();
      } catch {
        try { term.paste(await navigator.clipboard.readText()); term.focus(); }
        catch { setNotice('Rasm yoki fayl uchun 📎 tugmasini bosing. Matn uchun Ctrl+Shift+V yoki telefonning Joylash menyusidan foydalaning.'); }
      }
    };
    const capturePaste = event => {
      const files = [...(event.clipboardData?.files || [])];
      if (!files.length) return;
      event.preventDefault(); event.stopPropagation(); uploadFiles(files);
    };
    const terminalElement = container.current;
    terminalElement.addEventListener('paste', capturePaste, true);
    const resize = () => {
      clearTimeout(fitTimer);
      fitTimer = setTimeout(() => {
        if (disposed || !container.current?.clientWidth || !container.current?.clientHeight) return;
        try {
          fit.fit();
          if (!legacyRedrawPending) send('resize', { cols: term.cols, rows: term.rows });
        } catch { /* Container is transitioning. */ }
      }, 80);
    };
    const connect = () => {
      if (disposed || stopped) return;
      clearTimeout(retryTimer);
      setStatus(attempts ? 'reconnecting' : 'connecting');
      try { fit.fit(); } catch { /* First layout. */ }
      const current = createTerminalSocket(id, term.cols, term.rows);
      socket = current; socket.binaryType = 'arraybuffer';
      socket.onopen = () => {
        if (disposed || socket !== current) return;
        attempts = 0; term.reset(); term.options.disableStdin = id !== 'legacy'; setStatus(id === 'legacy' ? 'restoring' : 'connecting');
        if (id === 'legacy') {
          // The old shell has no screen replay. Setting the SAME PTY size does
          // not emit SIGWINCH, leaving a fresh xterm blank after reconnect.
          // Briefly change its width, then restore it to request a TUI redraw
          // without injecting keystrokes into the running program.
          clearTimeout(redrawTimer);
          legacyRedrawPending = true;
          send('resize', { cols: term.cols > 2 ? term.cols - 1 : term.cols + 1, rows: term.rows });
          redrawTimer = setTimeout(() => {
            legacyRedrawPending = false;
            if (disposed || socket !== current || current.readyState !== WebSocket.OPEN) return;
            send('resize', { cols: term.cols, rows: term.rows });
            resize();
          }, 150);
        } else resize();
      };
      socket.onmessage = (event) => {
        if (disposed || socket !== current) return;
        if (event.data instanceof ArrayBuffer) {
          // The migration bridge also receives desktop frames from the old agent.
          if (id !== 'legacy') term.write(new Uint8Array(event.data));
        }
        else {
          try {
            const msg = JSON.parse(event.data);
            if (id === 'legacy' && msg.type === 'shell_output') {
              term.write(msg.payload.data);
              term.options.disableStdin = false;
              setStatus('connected');
            }
            if (msg.type === 'ready') { term.options.disableStdin = false; setStatus('connected'); }
            if (msg.type === 'stopped') { stopped = true; term.options.disableStdin = true; setStatus('stopped'); }
            if (msg.type === 'error') setNotice(msg.message);
          } catch { /* Ignore unrelated messages from legacy bridge. */ }
        }
      };
      socket.onclose = (event) => {
        if (disposed || socket !== current) return;
        term.options.disableStdin = true;
        if (event.code === 4004 || stopped) { stopped = true; setStatus('stopped'); return; }
        setStatus('reconnecting');
        retryTimer = setTimeout(connect, Math.min(1000 * 1.5 ** attempts++, 10000));
      };
      socket.onerror = () => current.close();
    };
    term.onData(data => send('input', { data }));
    term.onResize(({ cols, rows }) => { if (!legacyRedrawPending) send('resize', { cols, rows }); });
    const observer = new ResizeObserver(resize); observer.observe(container.current);
    const resume = () => {
      if (!document.hidden && !disposed && !stopped) {
        resize();
        if (!socket || socket.readyState >= WebSocket.CLOSING) connect();
      }
    };
    window.addEventListener('online', resume);
    document.addEventListener('visibilitychange', resume);
    window.visualViewport?.addEventListener('resize', resize);
    controls.current = {
      key: data => { send('input', { data }); term.focus(); },
      keyboard: () => { if (document.activeElement === term.textarea) term.blur(); else term.focus(); },
      font: size => { term.options.fontSize = size; resize(); },
      reconnect: () => { if (!stopped) { if (socket) { socket.onclose = null; socket.close(); } connect(); } },
      paste: pasteClipboard,
      uploadFiles,
      copy: async () => { try { await navigator.clipboard.writeText(term.getSelection()); setNotice('Tanlangan matn nusxalandi.'); } catch { setNotice('Matnni tanlab, browserning nusxalash menyusidan foydalaning.'); } },
      download: () => {
        const buffer = term.buffer.active;
        const lines = [];
        for (let i = 0; i < buffer.length; i++) lines.push(buffer.getLine(i)?.translateToString(true) || '');
        const url = URL.createObjectURL(new Blob([lines.join('\n')], { type: 'text/plain;charset=utf-8' }));
        const a = document.createElement('a'); a.href = url; a.download = `terminal-${id}.txt`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
      },
    };
    connect();
    return () => {
      disposed = true; clearTimeout(retryTimer); clearTimeout(fitTimer); clearTimeout(redrawTimer); observer.disconnect();
      window.removeEventListener('online', resume); document.removeEventListener('visibilitychange', resume);
      window.visualViewport?.removeEventListener('resize', resize);
      terminalElement.removeEventListener('paste', capturePaste, true);
      if (socket) { socket.onclose = null; socket.close(); }
      controls.current = {}; term.dispose();
    };
  }, [id]);

  const changeFont = (delta) => {
    const size = Math.min(22, Math.max(10, fontSize + delta));
    fontRef.current = size; setFontSize(size); localStorage.setItem('terminal_font_size', size); controls.current.font?.(size);
  };
  // A quiet legacy job may not repaint on resize. Keep its keyboard available
  // so the user can interact or interrupt it while waiting for screen output.
  const canInput = status === 'connected' || status === 'restoring';
  return <div className={`terminal-shell${dragging ? ' terminal-dragging' : ''}`} onDragOver={e => {
    if ([...e.dataTransfer.types].includes('Files')) { e.preventDefault(); e.dataTransfer.dropEffect = canInput && !uploading ? 'copy' : 'none'; setDragging(true); }
  }} onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget)) setDragging(false); }} onDrop={e => {
    if (!e.dataTransfer.files.length) return;
    e.preventDefault(); setDragging(false); controls.current.uploadFiles?.(e.dataTransfer.files);
  }}>
    <div className="terminal-toolbar">
      <span className={`terminal-connection ${status}`} role="status" title={session.recoveredAt ? recoveryDetails : undefined}><i /><span>{status === 'connected' ? 'Ulangan' : status === 'stopped' ? 'Saqlangan tarix' : status === 'restoring' ? 'Ekran tiklanmoqda…' : status === 'reconnecting' ? 'Qayta ulanmoqda…' : 'Ulanmoqda…'}</span></span>
      <span className="terminal-current-path" title={session.cwd || session.title}>{session.cwd || session.title}</span>
      <div className="terminal-tools">
        {status !== 'stopped' && <><button onClick={() => controls.current.paste?.()} disabled={!canInput || uploading}>Joylash</button>
          <button onClick={() => fileInput.current?.click()} disabled={!canInput || uploading} aria-label="Rasm yoki fayl biriktirish" title="Rasm yoki fayl biriktirish (25 MB gacha)">{uploading ? '…' : '📎'}</button>
          <button onClick={() => setShowKeys(v => !v)} aria-expanded={showKeys} aria-label="Terminal tugmalarini ko‘rsatish">⌨</button>
          <button onClick={() => controls.current.reconnect?.()} aria-label="Qayta ulanish">↻</button></>}
        {status === 'stopped' && id !== 'legacy' && <button className="terminal-restart" disabled={restarting} onClick={onRestart} title="Saqlangan tarix bilan yangi terminal ochish">{restarting ? 'Ochilmoqda…' : '↻ Qayta ochish'}</button>}
        <details className="terminal-menu" onKeyDown={e => { if (e.key === 'Escape') { e.currentTarget.open = false; e.currentTarget.querySelector('summary').focus(); } }}>
          <summary aria-label="Terminal amallari" title="Terminal amallari">•••</summary>
          <div className="terminal-menu-content">
            <div className="terminal-font-controls"><button onClick={() => changeFont(-1)} aria-label="Shriftni kichraytirish">A−</button><span>{fontSize}px</span><button onClick={() => changeFont(1)} aria-label="Shriftni kattalashtirish">A+</button></div>
            <button onClick={() => controls.current.copy?.()}>Nusxa olish</button>
            <button onClick={() => controls.current.download?.()}>↓ Tarixni yuklash</button>
            {session.recoveredAt && <button title={recoveryDetails} onClick={e => { e.currentTarget.closest('details').open = false; setNotice(''); setRecoveryDismissed(false); }}>Tiklanish haqida</button>}
            {session.resumeCommand && <button title={session.resumeCommand} onClick={e => { e.currentTarget.closest('details').open = false; copyResumeCommand(); }}>Codex davom ettirish buyrug‘i</button>}
            {session.id !== 'legacy' && <><button onClick={e => { e.currentTarget.closest('details').open = false; onRename?.(); }} aria-label="Terminal nomini o‘zgartirish">Nomlash</button><button className="terminal-danger" onClick={e => { e.currentTarget.closest('details').open = false; onRemove?.(); }} aria-label="Terminalni tugatish">Tugatish</button></>}
          </div>
        </details>
      </div>
    </div>
    <input ref={fileInput} className="terminal-file-input" type="file" multiple tabIndex={-1} aria-label="Terminalga fayl yuklash" onChange={e => { controls.current.uploadFiles?.(e.target.files); e.target.value = ''; }} />
    {(notice || showRecovery) && <div className={`terminal-notice${!notice && showRecovery ? ' terminal-recovery-notice' : ''}`} role="status"><span>{notice || recoveryText}</span><button onClick={dismissNotice} aria-label="Xabarni yopish">×</button></div>}
    <div className="rd-terminal-content" ref={container} aria-label={session.title} />
    {showKeys && status !== 'stopped' && <div className="terminal-keyboard" aria-label="Terminal tugmalari">
      <button onClick={() => controls.current.keyboard?.()} aria-label="Klaviaturani ochish yoki yopish">⌨</button>
      {KEYS.map(([label, data]) => <button key={label} disabled={!canInput} onPointerDown={e => e.preventDefault()} onClick={() => controls.current.key?.(data)}>{label}</button>)}
    </div>}
  </div>;
}
