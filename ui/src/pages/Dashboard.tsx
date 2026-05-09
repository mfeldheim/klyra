import { useEffect, useState } from 'react'
import { api, type AlarmState, type HistoryEvent, type ConfigResponse } from '../api/client'
import { AlarmCard } from '../components/AlarmCard'
import { Timeline } from '../components/Timeline'

export function Dashboard() {
  const [alarms, setAlarms] = useState<Record<string, AlarmState>>({})
  const [history, setHistory] = useState<HistoryEvent[]>([])
  const [cfg, setCfg] = useState<ConfigResponse | null>(null)

  useEffect(() => {
    const load = () => {
      api.status().then(r => setAlarms(r.alarms)).catch(() => {})
      api.history().then(setHistory).catch(() => {})
      api.config().then(setCfg).catch(() => {})
    }
    load()
    const id = setInterval(load, 30000)
    return () => clearInterval(id)
  }, [])

  const typeMap: Record<string, string> = {}
  cfg?.monitors.forEach(m => { typeMap[m.name] = m.type })

  const all = Object.values(alarms)
  const firing = all.filter(a => a.status === 'FIRING')
  const ok = all.filter(a => a.status === 'OK')
  const unknown = all.filter(a => a.status === 'UNKNOWN')

  return (
    <div className="main">
      <div className="summary">
        <div className={`summary-card${firing.length ? ' firing' : ''}`}>
          <div className={`summary-num ${firing.length ? 'red' : 'gray'}`}>{firing.length}</div>
          <div className="summary-label">Firing</div>
        </div>
        <div className="summary-card">
          <div className="summary-num green">{ok.length}</div>
          <div className="summary-label">OK</div>
        </div>
        <div className="summary-card">
          <div className="summary-num gray">{unknown.length}</div>
          <div className="summary-label">Unknown</div>
        </div>
        <div className="summary-card">
          <div className="summary-num gray">{all.length}</div>
          <div className="summary-label">Total</div>
        </div>
      </div>

      {all.length > 0 && (
        <div className="block" style={{ marginBottom: 20 }}>
          <h3>24h overview</h3>
          {all.map(a => (
            <Timeline key={a.monitorName} monitorName={a.monitorName} events={history} currentStatus={a.status} />
          ))}
        </div>
      )}

      {firing.length > 0 && (
        <div style={{ marginBottom: 20 }}>
          <div className="group-header">🔴 Firing ({firing.length})</div>
          {firing.map(a => <AlarmCard key={a.monitorName} alarm={a} monitorType={typeMap[a.monitorName]} />)}
        </div>
      )}

      {ok.length > 0 && (
        <div>
          <div className="group-header">✅ OK ({ok.length})</div>
          {ok.map(a => <AlarmCard key={a.monitorName} alarm={a} monitorType={typeMap[a.monitorName]} />)}
        </div>
      )}
    </div>
  )
}
