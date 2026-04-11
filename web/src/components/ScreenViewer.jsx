import { useRef, useEffect, useState, useCallback } from 'react';

// Cursor SVG paths by type
const CURSOR_SHAPES = {
  default: (
    <svg width="20" height="20" viewBox="0 0 20 20">
      <path d="M2 2 L2 16 L6.5 11.5 L11 18 L14 16.5 L9.5 10 L16 10 Z"
        fill="rgba(255,255,255,0.95)" stroke="rgba(0,0,0,0.85)" strokeWidth="1.5"/>
    </svg>
  ),
  text: (
    <svg width="16" height="22" viewBox="0 0 16 22">
      <line x1="8" y1="2" x2="8" y2="20" stroke="rgba(255,255,255,0.95)" strokeWidth="2.5"/>
      <line x1="8" y1="2" x2="8" y2="20" stroke="rgba(0,0,0,0.8)" strokeWidth="1.5"/>
      <line x1="4" y1="2" x2="12" y2="2" stroke="rgba(0,0,0,0.8)" strokeWidth="1.5"/>
      <line x1="4" y1="20" x2="12" y2="20" stroke="rgba(0,0,0,0.8)" strokeWidth="1.5"/>
      <line x1="4" y1="2" x2="12" y2="2" stroke="rgba(255,255,255,0.9)" strokeWidth="1"/>
      <line x1="4" y1="20" x2="12" y2="20" stroke="rgba(255,255,255,0.9)" strokeWidth="1"/>
    </svg>
  ),
  pointer: (
    <svg width="20" height="22" viewBox="0 0 20 22">
      <path d="M6 2 L6 14 L9 11 L14 18 L16 16 L11 9 L15 9 Z"
        fill="rgba(255,255,255,0.95)" stroke="rgba(0,0,0,0.85)" strokeWidth="1.3"/>
    </svg>
  ),
  crosshair: (
    <svg width="22" height="22" viewBox="0 0 22 22">
      <line x1="11" y1="1" x2="11" y2="21" stroke="rgba(0,0,0,0.7)" strokeWidth="2"/>
      <line x1="1" y1="11" x2="21" y2="11" stroke="rgba(0,0,0,0.7)" strokeWidth="2"/>
      <line x1="11" y1="1" x2="11" y2="21" stroke="rgba(255,255,255,0.9)" strokeWidth="1"/>
      <line x1="1" y1="11" x2="21" y2="11" stroke="rgba(255,255,255,0.9)" strokeWidth="1"/>
      <circle cx="11" cy="11" r="3" fill="none" stroke="rgba(255,255,255,0.8)" strokeWidth="1"/>
    </svg>
  ),
  move: (
    <svg width="22" height="22" viewBox="0 0 22 22">
      <path d="M11 1 L14 5 L12 5 L12 9 L16 9 L16 7 L20 11 L16 15 L16 13 L12 13 L12 17 L14 17 L11 21 L8 17 L10 17 L10 13 L6 13 L6 15 L2 11 L6 7 L6 9 L10 9 L10 5 L8 5 Z"
        fill="rgba(255,255,255,0.95)" stroke="rgba(0,0,0,0.8)" strokeWidth="1"/>
    </svg>
  ),
  wait: (
    <svg width="18" height="22" viewBox="0 0 18 22">
      <rect x="3" y="1" width="12" height="20" rx="2" fill="none" stroke="rgba(0,0,0,0.7)" strokeWidth="1.5"/>
      <path d="M5 4 L13 4 L9 10 Z" fill="rgba(59,130,246,0.8)" stroke="none"/>
      <path d="M5 18 L13 18 L9 12 Z" fill="rgba(255,255,255,0.5)" stroke="none"/>
    </svg>
  ),
};

export default function ScreenViewer({ ws, isConnected }) {
  const canvasRef = useRef(null);
  const containerRef = useRef(null);
  const cursorRef = useRef(null);
  const [fps, setFps] = useState(0);
  const [resolution, setResolution] = useState('');
  const [isFocused, setIsFocused] = useState(false);
  const [frameSize, setFrameSize] = useState(0);
  const [latency, setLatency] = useState(0);
  const [cursorType, setCursorType] = useState('default');
  const [audioEnabled, setAudioEnabled] = useState(false);
  const [audioAvailable, setAudioAvailable] = useState(true);
  const frameCountRef = useRef(0);
  const lastFpsTimeRef = useRef(Date.now());
  const frameSizeAccum = useRef(0);
  const lastMouseSend = useRef(0);
  const isDecodingRef = useRef(false);
  const decodeTimeoutRef = useRef(null);

  // Audio refs
  const audioCtxRef = useRef(null);
  const audioEnabledRef = useRef(false);
  const audioNextTime = useRef(0);

  // Track whether user is actively moving mouse (vs remote cursor update)
  const userMouseActive = useRef(false);
  const userMouseTimer = useRef(null);

  // ==========================================
  // FRAME HANDLER
  // ==========================================
  const handleFrame = useCallback((data) => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    // Audio check
    const view = new Uint8Array(data);
    if (view[0] === 0x41) {
      handleAudioChunk(data.slice(1));
      return;
    }

    // Drop if still decoding — but with safety timeout
    if (isDecodingRef.current) return;

    const frameTime = Date.now();
    const blob = new Blob([data], { type: 'image/jpeg' });
    isDecodingRef.current = true;

    // Safety timeout: if decode takes >500ms, force reset
    if (decodeTimeoutRef.current) clearTimeout(decodeTimeoutRef.current);
    decodeTimeoutRef.current = setTimeout(() => {
      isDecodingRef.current = false;
    }, 500);

    createImageBitmap(blob).then((bitmap) => {
      if (decodeTimeoutRef.current) clearTimeout(decodeTimeoutRef.current);

      const ctx = canvas.getContext('2d');
      if (canvas.width !== bitmap.width || canvas.height !== bitmap.height) {
        canvas.width = bitmap.width;
        canvas.height = bitmap.height;
        setResolution(`${bitmap.width}×${bitmap.height}`);
      }
      ctx.drawImage(bitmap, 0, 0);
      bitmap.close();

      setLatency(Date.now() - frameTime);

      frameCountRef.current++;
      frameSizeAccum.current += data.byteLength;
      const now = Date.now();
      if (now - lastFpsTimeRef.current >= 1000) {
        setFps(frameCountRef.current);
        setFrameSize(Math.round(frameSizeAccum.current / 1024));
        frameCountRef.current = 0;
        frameSizeAccum.current = 0;
        lastFpsTimeRef.current = now;
      }
      isDecodingRef.current = false;
    }).catch(() => {
      if (decodeTimeoutRef.current) clearTimeout(decodeTimeoutRef.current);
      isDecodingRef.current = false;
    });
  }, []);

  // ==========================================
  // AUDIO
  // ==========================================
  const handleAudioChunk = useCallback((pcmData) => {
    if (!audioEnabledRef.current || !audioCtxRef.current) return;
    const ctx = audioCtxRef.current;
    if (ctx.state === 'suspended') ctx.resume();

    const int16Array = new Int16Array(pcmData);
    const float32Array = new Float32Array(int16Array.length);
    for (let i = 0; i < int16Array.length; i++) {
      float32Array[i] = int16Array[i] / 32768.0;
    }

    const audioBuffer = ctx.createBuffer(1, float32Array.length, 22050);
    audioBuffer.getChannelData(0).set(float32Array);
    const source = ctx.createBufferSource();
    source.buffer = audioBuffer;
    source.connect(ctx.destination);

    const startTime = Math.max(ctx.currentTime, audioNextTime.current);
    source.start(startTime);
    audioNextTime.current = startTime + audioBuffer.duration;
  }, []);

  const toggleAudio = useCallback(() => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    if (audioEnabled) {
      ws.send(JSON.stringify({ type: 'audio_stop' }));
      audioEnabledRef.current = false;
      setAudioEnabled(false);
      if (audioCtxRef.current) { audioCtxRef.current.close(); audioCtxRef.current = null; }
    } else {
      try {
        audioCtxRef.current = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 22050, latencyHint: 'interactive' });
        audioNextTime.current = 0;
        audioEnabledRef.current = true;
        setAudioEnabled(true);
        ws.send(JSON.stringify({ type: 'audio_start' }));
      } catch (e) { setAudioAvailable(false); }
    }
  }, [ws, audioEnabled]);

  useEffect(() => {
    return () => {
      if (audioCtxRef.current) { audioCtxRef.current.close(); audioCtxRef.current = null; }
      audioEnabledRef.current = false;
      if (decodeTimeoutRef.current) clearTimeout(decodeTimeoutRef.current);
    };
  }, []);

  // ==========================================
  // SERVER CURSOR SYNC — only update cursor TYPE from server (text, pointer, etc.)
  // Position is always controlled locally for instant response
  // ==========================================
  const handleCursorFromServer = useCallback((payload) => {
    // Update cursor type from server always
    if (payload.cursorType) {
      setCursorType(payload.cursorType);
    }
    // NO position override from server — local cursor is always authoritative
    // This prevents the cursor from snapping back to stale server positions
  }, []);

  // ==========================================
  // WEBSOCKET MESSAGES
  // ==========================================
  useEffect(() => {
    if (!ws) return;
    const handler = (event) => {
      if (event.data instanceof ArrayBuffer) {
        handleFrame(event.data);
      } else if (event.data instanceof Blob) {
        event.data.arrayBuffer().then(handleFrame);
      } else if (typeof event.data === 'string') {
        try {
          const msg = JSON.parse(event.data);
          if (msg.type === 'cursor_position' && msg.payload) {
            handleCursorFromServer(msg.payload);
          }
        } catch (e) {}
      }
    };
    ws.addEventListener('message', handler);
    return () => ws.removeEventListener('message', handler);
  }, [ws, handleFrame, handleCursorFromServer]);

  // ==========================================
  // LOCAL CURSOR — instant position update
  // ==========================================
  const updateLocalCursor = useCallback((e) => {
    const canvas = canvasRef.current;
    const cursor = cursorRef.current;
    if (!canvas || !cursor) return;

    const rect = canvas.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const py = e.clientY - rect.top;

    // GPU-accelerated transform — no layout, no transitions
    cursor.style.transform = `translate3d(${px}px, ${py}px, 0)`;

    // Mark user as actively moving mouse (suppress server cursor updates for position)
    userMouseActive.current = true;
    if (userMouseTimer.current) clearTimeout(userMouseTimer.current);
    userMouseTimer.current = setTimeout(() => {
      userMouseActive.current = false;
    }, 300); // After 300ms of no mouse, re-enable server position sync
  }, []);

  // ==========================================
  // MOUSE EVENTS
  // ==========================================
  const sendMouseEvent = useCallback((e, action) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const canvas = canvasRef.current;
    if (!canvas) return;

    if (action === 'move') {
      const now = Date.now();
      if (now - lastMouseSend.current < 8) return;
      lastMouseSend.current = now;
    }

    const rect = canvas.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;
    const buttonMap = { 0: 'left', 1: 'middle', 2: 'right' };

    const payload = {
      x: Math.max(0, Math.min(1, x)),
      y: Math.max(0, Math.min(1, y)),
      button: buttonMap[e.button] || 'left',
      action,
      deltaY: e.deltaY || 0,
    };

    ws.send(JSON.stringify({ type: 'mouse_event', payload }));
  }, [ws]);

  const handleMouseMove = useCallback((e) => {
    updateLocalCursor(e);
    sendMouseEvent(e, 'move');
  }, [updateLocalCursor, sendMouseEvent]);

  const handleMouseDown = useCallback((e) => {
    e.preventDefault();
    if (canvasRef.current && document.activeElement !== canvasRef.current) canvasRef.current.focus();
    updateLocalCursor(e);
    sendMouseEvent(e, 'down');
  }, [updateLocalCursor, sendMouseEvent]);

  const handleMouseUp = useCallback((e) => {
    updateLocalCursor(e);
    sendMouseEvent(e, 'up');
  }, [updateLocalCursor, sendMouseEvent]);

  const handleClick = useCallback((e) => {
    if (canvasRef.current) canvasRef.current.focus();
    updateLocalCursor(e);
    sendMouseEvent(e, 'click');
  }, [updateLocalCursor, sendMouseEvent]);

  const handleDblClick = useCallback((e) => {
    updateLocalCursor(e);
    sendMouseEvent(e, 'dblclick');
  }, [updateLocalCursor, sendMouseEvent]);

  const handleContextMenu = useCallback((e) => {
    e.preventDefault();
    updateLocalCursor(e);
    sendMouseEvent(e, 'click');
  }, [updateLocalCursor, sendMouseEvent]);

  const lastScrollSend = useRef(0);
  const handleWheel = useCallback((e) => {
    e.preventDefault();
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const now = Date.now();
    if (now - lastScrollSend.current < 50) return;
    lastScrollSend.current = now;
    const rect = canvas.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;
    let normalizedDelta = e.deltaY;
    if (Math.abs(normalizedDelta) > 50) normalizedDelta = normalizedDelta > 0 ? 120 : -120;
    ws.send(JSON.stringify({
      type: 'mouse_event',
      payload: { x: Math.max(0, Math.min(1, x)), y: Math.max(0, Math.min(1, y)), button: 'left', action: 'scroll', deltaY: normalizedDelta }
    }));
  }, [ws]);

  const handleCanvasFocus = useCallback(() => setIsFocused(true), []);
  const handleCanvasBlur = useCallback(() => setIsFocused(false), []);

  // Keyboard
  useEffect(() => {
    if (!ws || !isConnected) return;
    const handleKeyDown = (e) => {
      if (!isFocused) return;
      if (e.key === 'F11' || (e.ctrlKey && e.shiftKey && e.key === 'I')) return;
      e.preventDefault();
      e.stopPropagation();
      const key = e.key;
      const code = e.code;
      ws.send(JSON.stringify({ type: 'keyboard_event', payload: { key, code, action: 'keydown', ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, meta: e.metaKey } }));
    };
    const handleKeyUp = (e) => {
      if (!isFocused) return;
      e.preventDefault();
      e.stopPropagation();
      ws.send(JSON.stringify({ type: 'keyboard_event', payload: { key: e.key, code: e.code, action: 'keyup', ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, meta: e.metaKey } }));
    };
    window.addEventListener('keydown', handleKeyDown, true);
    window.addEventListener('keyup', handleKeyUp, true);
    return () => { window.removeEventListener('keydown', handleKeyDown, true); window.removeEventListener('keyup', handleKeyUp, true); };
  }, [ws, isConnected, isFocused]);

  useEffect(() => { if (isConnected && canvasRef.current) canvasRef.current.focus(); }, [isConnected]);
  useEffect(() => {
    const fn = () => { setTimeout(() => { if (canvasRef.current) canvasRef.current.focus(); }, 100); };
    document.addEventListener('fullscreenchange', fn);
    return () => document.removeEventListener('fullscreenchange', fn);
  }, []);

  const handleContainerClick = useCallback(() => { if (canvasRef.current) canvasRef.current.focus(); }, []);
  const handleMouseEnter = useCallback((e) => { updateLocalCursor(e); }, [updateLocalCursor]);

  if (!isConnected) {
    return (
      <div className="screen-viewer">
        <div className="screen-connecting">
          <div className="screen-connecting-spinner" />
          <span>Connecting to remote screen...</span>
        </div>
      </div>
    );
  }

  // Pick cursor shape based on cursorType from server
  const cursorSVG = CURSOR_SHAPES[cursorType] || CURSOR_SHAPES.default;

  return (
    <div className="screen-viewer" ref={containerRef} onClick={handleContainerClick}>
      <div className="screen-canvas-wrapper">
        <canvas
          ref={canvasRef}
          className={`screen-canvas ${isFocused ? 'focused' : ''}`}
          onMouseMove={handleMouseMove}
          onMouseDown={handleMouseDown}
          onMouseUp={handleMouseUp}
          onClick={handleClick}
          onDoubleClick={handleDblClick}
          onContextMenu={handleContextMenu}
          onWheel={handleWheel}
          onFocus={handleCanvasFocus}
          onBlur={handleCanvasBlur}
          onMouseEnter={handleMouseEnter}
          tabIndex={0}
          id="screen-canvas"
        />

        {/* Cursor overlay — local position (instant) + server cursor type */}
        {isFocused && (
          <div ref={cursorRef} className="local-cursor">
            {cursorSVG}
          </div>
        )}
      </div>

      {/* Stats overlay */}
      <div className="screen-overlay">
        <div className="screen-info">{resolution}</div>
        <div className="screen-info">{fps} FPS</div>
        <div className="screen-info">{frameSize} KB/s</div>
        <div className="screen-info">{latency}ms</div>
        {audioAvailable && (
          <div
            className={`screen-info screen-audio-btn ${audioEnabled ? 'active' : ''}`}
            onClick={(e) => { e.stopPropagation(); toggleAudio(); }}
            title={audioEnabled ? 'Click to mute audio' : 'Click to enable audio'}
          >
            {audioEnabled ? '🔊 Audio ON' : '🔇 Audio'}
          </div>
        )}

        <div className={`screen-info focus-indicator ${isFocused ? 'active' : ''}`}>
          {isFocused ? '🟢 Active' : '⚪ Click to Control'}
        </div>
      </div>
    </div>
  );
}
