import { useState } from 'react'
import { Dashboard } from './pages/Dashboard'
import { History } from './pages/History'
import { Config } from './pages/Config'
import { Silences } from './pages/Silences'
import './index.css'

type Tab = 'dashboard' | 'history' | 'config' | 'silences'

export function App() {
  const [tab, setTab] = useState<Tab>('dashboard')

  return (
    <>
      <nav className="nav">
        <div className="nav-logo">kly<span>ra</span></div>
        {(['dashboard', 'history', 'config', 'silences'] as Tab[]).map(t => (
          <div key={t} className={`nav-tab${tab === t ? ' active' : ''}`} onClick={() => setTab(t)}>
            {t.charAt(0).toUpperCase() + t.slice(1)}
          </div>
        ))}
      </nav>
      {tab === 'dashboard' && <Dashboard />}
      {tab === 'history' && <History />}
      {tab === 'config' && <Config />}
      {tab === 'silences' && <Silences />}
    </>
  )
}
