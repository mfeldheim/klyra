import type { AlarmState } from '../api/client'
import { StatusBadge } from './StatusBadge'

function timeAgo(iso: string): string {
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  return `${Math.floor(diff / 3600)}h ago`
}

function firedFor(iso?: string): string {
  if (!iso) return ''
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return `firing for ${diff}s`
  if (diff < 3600) return `firing for ${Math.floor(diff / 60)}m`
  return `firing for ${Math.floor(diff / 3600)}h`
}

export function AlarmCard({ alarm, monitorType }: { alarm: AlarmState; monitorType?: string }) {
  return (
    <div className={`card ${alarm.status.toLowerCase()}`}>
      <div className="card-body">
        <div className="card-name">{alarm.monitorName}</div>
        <div className="card-meta">
          <StatusBadge status={alarm.status} />
          {alarm.message && <> · {alarm.message}</>}
          {monitorType && <> · <span className={`tag ${monitorType}`}>{monitorType}</span></>}
        </div>
      </div>
      <div className="card-right">
        {alarm.lastValue !== undefined && (
          <div className="card-value">{String(alarm.lastValue)}</div>
        )}
        <div className="card-time">
          {alarm.firedAt ? firedFor(alarm.firedAt) : `checked ${timeAgo(alarm.lastCheck)}`}
        </div>
      </div>
    </div>
  )
}
