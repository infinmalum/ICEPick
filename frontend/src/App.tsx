import { useEffect, useState } from 'react'
import { StatusService, type AppStatus } from '../bindings/icepick'
import type { AgentProfile, Message, Run, Session } from '../bindings/icepick/internal/storage'

type ModelDraft = { provider: string; baseUrl: string; name: string }

function App() {
  const [status, setStatus] = useState<AppStatus | null>(null)
  const [error, setError] = useState('')
  const [sessions, setSessions] = useState<Session[]>([])
  const [agents, setAgents] = useState<AgentProfile[]>([])
  const [agentID, setAgentID] = useState('chat')
  const [agentDraft, setAgentDraft] = useState<AgentProfile | null>(null)
  const [agentBusy, setAgentBusy] = useState(false)
  const [agentError, setAgentError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [messages, setMessages] = useState<Message[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [activeRun, setActiveRun] = useState<string | null>(null)
  const [prompt, setPrompt] = useState('')
  const [taskError, setTaskError] = useState('')
  const [sending, setSending] = useState(false)
  const [loadingSession, setLoadingSession] = useState(false)
  const [modelDraft, setModelDraft] = useState<ModelDraft>({ provider: 'ollama', baseUrl: 'http://127.0.0.1:11434/v1', name: '' })
  const [modelBusy, setModelBusy] = useState(false)
  const [modelError, setModelError] = useState('')

  useEffect(() => {
    StatusService.GetStatus()
      .then((value) => {
        setStatus(value)
        setModelDraft(value.model)
      })
      .catch((reason: unknown) => setError(String(reason)))
    StatusService.ListAgents().then((items) => setAgents(items ?? [])).catch((reason: unknown) => setError(String(reason)))
  }, [])

  useEffect(() => {
    setSelected(null)
    setSessions([])
    StatusService.ListAgentSessions(agentID).then((items) => setSessions(items ?? [])).catch((reason: unknown) => setError(String(reason)))
  }, [agentID])

  useEffect(() => {
    setAgentDraft(agents.find((item) => item.id === agentID) ?? null)
  }, [agents, agentID])

  useEffect(() => {
    if (!selected) {
      setMessages([])
      setRuns([])
      setActiveRun(null)
      setLoadingSession(false)
      return
    }
    let cancelled = false
    setLoadingSession(true)
    Promise.all([StatusService.ListMessages(agentID, selected), StatusService.ListRuns(agentID, selected)])
      .then(([history, previousRuns]) => {
        if (cancelled) return
        setMessages(history ?? [])
        setRuns(previousRuns ?? [])
        setActiveRun((previousRuns ?? []).find((item) => item.status === 'running')?.id ?? null)
        setLoadingSession(false)
      })
      .catch((reason: unknown) => { if (!cancelled) { setTaskError(String(reason)); setLoadingSession(false) } })
    return () => { cancelled = true }
  }, [agentID, selected])

  useEffect(() => {
    if (!activeRun || !selected) return
    let cancelled = false
    const timer = window.setInterval(async () => {
      try {
        const [run, history] = await Promise.all([StatusService.GetRun(agentID, activeRun), StatusService.ListMessages(agentID, selected)])
        if (cancelled) return
        setMessages(history ?? [])
        setRuns((current) => [run, ...current.filter((item) => item.id !== run.id)])
        if (run.status !== 'running') setActiveRun(null)
      } catch (reason) {
        if (!cancelled) setTaskError(String(reason))
      }
    }, 800)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [activeRun, agentID, selected])

  async function createAgent(fromID = '') {
    const name = window.prompt(fromID ? '副本名称' : '新 Agent 名称', fromID ? `${agents.find((item) => item.id === fromID)?.name ?? 'Agent'} 副本` : '')
    if (!name?.trim()) return
    setAgentBusy(true)
    try {
      const created = await StatusService.CreateAgent(name.trim(), fromID)
      setAgents((current) => [...current, created])
      setAgentID(created.id)
      setAgentError('')
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function saveAgent(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!agentDraft) return
    setAgentBusy(true)
    try {
      const saved = await StatusService.UpdateAgent(agentDraft)
      setAgents((current) => current.map((item) => item.id === saved.id ? saved : item))
      setAgentError('')
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function restoreAgent() {
    if (!agentDraft?.preset) return
    setAgentBusy(true)
    try {
      const restored = await StatusService.RestoreAgentPreset(agentDraft.id, agentDraft.revision)
      setAgents((current) => current.map((item) => item.id === restored.id ? restored : item))
      setAgentError('')
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function refreshModel() {
    setModelBusy(true)
    try {
      const value = await StatusService.GetStatus()
      setStatus(value)
      setModelDraft(value.model)
      setModelError('')
    } catch (reason) {
      setModelError(String(reason))
    } finally {
      setModelBusy(false)
    }
  }

  async function saveModel(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setModelBusy(true)
    try {
      const value = await StatusService.SaveModelConfig(modelDraft)
      setStatus(value)
      setModelError('')
    } catch (reason) {
      setModelError(String(reason))
    } finally {
      setModelBusy(false)
    }
  }

  async function createSession() {
    setCreating(true)
    try {
      const session = await StatusService.CreateAgentSession(agentID)
      setSessions((current) => [session, ...current])
      setSelected(session.id)
      setError('')
    } catch (reason) {
      setError(String(reason))
    } finally {
      setCreating(false)
    }
  }

  async function sendTask(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || !prompt.trim()) return
    setSending(true)
    try {
      const run = await StatusService.StartAgentTask(agentID, selected, prompt)
      setPrompt('')
      setTaskError('')
      setRuns((current) => [run, ...current])
      setActiveRun(run.id)
      const history = await StatusService.ListMessages(agentID, selected)
      setMessages(history ?? [])
    } catch (reason) {
      setTaskError(String(reason))
    } finally {
      setSending(false)
    }
  }

  async function cancelTask() {
    if (!activeRun) return
    try {
      await StatusService.CancelTask(agentID, activeRun)
      setTaskError('')
    } catch (reason) {
      setTaskError(String(reason))
    }
  }

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">◈</span> ICEPick</div>
        <div className="sidebar-label">工作区</div>
        <div className="sidebar-item active">概览</div>
        <div className="sidebar-label sessions-label">Agents</div>
        {agents.map((item) => <button key={item.id} className={`sidebar-item session-item ${agentID === item.id ? 'selected' : ''}`} onClick={() => setAgentID(item.id)}>{item.name}</button>)}
        <button className="new-session" onClick={() => createAgent()} disabled={agentBusy || !status?.storageReady}>＋ 空白 Agent</button>
        <div className="sidebar-label sessions-label">会话</div>
        <button className="new-session" onClick={createSession} disabled={creating || !!error || !status?.storageReady}>＋ 新建会话</button>
        {sessions.map((session) => (
          <button key={session.id} className={`sidebar-item session-item ${selected === session.id ? 'selected' : ''}`} onClick={() => setSelected(session.id)}>{session.title}</button>
        ))}
        <div className="sidebar-item muted">MCP · 当前时间</div>
        <div className="sidebar-spacer" />
        <div className="sidebar-foot">本地桌面 Agent</div>
      </aside>

      <main className="content">
        <div className="eyebrow">LOCAL AGENT / DESKTOP</div>
        <h1>{agents.find((item) => item.id === agentID)?.name ?? 'ICEPick'}</h1>
        <p className="intro">连接本地模型后即可发送任务。Agent 可调用 MCP 工具；Agent Skills 可提供按需加载的任务指令。</p>

        <section className="status-card" aria-live="polite">
          <div className="card-title">运行状态</div>
          {error ? (
            <p className="error">无法读取本地服务状态：{error}</p>
          ) : status ? (
            <>
              <div className="status-row"><span>本地存储</span><strong className={status.storageReady ? 'ok' : ''}>{status.storageReady ? '已就绪' : '异常'}</strong></div>
              <div className="status-row"><span>Agent</span><strong className={status.agentReady ? 'ok' : ''}>{status.agentReady ? '已就绪' : '请配置模型'}</strong></div>
              <div className="status-row"><span>模型服务</span><strong className={status.modelConnected ? 'ok' : ''}>{status.modelConnected ? '已连接' : '未连接'}</strong></div>
              <p className="hint">{status.message}</p>
            </>
          ) : (
            <p className="hint">正在读取本地服务状态…</p>
          )}
        </section>

        {agentDraft && <details className="session-card agent-config">
          <summary className="card-title">Agent 配置 <span className="revision">v{agentDraft.revision}</span></summary>
          <form className="model-form" onSubmit={saveAgent}>
            <label>名称<input value={agentDraft.name} onChange={(event) => setAgentDraft({ ...agentDraft, name: event.target.value })} required /></label>
            <label>说明<input value={agentDraft.description} onChange={(event) => setAgentDraft({ ...agentDraft, description: event.target.value })} /></label>
            <label>系统提示词<textarea rows={4} value={agentDraft.systemPrompt} onChange={(event) => setAgentDraft({ ...agentDraft, systemPrompt: event.target.value })} /></label>
            <label>可用 MCP 工具（每行一个）<textarea rows={3} value={(agentDraft.allowedTools ?? []).join('\n')} onChange={(event) => setAgentDraft({ ...agentDraft, allowedTools: event.target.value.split('\n').map((name) => name.trim()).filter(Boolean) })} /></label>
            <label>启用 Skills（每行一个名称）<textarea rows={2} value={(agentDraft.enabledSkills ?? []).join('\n')} onChange={(event) => setAgentDraft({ ...agentDraft, enabledSkills: event.target.value.split('\n').map((name) => name.trim()).filter(Boolean) })} /></label>
            <label>审批模式<select value={agentDraft.approvalMode} onChange={(event) => setAgentDraft({ ...agentDraft, approvalMode: event.target.value })}>
              <option value="manual">全人工</option><option value="semi">半自动</option><option value="automatic">全自动</option>
            </select></label>
            <label>模型来源<select value={agentDraft.model.provider || 'default'} onChange={(event) => setAgentDraft({ ...agentDraft, model: event.target.value === 'default' ? { provider: '', baseUrl: '', name: '' } : { provider: event.target.value, baseUrl: event.target.value === 'lmstudio' ? 'http://127.0.0.1:1234/v1' : 'http://127.0.0.1:11434/v1', name: '' } })}>
              <option value="default">使用应用默认模型</option><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option>
            </select></label>
            {agentDraft.model.provider && <><label>Agent 模型地址<input value={agentDraft.model.baseUrl} onChange={(event) => setAgentDraft({ ...agentDraft, model: { ...agentDraft.model, baseUrl: event.target.value } })} /></label><label>Agent 模型 ID<input value={agentDraft.model.name} onChange={(event) => setAgentDraft({ ...agentDraft, model: { ...agentDraft.model, name: event.target.value } })} /></label></>}
            <label>上下文预算（token）<input type="number" min={1024} max={1000000} value={agentDraft.contextBudget} onChange={(event) => setAgentDraft({ ...agentDraft, contextBudget: Number(event.target.value) })} /></label>
            <label className="checkbox-label"><input type="checkbox" checked={agentDraft.delegation.enabled} onChange={(event) => setAgentDraft({ ...agentDraft, delegation: { ...agentDraft.delegation, enabled: event.target.checked } })} />允许委派子 Agent</label>
            {agentDraft.delegation.enabled && <><label>可委派目标 ID（每行一个）<textarea rows={2} value={(agentDraft.delegation.targetAgentIds ?? []).join('\n')} onChange={(event) => setAgentDraft({ ...agentDraft, delegation: { ...agentDraft.delegation, targetAgentIds: event.target.value.split('\n').map((id) => id.trim()).filter(Boolean) } })} /></label><label>最大委派深度<input type="number" min={0} max={4} value={agentDraft.delegation.maxDepth} onChange={(event) => setAgentDraft({ ...agentDraft, delegation: { ...agentDraft.delegation, maxDepth: Number(event.target.value) } })} /></label></>}
            <label>允许的工作区路径（每行一个）<textarea rows={2} value={(agentDraft.permissions.workspacePaths ?? []).join('\n')} onChange={(event) => setAgentDraft({ ...agentDraft, permissions: { ...agentDraft.permissions, workspacePaths: event.target.value.split('\n').map((path) => path.trim()).filter(Boolean) } })} /></label>
            <label>允许的命令（每行一个）<textarea rows={2} value={(agentDraft.permissions.commands ?? []).join('\n')} onChange={(event) => setAgentDraft({ ...agentDraft, permissions: { ...agentDraft.permissions, commands: event.target.value.split('\n').map((cmd) => cmd.trim()).filter(Boolean) } })} /></label>
            <label>允许的网络目标（每行一个）<textarea rows={2} value={(agentDraft.permissions.networkHosts ?? []).join('\n')} onChange={(event) => setAgentDraft({ ...agentDraft, permissions: { ...agentDraft.permissions, networkHosts: event.target.value.split('\n').map((host) => host.trim()).filter(Boolean) } })} /></label>
            <div className="model-actions"><button type="submit" disabled={agentBusy}>保存 Agent</button><button type="button" className="secondary" disabled={agentBusy} onClick={() => createAgent(agentDraft.id)}>复制</button>{agentDraft.preset && <button type="button" className="secondary" disabled={agentBusy} onClick={restoreAgent}>恢复预设</button>}</div>
          </form>
          {agentError && <p className="error">{agentError}</p>}
          <p className="hint">目前仅执行已允许的内置只读工具；审批、权限范围、委派和上下文压缩尚未接通。</p>
        </details>}

        <section className="session-card">
          <div className="card-title">本地模型</div>
          <form className="model-form" onSubmit={saveModel}>
            <label>服务
              <select value={modelDraft.provider} onChange={(event) => setModelDraft({ provider: event.target.value, baseUrl: event.target.value === 'lmstudio' ? 'http://127.0.0.1:1234/v1' : 'http://127.0.0.1:11434/v1', name: '' })}>
                <option value="ollama">Ollama</option>
                <option value="lmstudio">LM Studio</option>
              </select>
            </label>
            <label>本机地址
              <input value={modelDraft.baseUrl} onChange={(event) => setModelDraft((current) => ({ ...current, baseUrl: event.target.value }))} spellCheck={false} required />
            </label>
            <label>模型 ID（可稍后选择）
              <input value={modelDraft.name} onChange={(event) => setModelDraft((current) => ({ ...current, name: event.target.value }))} list="local-models" spellCheck={false} />
              <datalist id="local-models">{status?.availableModels?.map((name) => <option key={name} value={name} />)}</datalist>
            </label>
            <div className="model-actions">
              <button type="submit" disabled={modelBusy || !status?.storageReady}>保存并检测</button>
              <button type="button" className="secondary" onClick={refreshModel} disabled={modelBusy}>刷新连接</button>
            </div>
          </form>
          {modelError && <p className="error">{modelError}</p>}
          {status?.modelConnected && <p className="hint">可见模型：{status.availableModels?.length ? status.availableModels.join('、') : '暂无'}</p>}
        </section>

        <section className="session-card">
          <div className="card-title">{selected ? sessions.find((item) => item.id === selected)?.title : '空会话'}</div>
          {!selected ? <p className="hint">先新建或选择会话。</p> : <>
            <div className="message-list" aria-live="polite">
              {messages.length === 0 && <p className="hint">还没有消息。可以试试“现在几点？”来验证 MCP 时间工具。</p>}
              {messages.map((item) => <div className={`message message-${item.role}`} key={item.id}><span className="message-role">{item.role === 'user' ? '你' : item.role === 'tool' ? 'MCP 工具' : 'ICEPick'}</span><p>{item.content}</p></div>)}
            </div>
            <form className="task-form" onSubmit={sendTask}>
              <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="给 ICEPick 一个任务…" rows={3} />
              <div className="model-actions">
                <button type="submit" disabled={sending || loadingSession || !!activeRun || !status?.storageReady || !prompt.trim()}>发送任务</button>
                {activeRun && <button type="button" className="secondary" onClick={cancelTask}>取消任务</button>}
              </div>
            </form>
            {activeRun && <p className="hint">任务正在运行…</p>}
            {taskError && <p className="error">{taskError}</p>}
            {runs[0]?.status === 'failed' && <p className="error">任务失败：{runs[0].error}</p>}
            {runs[0]?.status === 'cancelled' && <p className="hint">任务已取消。</p>}
          </>}
        </section>

        <p className="next-step">内置 MCP 工具：local__current_time。文件、命令和其他能力将在权限设计完成后接入。</p>
      </main>
    </div>
  )
}

export default App
