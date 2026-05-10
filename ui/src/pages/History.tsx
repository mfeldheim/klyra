import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type HistoryEvent } from '../api/client'

export function History() {
  const navigate = useNavigate()
  const [events, setEvents] = useState<HistoryEvent[]>([])
  const [filter, setFilter] = useState('')

  useEffect(() => {
    api.history().then(e => setEvents([...e].reverse())).catch(() => {})
  }, [])

  const filtered = filter ? events.filter(e => e.monitorName.includes(filter)) : events

  return (
    <div className="main">
      <div className="block">
        <h3>24h Event History</h3>
        <div className="form-row" style={{ marginBottom: 12 }}>
          <input placeholder="Filter by monitor name…" value={filter} onChange={e => setFilter(e.target.value)} style={{ width: 260 }} />
        </div>
        <table>
          <thead><tr><th>Time</th><th>Monitor</th><th>Transition</th><th>Message</th><th>Incident</th></tr></thead>
          <tbody>
            {filtered.map((ev, i) => (
              <tr key={i}>
                <td>{new Date(ev.at).toLocaleString()}</td>
                <td>{ev.monitorName}</td>
                <td style={{ color: ev.transition === 'FIRING' ? '#f44336' : '#4caf50' }}>{ev.transition}</td>
                <td style={{ color: '#8b949e' }}>{ev.message || '—'}</td>
                <td>
                  {ev.incidentId
                    ? <span className="incident-link" onClick={() => navigate(`/incidents/${ev.incidentId}`)}>{ev.incidentId.slice(-12)}</span>
                    : <span style={{ color: '#484f58' }}>—</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {filtered.length === 0 && <p style={{ color: '#8b949e', padding: '12px 8px' }}>No events.</p>}
      </div>
    </div>
  )
}
