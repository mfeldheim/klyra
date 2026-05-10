import { useEffect, useState } from 'react'
import { api, type AlarmState, type HistoryEvent, type ConfigResponse } from '../api/client'
import { AlarmCard } from '../components/AlarmCard'
import { GroupTile } from '../components/GroupTile'
import { Timeline } from '../components/Timeline'
import { IncidentView } from './Incident'

export function Dashboard() {
  const [alarms, setAlarms] = useState<Record<string, AlarmState>>({})
  const [history, setHistory] = useState<HistoryEvent[]>([])
  const [cfg, setCfg] = useState<ConfigResponse | null>(null)
  const [openGroups, setOpenGroups] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<string | null>(null)
  const [openIncident, setOpenIncident] = useState<string | null>(null)

  useEffect(() => {
    const load = () => {
      api.status().then(r => setAlarms(r.alarms)).catch(() => {})
      api.history().then(setHistory).catch(() => {})
      api.config().then(setCfg).catch(() => {})
    }
    load()
    const id = setInterval(load, 15000)
    const onVisible = () => { if (document.visibilityState === 'visible') load() }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(id)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  const typeMap: Record<string, string> = {}
  cfg?.monitors?.forEach(m => { typeMap[m.name] = m.type })

  const all = Object.values(alarms)
  const firing = all.filter(a => a.status === 'FIRING')
  const ok = all.filter(a => a.status === 'OK')
  const unknown = all.filter(a => a.status === 'UNKNOWN')

  // Build ordered group list from config order, ungrouped last.
  const groupOrder: string[] = []
  cfg?.monitors?.forEach(m => {
    const g = m.group ?? ''
    if (!groupOrder.includes(g)) groupOrder.push(g)
  })
  // Also catch any groups that appear in alarms but not config (edge case).
  all.forEach(a => {
    const g = a.group ?? ''
    if (!groupOrder.includes(g)) groupOrder.push(g)
  })
  // Move '' (ungrouped) to the end.
  const ungroupedIdx = groupOrder.indexOf('')
  if (ungroupedIdx !== -1) {
    groupOrder.splice(ungroupedIdx, 1)
    groupOrder.push('')
  }

  // Bucket alarms by group, firing-first within each group.
  const grouped: Record<string, AlarmState[]> = {}
  all.forEach(a => {
    const g = a.group ?? ''
    if (!grouped[g]) grouped[g] = []
    grouped[g].push(a)
  })
  const statusOrder = { FIRING: 0, UNKNOWN: 1, OK: 2 } as const
  Object.values(grouped).forEach(arr =>
    arr.sort((a, b) => (statusOrder[a.status] ?? 3) - (statusOrder[b.status] ?? 3))
  )

  function toggleGroup(name: string) {
    setOpenGroups(prev => {
      const next = new Set(prev)
      next.has(name) ? next.delete(name) : next.add(name)
      return next
    })
  }

  const visibleGroups = groupOrder.filter(g => grouped[g]?.length)

  if (openIncident) {
    return <IncidentView incidentId={openIncident} onBack={() => setOpenIncident(null)} />
  }

  return (
    <div className="main">
      {/* Summary cards */}
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

      {/* Group tile grid */}
      {visibleGroups.length > 0 && (
        <div className="group-grid">
          {visibleGroups.map(g => (
            <GroupTile
              key={g || '__ungrouped__'}
              name={g || 'ungrouped'}
              alarms={grouped[g]}
              typeMap={typeMap}
              active={openGroups.has(g)}
              onClick={() => toggleGroup(g)}
            />
          ))}
        </div>
      )}

      {/* Drill-down panels — one per open group, in order */}
      {visibleGroups.filter(g => openGroups.has(g)).map(g => (
        <div key={g || '__ungrouped__'} className="drill-panel">
          <div className="drill-header">
            <div className="drill-header-name">{g || 'ungrouped'}</div>
            <button type="button" className="drill-header-close" onClick={() => toggleGroup(g)} aria-label="Close">×</button>
          </div>
          <div className="drill-body">
            {grouped[g].map(a => (
              <AlarmCard
                key={a.monitorName}
                alarm={a}
                monitorType={typeMap[a.monitorName]}
                selected={selected === a.monitorName}
                onSelect={a2 => setSelected(prev => prev === a2.monitorName ? null : a2.monitorName)}
                onOpenIncident={setOpenIncident}
              />
            ))}
          </div>
        </div>
      ))}

      {/* 24h timeline */}
      {all.length > 0 && (
        <div className="block" style={{ marginBottom: 20 }}>
          <h3>24h overview</h3>
          {all.map(a => (
            <Timeline key={a.monitorName} monitorName={a.monitorName} events={history} currentStatus={a.status} />
          ))}
        </div>
      )}
    </div>
  )
}
