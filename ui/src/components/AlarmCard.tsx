import { useEffect, useMemo, useRef, useState } from 'react'
import type { AlarmState } from '../api/client'
import { StatusBadge } from './StatusBadge'
import { typeIcon, iconFromName } from '../utils/typeIcon'

type WorkloadRef = {
  kind: 'deploy' | 'sts' | 'ds'
  namespace: string
  name: string
}

function parseAffectedWorkloads(message?: string): WorkloadRef[] {
  if (!message) return []
  const re = /(deploy|sts|ds)\/([^\/,\s]+)\/([^\s\(,]+)\s*\(\d+\/\d+\)/g
  const results: WorkloadRef[] = []
  const seen = new Set<string>()
  let match: RegExpExecArray | null
  while ((match = re.exec(message)) !== null) {
    const key = `${match[1]}/${match[2]}/${match[3]}`
    if (seen.has(key)) continue
    seen.add(key)
    results.push({
      kind: match[1] as WorkloadRef['kind'],
      namespace: match[2],
      name: match[3],
    })
  }
  return results
}

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
  const affectedWorkloads = useMemo(() => parseAffectedWorkloads(alarm.message), [alarm.message])
  const [logsOpen, setLogsOpen] = useState(false)
  const [logTarget, setLogTarget] = useState<WorkloadRef | null>(null)
  const [logLines, setLogLines] = useState<string[]>([])
  const [logFilter, setLogFilter] = useState('')
  const [logError, setLogError] = useState<string | null>(null)
  const [following, setFollowing] = useState(false)
  const logBodyRef = useRef<HTMLPreElement | null>(null)

  useEffect(() => {
    if (!selected) {
      setLogsOpen(false)
      setLogTarget(null)
      setLogLines([])
      setLogFilter('')
      setLogError(null)
      setFollowing(false)
    }
  }, [selected])

  useEffect(() => {
    if (!logsOpen) {
      setFollowing(false)
    }
  }, [logsOpen])

  useEffect(() => {
    if (!logsOpen || affectedWorkloads.length === 0) {
      setLogTarget(null)
      return
    }
    if (!logTarget) {
      setLogTarget(affectedWorkloads[0])
      return
    }
    const stillPresent = affectedWorkloads.some(
      w => w.kind === logTarget.kind && w.namespace === logTarget.namespace && w.name === logTarget.name,
    )
    if (!stillPresent) {
      setLogTarget(affectedWorkloads[0])
    }
  }, [logsOpen, affectedWorkloads, logTarget])

  useEffect(() => {
    if (!logsOpen || !logTarget) return

    setLogLines([])
    setLogError(null)
    setFollowing(true)

    const params = new URLSearchParams({
      kind: logTarget.kind,
      namespace: logTarget.namespace,
      name: logTarget.name,
      follow: 'true',
    })
    const es = new EventSource(`/api/workloads/logs?${params.toString()}`)

    es.addEventListener('line', evt => {
      try {
        const payload = JSON.parse((evt as MessageEvent).data) as { line?: string }
        if (!payload.line) return
        setLogLines(prev => {
          const next = [...prev, payload.line!]
          return next.length > 5000 ? next.slice(next.length - 5000) : next
        })
      } catch {
        // ignore malformed log line payloads
      }
    })

    es.addEventListener('error', evt => {
      try {
        const payload = JSON.parse((evt as MessageEvent).data) as { error?: string }
        if (payload.error) setLogError(payload.error)
      } catch {
        setLogError('log stream disconnected')
      }
      // Keep EventSource open so browser reconnect logic can continue following.
    })

    es.addEventListener('done', () => {
      setFollowing(false)
      es.close()
    })

    es.onerror = () => {
      setLogError('log stream disconnected')
      // Keep EventSource open so browser reconnect logic can continue following.
    }

    return () => {
      setFollowing(false)
      es.close()
    }
  }, [logsOpen, logTarget])

  useEffect(() => {
    if (!logBodyRef.current) return
    const el = logBodyRef.current
    const nearBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 60
    if (nearBottom || following) {
      el.scrollTop = el.scrollHeight
    }
  }, [logLines, following])

  const filteredLines = useMemo(() => {
    if (!logFilter.trim()) return logLines
    const q = logFilter.toLowerCase()
    return logLines.filter(line => line.toLowerCase().includes(q))
  }, [logLines, logFilter])

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
            {affectedWorkloads.length > 0 && (
              <div className="workload-log-tools" onClick={e => e.stopPropagation()}>
                <button
                  className="incident-btn"
                  onClick={() => {
                    setLogsOpen(true)
                    setLogTarget(affectedWorkloads[0])
                  }}
                >
                  View logs on affected resources
                </button>
              </div>
            )}

            {logsOpen && logTarget && (
              <div className="log-modal-overlay" onClick={() => setLogsOpen(false)}>
                <div className="log-modal" onClick={e => e.stopPropagation()}>
                  <div className="log-modal-header">
                    <div className="log-modal-title">Affected workload logs</div>
                    <button className="btn-secondary" onClick={() => setLogsOpen(false)}>Close</button>
                  </div>
                  <div className="log-shell-bar">
                    <select
                      className="log-workload-select"
                      value={`${logTarget.kind}/${logTarget.namespace}/${logTarget.name}`}
                      onChange={e => {
                        const [kind, namespace, name] = e.target.value.split('/')
                        setLogTarget({ kind: kind as WorkloadRef['kind'], namespace, name })
                      }}
                    >
                      {affectedWorkloads.map(w => {
                        const key = `${w.kind}/${w.namespace}/${w.name}`
                        return <option key={key} value={key}>{key}</option>
                      })}
                    </select>
                    <input
                      className="log-filter-input"
                      type="text"
                      value={logFilter}
                      onChange={e => setLogFilter(e.target.value)}
                      placeholder="filter logs"
                    />
                    <span className="log-shell-target">{following ? 'following' : 'stopped'}</span>
                  </div>
                  <div className="log-shell">
                    <pre className="log-shell-body" ref={logBodyRef}>
                      {filteredLines.length > 0 ? filteredLines.join('\n') : 'waiting for log lines...'}
                    </pre>
                    {logError && <div className="log-shell-error">{logError}</div>}
                  </div>
                </div>
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
