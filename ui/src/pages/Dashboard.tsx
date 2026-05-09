import { useEffect, useState } from 'react'
import { api, type AlarmState, type HistoryEvent, type ConfigResponse } from '../api/client'
import { AlarmCard } from '../components/AlarmCard'
import { Timeline } from '../components/Timeline'

export function Dashboard() {
  const [alarms, setAlarms] = useState<Record<string, AlarmState>>({})
  const [history, setHistory] = useState<HistoryEvent[]>([])
  const [cfg, setCfg] = useState<ConfigResponse | null>(null)
  const [activeTypes, setActiveTypes] = useState<Set<string>>(new Set())
  const [activeStatuses, setActiveStatuses] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<string | null>(null)

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
  cfg?.monitors?.forEach(m => { typeMap[m.name] = m.type })

  const all = Object.values(alarms)
  const firing = all.filter(a => a.status === 'FIRING')
  const ok = all.filter(a => a.status === 'OK')
  const unknown = all.filter(a => a.status === 'UNKNOWN')

  // Collect distinct types present in alarms
  const typeCounts: Record<string, number> = {}
  all.forEach(a => {
    const t = typeMap[a.monitorName]
    if (t) typeCounts[t] = (typeCounts[t] ?? 0) + 1
  })

  const statusCounts: Record<string, number> = {
    FIRING: firing.length,
    OK: ok.length,
    UNKNOWN: unknown.length,
  }

  function toggleType(t: string) {
    setActiveTypes(prev => {
      const next = new Set(prev)
      next.has(t) ? next.delete(t) : next.add(t)
      return next
    })
  }

  function toggleStatus(s: string) {
    setActiveStatuses(prev => {
      const next = new Set(prev)
      next.has(s) ? next.delete(s) : next.add(s)
      return next
    })
  }

  // Apply filters: OR within group, AND across groups
  const filtered = all.filter(a => {
    const typeOk = activeTypes.size === 0 || activeTypes.has(typeMap[a.monitorName] ?? '')
    const statusOk = activeStatuses.size === 0 || activeStatuses.has(a.status)
    return typeOk && statusOk
  })

  const filteredFiring = filtered.filter(a => a.status === 'FIRING')
  const filteredOk = filtered.filter(a => a.status === 'OK')

  const hasTypeChips = Object.keys(typeCounts).length > 0

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

      {all.length > 0 && (
        <>
          {hasTypeChips && (
            <div className="filters">
              {Object.entries(typeCounts).map(([t, count]) => (
                <span
                  key={t}
                  className={`chip${activeTypes.has(t) ? ' active' : ''}`}
                  onClick={() => toggleType(t)}
                >
                  <span className="chip-dot" />
                  {t} {count}
                </span>
              ))}
            </div>
          )}
          <div className="filters">
            {(['FIRING', 'OK', 'UNKNOWN'] as const).map(s => {
              const count = statusCounts[s]
              if (count === 0) return null
              return (
                <span
                  key={s}
                  className={`chip ${s.toLowerCase()}${activeStatuses.has(s) ? ' active' : ''}`}
                  onClick={() => toggleStatus(s)}
                >
                  <span className="chip-dot" />
                  {s} {count}
                </span>
              )
            })}
          </div>
        </>
      )}

      {filteredFiring.length > 0 && (
        <div style={{ marginBottom: 20 }}>
          <div className="group-header">🔴 Firing ({filteredFiring.length})</div>
          {filteredFiring.map(a => (
            <AlarmCard
              key={a.monitorName}
              alarm={a}
              monitorType={typeMap[a.monitorName]}
              selected={selected === a.monitorName}
              onSelect={a2 => setSelected(prev => prev === a2.monitorName ? null : a2.monitorName)}
            />
          ))}
        </div>
      )}

      {filteredOk.length > 0 && (
        <div>
          <div className="group-header">✅ OK ({filteredOk.length})</div>
          {filteredOk.map(a => (
            <AlarmCard
              key={a.monitorName}
              alarm={a}
              monitorType={typeMap[a.monitorName]}
              selected={selected === a.monitorName}
              onSelect={a2 => setSelected(prev => prev === a2.monitorName ? null : a2.monitorName)}
            />
          ))}
        </div>
      )}
    </div>
  )
}
