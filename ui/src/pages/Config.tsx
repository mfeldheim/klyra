import { useEffect, useState } from 'react'
import { api, type ConfigResponse } from '../api/client'

export function Config() {
  const [cfg, setCfg] = useState<ConfigResponse | null>(null)

  useEffect(() => { api.config().then(setCfg).catch(() => {}) }, [])

  if (!cfg) return <div className="main"><p style={{ color: '#8b949e' }}>Loading…</p></div>

  const monitors = cfg.monitors ?? []
  const actions = cfg.actions ?? []

  return (
    <div className="main">
      <div className="block">
        <h3>Monitors ({monitors.length})</h3>
        <table>
          <thead><tr><th>Name</th><th>Type</th><th>Interval</th><th>Threshold</th><th>Actions</th></tr></thead>
          <tbody>
            {monitors.map(m => (
              <tr key={m.name}>
                <td>{m.name}</td>
                <td><span className={`tag ${m.type}`}>{m.type}</span></td>
                <td>{m.interval || '—'}</td>
                <td>{m.threshold.operator} {String(m.threshold.value)}{m.threshold.for ? ` for ${m.threshold.for}` : ''}</td>
                <td>{m.actions.join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="block">
        <h3>Actions ({actions.length})</h3>
        <table>
          <thead><tr><th>Name</th><th>Type</th></tr></thead>
          <tbody>
            {actions.map(a => (
              <tr key={a.name}><td>{a.name}</td><td>{a.type}</td></tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
