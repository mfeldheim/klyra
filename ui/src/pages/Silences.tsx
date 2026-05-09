import { useEffect, useState } from 'react'
import { api, type Silence } from '../api/client'

export function Silences() {
  const [silences, setSilences] = useState<Silence[]>([])
  const [monitor, setMonitor] = useState('')
  const [duration, setDuration] = useState('1h')
  const [reason, setReason] = useState('')
  const [err, setErr] = useState('')

  useEffect(() => {
    api.silences().then(setSilences).catch(() => {})
  }, [])

  const active = silences.filter(s => new Date(s.until) > new Date())

  const create = async () => {
    setErr('')
    try {
      await api.createSilence(monitor, duration, reason)
      setMonitor(''); setDuration('1h'); setReason('')
      api.silences().then(setSilences).catch(() => {})
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const remove = async (id: string) => {
    await api.deleteSilence(id).catch(() => {})
    api.silences().then(setSilences).catch(() => {})
  }

  return (
    <div className="main">
      <div className="block">
        <h3>Create Silence</h3>
        <div className="form-row">
          <input placeholder="Monitor name (empty = all)" value={monitor} onChange={e => setMonitor(e.target.value)} style={{ width: 220 }} />
          <input placeholder="Duration (e.g. 1h, 30m)" value={duration} onChange={e => setDuration(e.target.value)} style={{ width: 140 }} />
          <input placeholder="Reason" value={reason} onChange={e => setReason(e.target.value)} style={{ width: 200 }} />
          <button className="btn" onClick={create}>Silence</button>
        </div>
        {err && <p style={{ color: '#f44336', fontSize: 11 }}>{err}</p>}
      </div>
      <div className="block">
        <h3>Active Silences ({active.length})</h3>
        {active.length === 0 && <p style={{ color: '#8b949e', padding: '8px 0' }}>No active silences.</p>}
        <table>
          <thead><tr><th>Monitor</th><th>Until</th><th>Reason</th><th></th></tr></thead>
          <tbody>
            {active.map(s => (
              <tr key={s.id}>
                <td>{s.monitorName || <em style={{ color: '#8b949e' }}>all</em>}</td>
                <td>{new Date(s.until).toLocaleString()}</td>
                <td style={{ color: '#8b949e' }}>{s.reason || '—'}</td>
                <td><button className="btn danger" onClick={() => remove(s.id)}>Remove</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
