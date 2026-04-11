import { useRef, useEffect, useState, useCallback } from 'react';

const QUALITY_PRESETS = {
  low:    { fps: 10, quality: 50, maxWidth: 1920, maxHeight: 1080, label: 'Low',    icon: '📶' },
  medium: { fps: 15, quality: 70, maxWidth: 2560, maxHeight: 1440, label: 'Medium', icon: '📶' },
  high:   { fps: 15, quality: 85, maxWidth: 3840, maxHeight: 2160, label: 'High',   icon: '📶' },
  ultra:  { fps: 25, quality: 95, maxWidth: 3840, maxHeight: 2160, label: 'Ultra',  icon: '🔥' },
};

export default function ScreenViewer({ ws, isConnected }) {
  const canvasRef = useRef(null);
  const containerRef = useRef(null);
  const cursorOverlayRef = useRef(null);
  const [fps, setFps] = useState(0);
  const [resolution, setResolution] = useState('');
  const [qualityPreset, setQualityPreset] = useState('high');
  const [showQualityPanel, setShowQualityPanel] = useState(false);
  const [remoteCursor, setRemoteCursor] = useState({ x: 0.5, y: 0.5, cursorType: 'default' });
  const [isFocused, setIsFocused] = useState(false);
  const [frameSize, setFrameSize] = useState(0);
  const frameCountRef = useRef(0);
  const lastFpsTimeRef = useRef(Date.now());
  const frameSizeAccum = useRef(0);
  const lastMouseSend = useRef(0);

  // Cursor type CSS mapping
  const cursorTypeMap = {
    default: 'default',
    pointer: 'pointer',
    text: 'text',
    wait: 'wait',
    crosshair: 'crosshair',
    move: 'move',
    'not-allowed': 'not-allowed',
    'col-resize': 'col-resize',
    'row-resize': 'row-resize',
    'n-resize': 'n-resize',
    's-resize': 's-resize',
    'e-resize': 'e-resize',
    'w-resize': 'w-resize',
    'ne-resize': 'ne-resize',
    'nw-resize': 'nw-resize',
    'se-resize': 'se-resize',
    'sw-resize': 'sw-resize',
    grab: 'grab',
    grabbing: 'grabbing',
  };

  // Handle incoming binary frames
  const handleFrame = useCallback((data) => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const blob = new Blob([data], { type: 'image/jpeg' });
    const url = URL.createObjectURL(blob);
    const img = new Image();

    img.onload = () => {
      const ctx = canvas.getContext('2d');
      canvas.width = img.width;
      canvas.height = img.height;
      ctx.drawImage(img, 0, 0);
      URL.revokeObjectURL(url);

      setResolution(`${img.width}×${img.height}`);

      // FPS counter
      frameCountRef.current++;
      frameSizeAccum.current += data.byteLength;
      const now = Date.now();
      if (now - lastFpsTimeRef.current >= 1000) {
        setFps(frameCountRef.current);
        setFrameSize(Math.round(frameSizeAccum.current / 1024)); // KB/s
        frameCountRef.current = 0;
        frameSizeAccum.current = 0;
        lastFpsTimeRef.current = now;
      }
    };

    img.onerror = () => URL.revokeObjectURL(url);
    img.src = url;
  }, []);

  // Handle cursor position messages
  const handleCursorPosition = useCallback((payload) => {
    setRemoteCursor({
      x: payload.x ?? 0,
      y: payload.y ?? 0,
      cursorType: payload.cursorType || 'default',
    });
  }, []);

  useEffect(() => {
    if (!ws) return;

    const handler = (event) => {
      if (event.data instanceof Blob) {
        event.data.arrayBuffer().then(handleFrame);
      } else if (event.data instanceof ArrayBuffer) {
        handleFrame(event.data);
      } else if (typeof event.data === 'string') {
        try {
          const msg = JSON.parse(event.data);
          if (msg.type === 'cursor_position' && msg.payload) {
            handleCursorPosition(msg.payload);
          }
        } catch (e) {
          // Not our message
        }
      }
    };

    ws.addEventListener('message', handler);
    return () => ws.removeEventListener('message', handler);
  }, [ws, handleFrame, handleCursorPosition]);

  // Mouse events with throttling
  const sendMouseEvent = useCallback((e, action) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;

    const canvas = canvasRef.current;
    if (!canvas) return;

    // Throttle mouse move to ~60fps (16ms)
    if (action === 'move') {
      const now = Date.now();
      if (now - lastMouseSend.current < 16) return;
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

    // For move events, send raw pixel deltas for 1:1 mouse feel
    if (action === 'move') {
      payload.movementX = e.movementX || 0;
      payload.movementY = e.movementY || 0;
    }

    ws.send(JSON.stringify({ type: 'mouse_event', payload }));
  }, [ws]);

  const handleMouseMove = useCallback((e) => sendMouseEvent(e, 'move'), [sendMouseEvent]);
  const handleMouseDown = useCallback((e) => {
    e.preventDefault();
    // Explicitly focus the canvas on any mousedown
    if (canvasRef.current && document.activeElement !== canvasRef.current) {
      canvasRef.current.focus();
    }
    sendMouseEvent(e, 'down');
  }, [sendMouseEvent]);
  const handleMouseUp = useCallback((e) => sendMouseEvent(e, 'up'), [sendMouseEvent]);
  const handleClick = useCallback((e) => {
    // Ensure focus on click too
    if (canvasRef.current) canvasRef.current.focus();
    sendMouseEvent(e, 'click');
  }, [sendMouseEvent]);
  const handleDblClick = useCallback((e) => sendMouseEvent(e, 'dblclick'), [sendMouseEvent]);
  const handleContextMenu = useCallback((e) => { e.preventDefault(); sendMouseEvent(e, 'click'); }, [sendMouseEvent]);

  // Scroll with throttle (50ms = max 20 events/sec) and deltaY normalization
  const lastScrollSend = useRef(0);
  const handleWheel = useCallback((e) => {
    e.preventDefault();
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const canvas = canvasRef.current;
    if (!canvas) return;

    // Throttle scroll events
    const now = Date.now();
    if (now - lastScrollSend.current < 50) return;
    lastScrollSend.current = now;

    const rect = canvas.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;

    // Normalize deltaY: clamp to small range regardless of browser/OS
    // Most browsers give 100-150 per tick, smooth scrolling gives 1-30
    let normalizedDelta = e.deltaY;
    if (Math.abs(normalizedDelta) > 50) {
      normalizedDelta = normalizedDelta > 0 ? 120 : -120; // normalize to 1 click
    }

    ws.send(JSON.stringify({
      type: 'mouse_event',
      payload: {
        x: Math.max(0, Math.min(1, x)),
        y: Math.max(0, Math.min(1, y)),
        button: 'left',
        action: 'scroll',
        deltaY: normalizedDelta,
      }
    }));
  }, [ws]);

  // Focus management for canvas
  const handleCanvasFocus = useCallback(() => setIsFocused(true), []);
  const handleCanvasBlur = useCallback(() => setIsFocused(false), []);

  // Keyboard events — only when canvas is focused
  useEffect(() => {
    if (!ws || !isConnected) return;

    const handleKeyDown = (e) => {
      if (!isFocused) return;
      // Don't prevent F11 (fullscreen) or browser dev tools
      if (e.key === 'F11' || (e.ctrlKey && e.shiftKey && e.key === 'I')) return;

      e.preventDefault();
      ws.send(JSON.stringify({
        type: 'keyboard_event',
        payload: {
          key: e.key,
          code: e.code,
          action: 'keydown',
          ctrl: e.ctrlKey,
          alt: e.altKey,
          shift: e.shiftKey,
          meta: e.metaKey,
        }
      }));
    };

    const handleKeyUp = (e) => {
      if (!isFocused) return;
      e.preventDefault();
      ws.send(JSON.stringify({
        type: 'keyboard_event',
        payload: {
          key: e.key,
          code: e.code,
          action: 'keyup',
          ctrl: e.ctrlKey,
          alt: e.altKey,
          shift: e.shiftKey,
          meta: e.metaKey,
        }
      }));
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [ws, isConnected, isFocused]);

  // Auto-focus canvas on mount and after fullscreen change
  useEffect(() => {
    if (isConnected && canvasRef.current) {
      canvasRef.current.focus();
    }
  }, [isConnected]);

  // Re-focus canvas on fullscreen change
  useEffect(() => {
    const onFullscreenChange = () => {
      // When entering or exiting fullscreen, re-focus the canvas
      setTimeout(() => {
        if (canvasRef.current) canvasRef.current.focus();
      }, 100);
    };
    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  // Click on the container (anywhere in screen-viewer) should focus canvas
  const handleContainerClick = useCallback(() => {
    if (canvasRef.current) canvasRef.current.focus();
  }, []);

  // Quality preset change
  const changeQuality = useCallback((preset) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const settings = QUALITY_PRESETS[preset];
    if (!settings) return;

    setQualityPreset(preset);
    ws.send(JSON.stringify({
      type: 'quality_settings',
      payload: {
        fps: settings.fps,
        quality: settings.quality,
        maxWidth: settings.maxWidth,
        maxHeight: settings.maxHeight,
      }
    }));
  }, [ws]);

  const toggleFullscreen = () => {
    const viewer = containerRef.current;
    if (!viewer) return;

    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else {
      viewer.requestFullscreen();
    }
  };

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

  return (
    <div className="screen-viewer" ref={containerRef} onClick={handleContainerClick}>
      {/* Canvas wrapper — cursor overlay is relative to this */}
      <div className="screen-canvas-wrapper" style={{ position: 'relative', display: 'inline-block', maxWidth: '100%', maxHeight: '100%' }}>
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
          tabIndex={0}
          id="screen-canvas"
        />

        {/* Remote cursor overlay — positioned relative to canvas */}
        {isFocused && (
          <div
            ref={cursorOverlayRef}
            className="remote-cursor-dot"
            style={{
              position: 'absolute',
              left: `${remoteCursor.x * 100}%`,
              top: `${remoteCursor.y * 100}%`,
              pointerEvents: 'none',
              zIndex: 5,
            }}
          >
            <svg width="20" height="20" viewBox="0 0 20 20" style={{ position: 'absolute', top: -1, left: -1 }}>
              <path
                d="M2 2 L2 16 L6.5 11.5 L11 18 L14 16.5 L9.5 10 L16 10 Z"
                fill="white"
                stroke="black"
                strokeWidth="1.5"
              />
            </svg>
          </div>
        )}
      </div>

      {/* Stats overlay */}
      <div className="screen-overlay">
        <div className="screen-info">{resolution}</div>
        <div className="screen-info">{fps} FPS</div>
        <div className="screen-info">{frameSize} KB/s</div>
        <div className={`screen-info focus-indicator ${isFocused ? 'active' : ''}`}>
          {isFocused ? '🟢 Active' : '⚪ Click to Control'}
        </div>
      </div>
    </div>
  );
}

