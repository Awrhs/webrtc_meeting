import { Routes, Route } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import RoomList from './pages/RoomList'
import MeetingRoom from './pages/MeetingRoom'
import Profile from './pages/Profile'
import ProtectedRoute from './components/ProtectedRoute'
import ErrorBoundary from './components/ErrorBoundary'

function App() {
  return (
    <ErrorBoundary>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <RoomList />
            </ProtectedRoute>
          }
        />
        <Route
          path="/meeting/:id"
          element={
            <ProtectedRoute>
              <MeetingRoom />
            </ProtectedRoute>
          }
        />
        <Route
          path="/profile"
          element={
            <ProtectedRoute>
              <Profile />
            </ProtectedRoute>
          }
        />
      </Routes>
    </ErrorBoundary>
  )
}

export default App
