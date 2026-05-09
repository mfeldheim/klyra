import type { AlarmStatus, HistoryEvent } from '../api/client'

interface Props {
  monitorName: string
  events: HistoryEvent[]
  windowHours?: number
  currentStatus?: AlarmStatus
}

export function Timeline({ monitorName, events, windowHours = 24, currentStatus }: Props) {
  const now = Date.now()
  const windowMs = windowHours * 3600 * 1000
  const start = now - windowMs

  const relevant = events
    .filter(e => e.monitorName === monitorName && new Date(e.at).getTime() >= start)
    .sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime())

  const segments: { type: 'ok' | 'fire'; flex: number }[] = []
  let cursor = start
  for (const ev of relevant) {
    const t = new Date(ev.at).getTime()
    const flex = Math.max(1, t - cursor)
    if (ev.transition === 'FIRING') {
      if (cursor < t) segments.push({ type: 'ok', flex })
    } else {
      if (cursor < t) segments.push({ type: 'fire', flex })
    }
    cursor = t
  }
  const tail = Math.max(1, now - cursor)
  const lastFiring = relevant.length > 0
    ? relevant[relevant.length - 1].transition === 'FIRING'
    : currentStatus === 'FIRING'
  segments.push({ type: lastFiring ? 'fire' : 'ok', flex: tail })

  return (
    <div className="tl-row">
      <div className="tl-name">{monitorName}</div>
      <div className="tl-bar">
        {segments.map((s, i) => (
          <div key={i} className={s.type === 'fire' ? 'tl-fire' : 'tl-ok'} style={{ flex: s.flex }} />
        ))}
      </div>
      <div className="tl-labels"><span>{windowHours}h ago</span><span>now</span></div>
    </div>
  )
}
