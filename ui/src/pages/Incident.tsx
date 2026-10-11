import { useEffect, useState } from 'react'
import { marked } from 'marked'
import { api, type Incident } from '../api/client'

interface Props {
  incidentId: string
  onBack: () => void
}

export function IncidentView({ incidentId, onBack }: Props) {
  const [incident, setIncident] = useState<Incident | null>(null)
  const [content, setContent] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    api.getIncident(incidentId)
      .then(r => { setIncident(r.incident); setContent(r.content) })
      .catch(e => setError(String(e)))
  }, [incidentId])

  const html = content ? marked.parse(content) as string : ''

  return (
    <div className="main">
      <div className="block" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <button className="btn-secondary" onClick={onBack}>← Back</button>
          <div style={{ flex: 1 }}>
            <div style={{ fontWeight: 600, fontSize: 15 }}>
              {incident?.monitorName ?? incidentId}
            </div>
            <div style={{ fontSize: 12, color: '#8b949e' }}>
              {incident && new Date(incident.firedAt).toLocaleString()}
              {' · '}
              <span style={{ color: incident?.status === 'active' ? '#f44336' : '#4caf50' }}>
                {incident?.status ?? '…'}
              </span>
              {incident?.resolvedAt && (
                <>
                  {' · resolved '}
                  {new Date(incident.resolvedAt).toLocaleString()}
                </>
              )}
            </div>
          </div>
          <div style={{ fontSize: 11, color: '#484f58', fontFamily: 'monospace' }}>{incidentId}</div>
        </div>

        {error && <div style={{ color: '#f44336', fontSize: 13 }}>{error}</div>}

        <div
          className="incident-content"
          style={{ overflowY: 'auto', maxHeight: 'calc(100vh - 220px)', minHeight: 200 }}
          dangerouslySetInnerHTML={{
            __html: html || '<p style="color:#484f58">No incident log recorded.</p>',
          }}
        />
      </div>
    </div>
  )
}
