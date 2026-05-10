import { useEffect, useRef, useState } from 'react'
import { marked } from 'marked'
import { api, type Incident } from '../api/client'

interface Props {
  incidentId: string
  onBack: () => void
}

export function IncidentView({ incidentId, onBack }: Props) {
  const [incident, setIncident] = useState<Incident | null>(null)
  const [active, setActive] = useState(false)
  const [content, setContent] = useState('')
  const [chatMsg, setChatMsg] = useState('')
  const [chatBusy, setChatBusy] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    api.getIncident(incidentId)
      .then(r => { setIncident(r.incident); setActive(r.active) })
      .catch(() => {})
  }, [incidentId])

  // SSE stream
  useEffect(() => {
    const es = new EventSource(`/api/incidents/${incidentId}/stream`)
    esRef.current = es

    es.addEventListener('delta', e => {
      const data = JSON.parse(e.data) as { text?: string }
      if (data.text) setContent(prev => prev + data.text)
    })
    es.addEventListener('done', () => {
      es.close()
      setActive(false)
    })
    es.onerror = () => es.close()

    return () => es.close()
  }, [incidentId])

  // Auto-scroll as content streams in
  useEffect(() => {
    if (contentRef.current) {
      contentRef.current.scrollTop = contentRef.current.scrollHeight
    }
  }, [content])

  async function sendChat() {
    const msg = chatMsg.trim()
    if (!msg || chatBusy) return
    setChatMsg('')
    setChatBusy(true)

    try {
      const res = await api.chatIncident(incidentId, msg)
      if (!res.ok) { setChatBusy(false); return }

      const reader = res.body?.getReader()
      const decoder = new TextDecoder()
      if (!reader) { setChatBusy(false); return }

      setContent(prev => prev + `\n\n---\n\n**You:** ${msg}\n\n`)

      let buf = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() ?? ''
        for (const line of lines) {
          if (line.startsWith('data:')) {
            try {
              const data = JSON.parse(line.slice(5).trim()) as { text?: string }
              if (data.text) setContent(prev => prev + data.text)
            } catch { /* ignore */ }
          }
        }
      }
    } finally {
      setChatBusy(false)
    }
  }

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
              {' · '}
              {incident?.investigationStatus ?? '…'}
            </div>
          </div>
          <div style={{ fontSize: 11, color: '#484f58', fontFamily: 'monospace' }}>{incidentId}</div>
        </div>

        <div
          ref={contentRef}
          className="incident-content"
          style={{ overflowY: 'auto', maxHeight: 'calc(100vh - 280px)', minHeight: 200 }}
          dangerouslySetInnerHTML={{ __html: html || '<p style="color:#484f58">Waiting for investigation…</p>' }}
        />

        {active && (
          <div className="form-row" style={{ gap: 8 }}>
            <input
              style={{ flex: 1 }}
              placeholder="Ask a follow-up question…"
              value={chatMsg}
              onChange={e => setChatMsg(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && !e.shiftKey && sendChat()}
              disabled={chatBusy}
            />
            <button onClick={sendChat} disabled={chatBusy || !chatMsg.trim()}>
              {chatBusy ? '…' : 'Send'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
