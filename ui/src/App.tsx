import { useState, useEffect, useRef } from 'react'
import { BrowserRouter, Routes, Route, useNavigate, useLocation, useParams } from 'react-router-dom'
import { Dashboard } from './pages/Dashboard'
import { History } from './pages/History'
import { Config } from './pages/Config'
import { Silences } from './pages/Silences'
import { IncidentView } from './pages/Incident'
import { api } from './api/client'
import './index.css'

function UserAvatar({ user }: { user: string }) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('click', handleClick)
    return () => document.removeEventListener('click', handleClick)
  }, [open])

  if (!user) return null

  return (
    <div className="avatar-wrap" ref={wrapRef}>
      <button className="avatar-btn" onClick={() => setOpen(o => !o)}>
        {user.charAt(0).toUpperCase()}
      </button>
      {open && (
        <div className="avatar-menu">
          <div className="avatar-menu-user">{user}</div>
          <a className="avatar-menu-item" href="/oauth2/sign_out">Sign out</a>
        </div>
      )}
    </div>
  )
}

const NAV_TABS = [
  { path: '/', label: 'Dashboard' },
  { path: '/history', label: 'History' },
  { path: '/config', label: 'Config' },
  { path: '/silences', label: 'Silences' },
]

function AppInner() {
  const [user, setUser] = useState('')
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    api.me().then(r => setUser(r.user)).catch(() => {})
  }, [])

  return (
    <>
      <nav className="nav">
        <div className="nav-logo">kly<span>ra</span></div>
        {NAV_TABS.map(t => (
          <div
            key={t.path}
            className={`nav-tab${location.pathname === t.path ? ' active' : ''}`}
            onClick={() => navigate(t.path)}
          >
            {t.label}
          </div>
        ))}
        <div className="nav-right">
          <UserAvatar user={user} />
        </div>
      </nav>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/history" element={<History />} />
        <Route path="/config" element={<Config />} />
        <Route path="/silences" element={<Silences />} />
        <Route path="/incidents/:id" element={<IncidentRoute />} />
      </Routes>
    </>
  )
}

function IncidentRoute() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  return <IncidentView incidentId={id!} onBack={() => navigate(-1)} />
}

export function App() {
  return (
    <BrowserRouter>
      <AppInner />
    </BrowserRouter>
  )
}
