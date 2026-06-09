import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { isAuthenticated } from './utils/api';
import LoginPage from './pages/LoginPage';
import SessionPage from './pages/SessionPage';

// Protected Route Wrapper
function ProtectedRoute({ children }) {
  if (!isAuthenticated()) {
    return <Navigate to="/login" replace />;
  }
  return children;
}

function App() {
  return (
    <Router>
      <Routes>
        <Route path="/login" element={<LoginPage />} />

        {/* Dashboard — har bir sidebar bo'limi alohida URL (/, /screen, /terminal, /files, /settings) */}
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <SessionPage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/:panel"
          element={
            <ProtectedRoute>
              <SessionPage />
            </ProtectedRoute>
          }
        />

        {/* Catch all */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Router>
  );
}

export default App;
