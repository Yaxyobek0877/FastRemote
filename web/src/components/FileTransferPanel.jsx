import { useState, useEffect, useCallback, useRef } from 'react';
import { formatBytes, formatDate } from '../utils/api';

export default function FileTransferPanel({ ws, isConnected }) {
  const [currentPath, setCurrentPath] = useState('~');
  const [entries, setEntries] = useState([]);
  const [loading, setLoading] = useState(false);
  const [pathInput, setPathInput] = useState('~');
  const [downloadProgress, setDownloadProgress] = useState(null);
  const [dragging, setDragging] = useState(false);
  const fileInputRef = useRef(null);
  const downloadChunksRef = useRef({});

  // Request directory listing
  const listDirectory = useCallback((path) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    setLoading(true);
    ws.send(JSON.stringify({
      type: 'file_list',
      payload: { path }
    }));
  }, [ws]);

  // Initial load
  useEffect(() => {
    if (isConnected) {
      listDirectory(currentPath);
    }
  }, [isConnected, listDirectory]);

  // Handle incoming messages
  useEffect(() => {
    if (!ws) return;

    const handler = (event) => {
      if (typeof event.data !== 'string') return;

      try {
        const msg = JSON.parse(event.data);

        if (msg.type === 'file_list_response' && msg.payload) {
          setLoading(false);
          if (msg.payload.error) {
            console.error('File list error:', msg.payload.error);
            return;
          }
          setCurrentPath(msg.payload.path);
          setPathInput(msg.payload.path);

          // Sort: directories first, then alphabetically
          const sorted = [...(msg.payload.entries || [])].sort((a, b) => {
            if (a.isDir && !b.isDir) return -1;
            if (!a.isDir && b.isDir) return 1;
            return a.name.localeCompare(b.name);
          });
          setEntries(sorted);
        }

        if (msg.type === 'file_data' && msg.payload) {
          handleFileData(msg.payload);
        }

        if (msg.type === 'file_upload_status' && msg.payload) {
          if (msg.payload.success) {
            // Refresh current directory
            listDirectory(currentPath);
          } else if (msg.payload.error) {
            alert('Upload error: ' + msg.payload.error);
          }
        }
      } catch (e) {
        // Not JSON
      }
    };

    ws.addEventListener('message', handler);
    return () => ws.removeEventListener('message', handler);
  }, [ws, currentPath, listDirectory]);

  const handleFileData = (payload) => {
    const key = payload.path;

    if (payload.error) {
      setDownloadProgress(null);
      alert('Download error: ' + payload.error);
      return;
    }

    // Accumulate chunks
    if (!downloadChunksRef.current[key]) {
      downloadChunksRef.current[key] = [];
    }

    // Decode base64 chunk
    const binaryStr = atob(payload.data);
    const bytes = new Uint8Array(binaryStr.length);
    for (let i = 0; i < binaryStr.length; i++) {
      bytes[i] = binaryStr.charCodeAt(i);
    }
    downloadChunksRef.current[key].push(bytes);

    // Update progress
    const progress = payload.total > 0 ? Math.round((payload.offset + bytes.length) / payload.total * 100) : 0;
    setDownloadProgress({
      fileName: payload.fileName,
      progress,
      total: payload.total,
    });

    if (payload.done) {
      // Combine all chunks and trigger download
      const allChunks = downloadChunksRef.current[key];
      const totalLength = allChunks.reduce((sum, chunk) => sum + chunk.length, 0);
      const combined = new Uint8Array(totalLength);
      let offset = 0;
      for (const chunk of allChunks) {
        combined.set(chunk, offset);
        offset += chunk.length;
      }

      const blob = new Blob([combined]);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = payload.fileName;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      delete downloadChunksRef.current[key];
      setTimeout(() => setDownloadProgress(null), 1500);
    }
  };

  const navigateTo = (entry) => {
    if (entry.isDir) {
      const newPath = currentPath === '/' 
        ? '/' + entry.name 
        : currentPath + '/' + entry.name;
      setCurrentPath(newPath);
      listDirectory(newPath);
    }
  };

  const navigateUp = () => {
    const parent = currentPath.substring(0, currentPath.lastIndexOf('/')) || '/';
    setCurrentPath(parent);
    listDirectory(parent);
  };

  const handlePathSubmit = (e) => {
    e.preventDefault();
    setCurrentPath(pathInput);
    listDirectory(pathInput);
  };

  const downloadFile = (entry) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const filePath = currentPath === '/' 
      ? '/' + entry.name 
      : currentPath + '/' + entry.name;

    ws.send(JSON.stringify({
      type: 'file_download',
      payload: { path: filePath }
    }));

    setDownloadProgress({
      fileName: entry.name,
      progress: 0,
      total: entry.size,
    });
  };

  const handleUpload = (files) => {
    if (!ws || ws.readyState !== WebSocket.OPEN || !files.length) return;

    const file = files[0];
    const destPath = currentPath === '/' 
      ? '/' + file.name 
      : currentPath + '/' + file.name;

    const reader = new FileReader();
    const chunkSize = 64 * 1024;

    let offset = 0;
    const readNextChunk = () => {
      const slice = file.slice(offset, offset + chunkSize);
      reader.readAsArrayBuffer(slice);
    };

    reader.onload = (e) => {
      const chunk = new Uint8Array(e.target.result);
      const base64 = btoa(String.fromCharCode(...chunk));

      const isDone = offset + chunk.length >= file.size;

      ws.send(JSON.stringify({
        type: 'file_upload_data',
        payload: {
          path: destPath,
          fileName: file.name,
          data: base64,
          offset: offset,
          total: file.size,
          done: isDone,
        }
      }));

      offset += chunk.length;
      if (!isDone) {
        readNextChunk();
      }
    };

    readNextChunk();
  };

  const getFileIcon = (entry) => {
    if (entry.isDir) return '📁';

    const ext = entry.name.split('.').pop()?.toLowerCase();
    const iconMap = {
      'txt': '📄', 'md': '📝', 'log': '📋',
      'js': '🟨', 'jsx': '⚛️', 'ts': '🔷', 'tsx': '⚛️',
      'py': '🐍', 'go': '🔵', 'rs': '🦀',
      'html': '🌐', 'css': '🎨', 'json': '📋',
      'jpg': '🖼️', 'jpeg': '🖼️', 'png': '🖼️', 'gif': '🖼️', 'svg': '🖼️',
      'mp4': '🎬', 'mp3': '🎵', 'wav': '🎵',
      'zip': '📦', 'tar': '📦', 'gz': '📦',
      'exe': '⚙️', 'sh': '⚙️', 'bat': '⚙️',
      'pdf': '📕', 'doc': '📘', 'docx': '📘',
    };

    return iconMap[ext] || '📄';
  };

  // Drag & drop handlers
  const handleDragOver = (e) => { e.preventDefault(); setDragging(true); };
  const handleDragLeave = () => setDragging(false);
  const handleDrop = (e) => {
    e.preventDefault();
    setDragging(false);
    if (e.dataTransfer.files.length) {
      handleUpload(Array.from(e.dataTransfer.files));
    }
  };

  return (
    <div className="file-panel">
      <form className="file-toolbar" onSubmit={handlePathSubmit}>
        <button
          type="button"
          className="btn btn-icon"
          onClick={navigateUp}
          title="Go up"
          id="file-up-btn"
        >
          ⬆️
        </button>
        <input
          className="file-path-input"
          value={pathInput}
          onChange={(e) => setPathInput(e.target.value)}
          placeholder="Enter path..."
          id="file-path-input"
        />
        <button type="submit" className="btn btn-secondary" id="file-go-btn">
          Go
        </button>
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => listDirectory(currentPath)}
          id="file-refresh-btn"
        >
          🔄
        </button>
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => fileInputRef.current?.click()}
          id="file-upload-btn"
        >
          📤 Upload
        </button>
        <input
          type="file"
          ref={fileInputRef}
          style={{ display: 'none' }}
          onChange={(e) => handleUpload(Array.from(e.target.files))}
        />
      </form>

      <div className="file-list">
        {loading ? (
          <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
            <div className="screen-connecting-spinner" style={{ margin: '0 auto 12px' }} />
            Loading...
          </div>
        ) : entries.length === 0 ? (
          <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
            Empty directory
          </div>
        ) : (
          entries.map((entry, i) => (
            <div
              key={`${entry.name}-${i}`}
              className="file-item"
              onClick={() => entry.isDir ? navigateTo(entry) : null}
              onDoubleClick={() => !entry.isDir ? downloadFile(entry) : null}
            >
              <span className="file-item-icon">{getFileIcon(entry)}</span>
              <span className={`file-item-name ${entry.isDir ? 'dir' : ''}`}>
                {entry.name}
              </span>
              <span className="file-item-size">
                {entry.isDir ? '--' : formatBytes(entry.size)}
              </span>
              <span className="file-item-date">
                {formatDate(entry.modTime)}
              </span>
              {!entry.isDir && (
                <div className="file-item-actions">
                  <button
                    className="btn btn-icon"
                    onClick={(e) => { e.stopPropagation(); downloadFile(entry); }}
                    title="Download"
                  >
                    ⬇️
                  </button>
                </div>
              )}
            </div>
          ))
        )}
      </div>

      <div className="file-upload-zone">
        <div
          className={`file-upload-area ${dragging ? 'dragging' : ''}`}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          onClick={() => fileInputRef.current?.click()}
        >
          📤 Drop files here or click to upload
        </div>
      </div>

      {downloadProgress && (
        <div className="file-download-progress">
          <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>
            {downloadProgress.progress >= 100 ? '✅' : '⬇️'} {downloadProgress.fileName}
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
            {downloadProgress.progress}% • {formatBytes(downloadProgress.total)}
          </div>
          <div className="progress-bar-bg">
            <div
              className="progress-bar-fill"
              style={{ width: `${downloadProgress.progress}%` }}
            />
          </div>
        </div>
      )}
    </div>
  );
}
