import type { AlarmState } from '../api/client'
import { StatusBadge } from './StatusBadge'
import { typeIcon, iconFromName } from '../utils/typeIcon'

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

function firedDuration(iso?: string): string {
  if (!iso) return ''
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ${diff % 60}s`
  const h = Math.floor(diff / 3600)
  const m = Math.floor((diff % 3600) / 60)
  return `${h}h ${m}m`
}

interface AlarmCardProps {
  alarm: AlarmState
  monitorType?: string
  selected?: boolean
  onSelect?: (alarm: AlarmState) => void
  onOpenIncident?: (id: string) => void
}

export function AlarmCard({ alarm, monitorType, selected, onSelect, onOpenIncident }: AlarmCardProps) {
  return (
    <div
      className={`card ${alarm.status.toLowerCase()}${selected ? ' selected' : ''}${onSelect ? ' clickable' : ''}`}
      onClick={() => onSelect?.(alarm)}
    >
      {alarm.icon && (
        <div className="card-icon" title={monitorType}>{iconFromName(alarm.icon, 20)}</div>
      )}
      <div className="card-body">
        <div className="card-name">{alarm.monitorName}</div>
        <div className="card-meta">
          <StatusBadge status={alarm.status} />
          {alarm.message && <> · {alarm.message}</>}
          {monitorType && !alarm.icon && (
            <> · <span className={`tag ${monitorType}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              {typeIcon(monitorType, 10)}{monitorType}
            </span></>
          )}
          {monitorType && alarm.icon && (
            <> · <span className={`tag ${monitorType}`}>{monitorType}</span></>
          )}
        </div>
        {selected && (
          <div className="card-detail">
            <div className="detail-row">
              <span className="detail-label">Last checked</span>
              <span className="detail-value">{new Date(alarm.lastCheck).toLocaleString()}</span>
            </div>
            {alarm.firedAt && (
              <>
                <div className="detail-row">
                  <span className="detail-label">Fired at</span>
                  <span className="detail-value">{new Date(alarm.firedAt).toLocaleString()}</span>
                </div>
                <div className="detail-row">
                  <span className="detail-label">Duration</span>
                  <span className="detail-value">{firedDuration(alarm.firedAt)}</span>
                </div>
              </>
            )}
            {alarm.lastValue !== undefined && (
              <div className="detail-row">
                <span className="detail-label">Value</span>
                <span className="detail-value">{String(alarm.lastValue)}</span>
              </div>
            )}
            {alarm.message && (
              <div className="detail-row">
                <span className="detail-label">Details</span>
                <span className="detail-value detail-message">{alarm.message}</span>
              </div>
            )}
            {alarm.incidentId && onOpenIncident && (
              <button
                className="incident-btn"
                onClick={e => { e.stopPropagation(); onOpenIncident(alarm.incidentId!) }}
              >
                View Incident →
              </button>
            )}
          </div>
        )}
      </div>
      <div className="card-right">
        {!selected && alarm.lastValue !== undefined && (
          <div className="card-value">{String(alarm.lastValue)}</div>
        )}
        <div className="card-time">
          {alarm.firedAt ? firedFor(alarm.firedAt) : `checked ${timeAgo(alarm.lastCheck)}`}
        </div>
      </div>
    </div>
  )
}
