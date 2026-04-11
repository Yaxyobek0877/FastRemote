import { useRef, useEffect, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

export default function TerminalPanel({ ws, isConnected, sessionId = 1, isVisible = true }) {
  const terminalRef = useRef(null);
  const termRef = useRef(null);
  const fitAddonRef = useRef(null);

  const sendShellInput = useCallback((data) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: 'shell_input', payload: { data, sessionId: String(sessionId) } }));
  }, [ws, sessionId]);

  const sendResize = useCallback((cols, rows) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: 'shell_resize', payload: { cols, rows, sessionId: String(sessionId) } }));
  }, [ws, sessionId]);

  // Initialize terminal
  useEffect(() => {
    if (!terminalRef.current || termRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontSize: 14,
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      theme: {
        background: '#0a0e17',
        foreground: '#e2e8f0',
        cursor: '#3b82f6',
        selectionBackground: 'rgba(59, 130, 246, 0.3)',
        black: '#1e293b', red: '#ef4444', green: '#10b981', yellow: '#f59e0b',
        blue: '#3b82f6', magenta: '#8b5cf6', cyan: '#06b6d4', white: '#e2e8f0',
        brightBlack: '#64748b', brightRed: '#f87171', brightGreen: '#34d399', brightYellow: '#fbbf24',
        brightBlue: '#60a5fa', brightMagenta: '#a78bfa', brightCyan: '#22d3ee', brightWhite: '#f8fafc',
      },
      lineHeight: 1.2,
      scrollback: 10000,
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalRef.current);

    // Delay fit to ensure container has dimensions
    setTimeout(() => {
      try {
        fitAddon.fit();
        sendResize(term.cols, term.rows);
      } catch(e) {}
    }, 150);

    term.onData(sendShellInput);
    term.onResize(({ cols, rows }) => sendResize(cols, rows));

    termRef.current = term;
    fitAddonRef.current = fitAddon;

    const resizeObserver = new ResizeObserver(() => {
      try { fitAddon.fit(); } catch(e) {}
    });
    resizeObserver.observe(terminalRef.current);

    return () => {
      resizeObserver.disconnect();
      term.dispose();
      termRef.current = null;
      fitAddonRef.current = null;
    };
  }, [sendShellInput, sendResize]);

  // Refit when tab becomes visible
  useEffect(() => {
    if (isVisible && fitAddonRef.current) {
      setTimeout(() => {
        try { fitAddonRef.current.fit(); } catch(e) {}
      }, 50);
    }
  }, [isVisible]);

  // Handle shell output
  useEffect(() => {
    if (!ws || !termRef.current) return;
    const handler = (event) => {
      if (typeof event.data !== 'string') return;
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === 'shell_output' && msg.payload?.data) {
          // Accept output for this session or for the default session
          const msgSid = msg.payload.sessionId || '1';
          if (msgSid === String(sessionId) || msgSid === '1') {
            termRef.current.write(msg.payload.data);
          }
        }
      } catch (e) {}
    };
    ws.addEventListener('message', handler);
    return () => ws.removeEventListener('message', handler);
  }, [ws, sessionId]);

  return <div className="rd-terminal-content" ref={terminalRef} id={`terminal-${sessionId}`} />;
}
