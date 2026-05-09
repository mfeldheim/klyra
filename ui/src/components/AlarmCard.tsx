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

function firedDuration(iso?: string): string {
  if (!iso) return ''
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ${diff % 60}s`
  const h = Math.floor(diff / 3600)
  const m = Math.floor((diff % 3600) / 60)
  return `${h}h ${m}m`
}

function typeIcon(type: string): JSX.Element {
  switch (type) {
    case 'http':
      return (
        <svg viewBox="0 0 16 16" width="10" height="10" fill="currentColor">
          <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.5" fill="none"/>
          <path d="M8 1c-1.5 2-2.5 4.5-2.5 7s1 5 2.5 7M8 1c1.5 2 2.5 4.5 2.5 7s-1 5-2.5 7M1 8h14" stroke="currentColor" strokeWidth="1.5" fill="none"/>
        </svg>
      )
    case 'kubernetes':
      return (
        <svg viewBox="0 0 16 16" width="10" height="10" fill="currentColor">
          <path d="M8 1L14.5 4.5v7L8 15 1.5 11.5v-7z" stroke="currentColor" strokeWidth="1.5" fill="none"/>
          <circle cx="8" cy="8" r="1.5"/>
        </svg>
      )
    case 'prometheus':
    case 'prometheus_scrape':
      return (
        <svg viewBox="0 0 16 16" width="10" height="10" fill="currentColor">
          <path d="M8 2c0 3-3 4-3 7a3 3 0 0 0 6 0c0-1.5-1-2.5-1-4 0 0-1 1-1 2.5C8.5 9 7 8 7 6.5 7 5 8 2 8 2z"/>
        </svg>
      )
    default:
      return (
        <svg viewBox="0 0 16 16" width="10" height="10" fill="currentColor">
          <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.5" fill="none"/>
          <text x="8" y="12" textAnchor="middle" fontSize="9">?</text>
        </svg>
      )
  }
}

interface AlarmCardProps {
  alarm: AlarmState
  monitorType?: string
  selected?: boolean
  onSelect?: (alarm: AlarmState) => void
}

export function AlarmCard({ alarm, monitorType, selected, onSelect }: AlarmCardProps) {
  return (
    <div
      className={`card ${alarm.status.toLowerCase()}${selected ? ' selected' : ''}${onSelect ? ' clickable' : ''}`}
      onClick={() => onSelect?.(alarm)}
    >
      <div className="card-body">
        <div className="card-name">{alarm.monitorName}</div>
        <div className="card-meta">
          <StatusBadge status={alarm.status} />
          {alarm.message && <> · {alarm.message}</>}
          {monitorType && (
            <> · <span className={`tag ${monitorType}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              {typeIcon(monitorType)}{monitorType}
            </span></>
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
