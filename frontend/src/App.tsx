import { useEffect, useState } from 'react'
import { StatusService, type AppStatus } from '../bindings/icepick'
import type { Session } from '../bindings/icepick/internal/storage'

function App() {
  const [status, setStatus] = useState<AppStatus | null>(null)
  const [error, setError] = useState('')
  const [sessions, setSessions] = useState<Session[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    StatusService.GetStatus()
      .then(setStatus)
      .catch((reason: unknown) => setError(String(reason)))
    StatusService.ListSessions()
      .then((items) => setSessions(items ?? []))
      .catch((reason: unknown) => setError(String(reason)))
  }, [])

  async function createSession() {
    setCreating(true)
    try {
      const session = await StatusService.CreateSession()
      setSessions((current) => [session, ...current])
      setSelected(session.id)
      setError('')
    } catch (reason) {
      setError(String(reason))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">◈</span> ICEPick</div>
        <div className="sidebar-label">工作区</div>
        <div className="sidebar-item active">概览</div>
        <div className="sidebar-label sessions-label">会话</div>
        <button className="new-session" onClick={createSession} disabled={creating || !!error || !status?.storageReady}>＋ 新建会话</button>
        {sessions.map((session) => (
          <button key={session.id} className={`sidebar-item session-item ${selected === session.id ? 'selected' : ''}`} onClick={() => setSelected(session.id)}>{session.title}</button>
        ))}
        <div className="sidebar-item muted">工具 · 即将支持</div>
        <div className="sidebar-spacer" />
        <div className="sidebar-foot">本地桌面 Agent</div>
      </aside>

      <main className="content">
        <div className="eyebrow">LOCAL AGENT / DESKTOP</div>
        <h1>ICEPick</h1>
        <p className="intro">本地会话和任务存储已就绪。模型连接与 Agent 循环将按迁移计划接入。</p>

        <section className="status-card" aria-live="polite">
          <div className="card-title">运行状态</div>
          {error ? (
            <p className="error">无法读取本地服务状态：{error}</p>
          ) : status ? (
            <>
              <div className="status-row"><span>本地存储</span><strong className={status.storageReady ? 'ok' : ''}>{status.storageReady ? '已就绪' : '异常'}</strong></div>
              <div className="status-row"><span>Agent</span><strong>{status.agentReady ? '就绪' : '尚未实现'}</strong></div>
              <div className="status-row"><span>模型</span><strong>{status.modelConnected ? '已连接' : '未连接'}</strong></div>
              <p className="hint">{status.message}</p>
            </>
          ) : (
            <p className="hint">正在读取本地服务状态…</p>
          )}
        </section>

        <section className="session-card">
          <div className="card-title">{selected ? sessions.find((item) => item.id === selected)?.title : '空会话'}</div>
          <p className="hint">{selected ? '会话已保存在本机。发送任务将在 Agent 循环接入后开放。' : '新建会话后即可在本地保存工作记录。'}</p>
        </section>

        <p className="next-step">下一步：实现模型适配层与 Agent 运行循环。</p>
      </main>
    </div>
  )
}

export default App
