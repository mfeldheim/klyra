import { useState, useEffect, useRef } from 'react'
import { Dashboard } from './pages/Dashboard'
import { History } from './pages/History'
import { Config } from './pages/Config'
import { Silences } from './pages/Silences'
import { api } from './api/client'
import './index.css'

type Tab = 'dashboard' | 'history' | 'config' | 'silences'

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

export function App() {
  const [tab, setTab] = useState<Tab>('dashboard')
  const [user, setUser] = useState('')

  useEffect(() => {
    api.me().then(r => setUser(r.user)).catch(() => {})
  }, [])

  return (
    <>
      <nav className="nav">
        <div className="nav-logo">kly<span>ra</span></div>
        {(['dashboard', 'history', 'config', 'silences'] as Tab[]).map(t => (
          <div key={t} className={`nav-tab${tab === t ? ' active' : ''}`} onClick={() => setTab(t)}>
            {t.charAt(0).toUpperCase() + t.slice(1)}
          </div>
        ))}
        <div className="nav-right">
          <UserAvatar user={user} />
        </div>
      </nav>
      {tab === 'dashboard' && <Dashboard />}
      {tab === 'history' && <History />}
      {tab === 'config' && <Config />}
      {tab === 'silences' && <Silences />}
    </>
  )
}
