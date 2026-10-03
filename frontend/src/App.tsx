import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { StatusService, type AppStatus } from '../bindings/icepick'
import type { Agent, AgentPolicy, ExfilGrant, Message, Profile, Run, Session, ToolCall } from '../bindings/icepick/internal/storage'
import type { CredentialStatus, MCPEndpointStatus } from '../bindings/icepick/internal/app'

type ModelDraft = { provider: string; baseUrl: string; name: string }

const remoteProviders = ['openai', 'anthropic', 'gemini'] as const
const providerDefaults: Record<string, string> = {
  ollama: 'http://127.0.0.1:11434/v1',
  lmstudio: 'http://127.0.0.1:1234/v1',
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  gemini: 'https://generativelanguage.googleapis.com',
}
const providerLabels: Record<string, string> = {
  ollama: 'Ollama（本机）', lmstudio: 'LM Studio（本机）', openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Gemini',
}
const scopeLabels: Record<string, string> = {
  public: '公开内容', workspace: '工作区文件', personal: '个人输入', credential: '凭据', unknown: '未知来源',
}

function App() {
  const [status, setStatus] = useState<AppStatus | null>(null)
  const [error, setError] = useState('')
  const [sessions, setSessions] = useState<Session[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [agentID, setAgentID] = useState('default')
  const [agentDraft, setAgentDraft] = useState<Agent | null>(null)
  const [agentBusy, setAgentBusy] = useState(false)
  const [agentError, setAgentError] = useState('')
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [profileEditID, setProfileEditID] = useState<string | null>(null)
  const [profileDraft, setProfileDraft] = useState<Profile | null>(null)
  const [profileBusy, setProfileBusy] = useState(false)
  const [profileError, setProfileError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [messages, setMessages] = useState<Message[]>([])
  const [toolCalls, setToolCalls] = useState<ToolCall[]>([])
  const [mcpServers, setMcpServers] = useState<MCPEndpointStatus[]>([])
  const [approvalBusy, setApprovalBusy] = useState(false)
  const [runs, setRuns] = useState<Run[]>([])
  const [activeRun, setActiveRun] = useState<string | null>(null)
  const [prompt, setPrompt] = useState('')
  const [taskError, setTaskError] = useState('')
  const [sending, setSending] = useState(false)
  const [loadingSession, setLoadingSession] = useState(false)
  const [modelDraft, setModelDraft] = useState<ModelDraft>({ provider: 'ollama', baseUrl: 'http://127.0.0.1:11434/v1', name: '' })
  const [modelBusy, setModelBusy] = useState(false)
  const [modelError, setModelError] = useState('')
  const [credStatus, setCredStatus] = useState<CredentialStatus[]>([])
  const [credDraft, setCredDraft] = useState<Record<string, string>>({})
  const [credBusy, setCredBusy] = useState(false)
  const [credError, setCredError] = useState('')
  const [grants, setGrants] = useState<ExfilGrant[]>([])
  const [grantForm, setGrantForm] = useState<{ destination: string; scopes: string[] }>({ destination: '', scopes: [] })
  const [grantBusy, setGrantBusy] = useState(false)
  const [grantError, setGrantError] = useState('')
  const [streamText, setStreamText] = useState('')

  useEffect(() => {
    StatusService.GetStatus()
      .then((value) => {
        setStatus(value)
        setModelDraft(value.model)
      })
      .catch((reason: unknown) => setError(String(reason)))
    StatusService.ListAgents().then((items) => setAgents(items ?? [])).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListProfiles().then((items) => setProfiles(items ?? [])).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListMCPEndpoints().then((items) => setMcpServers(items ?? [])).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListCredentialStatus().then((items) => setCredStatus(items ?? [])).catch((reason: unknown) => setCredError(String(reason)))
    StatusService.ListExfilGrants().then((items) => setGrants(items ?? [])).catch((reason: unknown) => setGrantError(String(reason)))
  }, [])

  const activeAgent = agents.find((item) => item.id === agentID) ?? null

  useEffect(() => {
    setAgentDraft(activeAgent ? structuredClone(activeAgent) : null)
  }, [activeAgent?.id, agents])

  // When the active agent changes, follow the profile it currently
  // references so the editor shows what the next run will use.
  useEffect(() => {
    if (!activeAgent) {
      setProfileEditID(null)
      setProfileDraft(null)
      return
    }
    setProfileEditID(activeAgent.profileId)
  }, [activeAgent?.profileId])

  useEffect(() => {
    const target = profiles.find((item) => item.id === profileEditID) ?? null
    setProfileDraft(target ? structuredClone(target) : null)
  }, [profileEditID, profiles])

  useEffect(() => {
    setSelected(null)
    setSessions([])
    setToolCalls([])
    setStreamText('')
    StatusService.ListAgentSessions(agentID).then((items) => setSessions(items ?? [])).catch((reason: unknown) => setError(String(reason)))
  }, [agentID])

  useEffect(() => {
    if (!selected) {
      setMessages([])
      setRuns([])
      setToolCalls([])
      setActiveRun(null)
      setLoadingSession(false)
      return
    }
    let cancelled = false
    setLoadingSession(true)
    Promise.all([StatusService.ListMessages(agentID, selected), StatusService.ListRuns(agentID, selected), StatusService.ListToolCalls(agentID, selected)])
      .then(([history, previousRuns, calls]) => {
        if (cancelled) return
        setMessages(history ?? [])
        setRuns(previousRuns ?? [])
        setToolCalls(calls ?? [])
        setActiveRun((previousRuns ?? []).find((item) => item.status === 'running')?.id ?? null)
        setLoadingSession(false)
      })
      .catch((reason: unknown) => { if (!cancelled) { setTaskError(String(reason)); setLoadingSession(false) } })
    return () => { cancelled = true }
  }, [agentID, selected])

  useEffect(() => {
    if (!activeRun || !selected) return
    let cancelled = false
    // Reconnect: pick up the streamed-but-uncommitted text so a page reload
    // or reconnect keeps the live transcript continuous.
    StatusService.PendingStreamText(agentID, activeRun).then((value) => { if (!cancelled) setStreamText(value ?? '') }).catch(() => {})
    const timer = window.setInterval(async () => {
      try {
        const [run, history, calls, pending] = await Promise.all([
          StatusService.GetRun(agentID, activeRun),
          StatusService.ListMessages(agentID, selected),
          StatusService.ListToolCalls(agentID, selected),
          StatusService.PendingStreamText(agentID, activeRun),
        ])
        if (cancelled) return
        setMessages(history ?? [])
        setToolCalls(calls ?? [])
        setStreamText(pending ?? '')
        setRuns((current) => [run, ...current.filter((item) => item.id !== run.id)])
        if (run.status !== 'running') setActiveRun(null)
      } catch (reason) {
        if (!cancelled) setTaskError(String(reason))
      }
    }, 800)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [activeRun, agentID, selected])

  // Live streaming deltas arrive through the Wails event bridge; the poll
  // above stays authoritative and overwrites the local merge each tick.
  useEffect(() => {
    if (!activeRun) return
    const off = Events.On('icepick:service-event', (event) => {
      const detail = (event.data as { type?: string; runId?: string; detail?: string }) ?? {}
      if (detail.type === 'stream.delta' && detail.runId === activeRun && detail.detail) {
        setStreamText((current) => current + detail.detail)
      }
    })
    return () => { off() }
  }, [activeRun])

  const pendingCalls = toolCalls.filter((item) => item.state === 'pending')
  const unknownCalls = toolCalls.filter((item) => item.state === 'execution_unknown')

  function profileName(id: string) {
    return profiles.find((item) => item.id === id)?.name ?? `Profile ${id.slice(0, 8)}…`
  }

  async function refreshAgents() {
    const items = await StatusService.ListAgents()
    setAgents(items ?? [])
    const refreshed = (items ?? []).find((item) => item.id === agentID) ?? null
    setAgentDraft(refreshed ? structuredClone(refreshed) : null)
  }

  async function refreshProfiles() {
    const items = await StatusService.ListProfiles()
    setProfiles(items ?? [])
  }

  async function refreshCalls() {
    if (!selected) return
    try {
      setToolCalls((await StatusService.ListToolCalls(agentID, selected)) ?? [])
    } catch (reason) { setTaskError(String(reason)) }
  }

  async function approveCall(call: ToolCall) {
    setApprovalBusy(true)
    try {
      await StatusService.ApproveToolCall(agentID, call.id)
      setTaskError('')
      await refreshCalls()
    } catch (reason) { setTaskError(String(reason)) } finally { setApprovalBusy(false) }
  }

  async function denyCall(call: ToolCall) {
    const note = window.prompt(`拒绝 ${call.toolName} 的原因（可留空）`, '') ?? ''
    setApprovalBusy(true)
    try {
      await StatusService.DenyToolCall(agentID, call.id, note)
      setTaskError('')
      await refreshCalls()
    } catch (reason) { setTaskError(String(reason)) } finally { setApprovalBusy(false) }
  }

  async function resolveUnknown(call: ToolCall, asSuccess: boolean) {
    const note = window.prompt(`处置依据（必填）：说明你核实到的 ${call.toolName} 的实际情况`, '') ?? ''
    if (!note.trim()) return
    let result = ''
    if (asSuccess) {
      result = window.prompt('经核对的实际结果（必填）', '') ?? ''
      if (!result.trim()) return
    }
    setApprovalBusy(true)
    try {
      await StatusService.ResolveToolCall(agentID, call.id, asSuccess ? 'succeeded' : 'failed', result, note)
      setTaskError('')
      await refreshCalls()
    } catch (reason) { setTaskError(String(reason)) } finally { setApprovalBusy(false) }
  }

  async function createAgent(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!agentDraft) return
    const form = event.currentTarget
    const name = (form.elements.namedItem('newAgentName') as HTMLInputElement).value.trim()
    const profileID = (form.elements.namedItem('newAgentProfile') as HTMLSelectElement).value
    if (!name || !profileID) return
    setAgentBusy(true)
    try {
      const created = await StatusService.CreateAgent(name, profileID)
      await refreshAgents()
      setAgentID(created.id)
      setAgentError('')
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function saveAgent(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!agentDraft) return
    setAgentBusy(true)
    try {
      await StatusService.UpdateAgent(agentDraft.id, agentDraft.name, agentDraft.policy)
      setAgentError('')
      await refreshAgents()
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function switchProfile(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!agentDraft) return
    const profileID = (event.currentTarget.elements.namedItem('switchProfile') as HTMLSelectElement).value
    if (!profileID || profileID === agentDraft.profileId) return
    setAgentBusy(true)
    try {
      await StatusService.SwitchAgentProfile(agentDraft.id, profileID)
      setAgentError('')
      await refreshAgents()
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function deleteAgent() {
    if (!agentDraft || !window.confirm(`删除 Agent「${agentDraft.name}」？其会话保留前必须先转移；共享 Profile 不会被删除。`)) return
    setAgentBusy(true)
    try {
      await StatusService.DeleteAgent(agentDraft.id)
      await refreshAgents()
      setAgentID('default')
      setAgentError('')
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function saveProfile(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!profileDraft) return
    setProfileBusy(true)
    try {
      const saved = await StatusService.UpdateProfile(profileDraft)
      setProfileError('')
      await refreshProfiles()
      setProfileDraft(saved)
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
  }

  async function createProfile(fromID = '') {
    let name = ''
    if (!fromID) {
      name = window.prompt('新 Profile 名称（空白模板）', '') ?? ''
      if (!name.trim()) return
    }
    setProfileBusy(true)
    try {
      const created = await StatusService.CreateProfile(name.trim(), fromID)
      await refreshProfiles()
      setProfileEditID(created.id)
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
  }

  async function deleteProfile() {
    if (!profileDraft || !window.confirm(`删除 Profile「${profileDraft.name}」？仅当没有 Agent 引用它时才会成功。`)) return
    setProfileBusy(true)
    try {
      await StatusService.DeleteProfile(profileDraft.id)
      await refreshProfiles()
      setProfileEditID(activeAgent?.profileId ?? 'default')
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
  }

  async function restoreProfileDefault() {
    if (!profileDraft) return
    setProfileBusy(true)
    try {
      const restored = await StatusService.RestoreProfileDefault(profileDraft.revision)
      await refreshProfiles()
      setProfileDraft(restored)
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
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

  async function saveCredential(provider: string) {
    const apiKey = (credDraft[provider] ?? '').trim()
    if (!apiKey) return
    setCredBusy(true)
    try {
      await StatusService.SaveCredential(provider, apiKey)
      setCredDraft((current) => ({ ...current, [provider]: '' }))
      setCredStatus((await StatusService.ListCredentialStatus()) ?? [])
      setCredError('')
    } catch (reason) { setCredError(String(reason)) } finally { setCredBusy(false) }
  }

  async function deleteCredential(provider: string) {
    setCredBusy(true)
    try {
      await StatusService.DeleteCredential(provider)
      setCredStatus((await StatusService.ListCredentialStatus()) ?? [])
      setCredError('')
    } catch (reason) { setCredError(String(reason)) } finally { setCredBusy(false) }
  }

  async function saveGrant(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!grantForm.destination.trim() || grantForm.scopes.length === 0) return
    setGrantBusy(true)
    try {
      await StatusService.GrantExfil(grantForm.destination, grantForm.scopes)
      setGrants((await StatusService.ListExfilGrants()) ?? [])
      setGrantForm({ destination: '', scopes: [] })
      setGrantError('')
    } catch (reason) { setGrantError(String(reason)) } finally { setGrantBusy(false) }
  }

  async function revokeGrant(destination: string) {
    if (!window.confirm(`撤销向 ${destination} 发送会话数据的授权？后续远端发送将被拒绝。`)) return
    setGrantBusy(true)
    try {
      await StatusService.RevokeExfil(destination)
      setGrants((await StatusService.ListExfilGrants()) ?? [])
      setGrantError('')
    } catch (reason) { setGrantError(String(reason)) } finally { setGrantBusy(false) }
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
    setStreamText('')
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

  function updatePolicy(patch: Partial<AgentPolicy>) {
    if (!agentDraft) return
    setAgentDraft({ ...agentDraft, policy: { ...agentDraft.policy, ...patch } })
  }

  function updatePermissions(patch: Partial<AgentPolicy['permissions']>) {
    if (!agentDraft) return
    setAgentDraft({ ...agentDraft, policy: { ...agentDraft.policy, permissions: { ...agentDraft.policy.permissions, ...patch } } })
  }

  const referencesOf = (id: string) => agents.filter((item) => item.profileId === id)

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">◈</span> ICEPick</div>
        <div className="sidebar-label">工作区</div>
        <div className="sidebar-item active">概览</div>
        <div className="sidebar-label sessions-label">Agents</div>
        {agents.map((item) => (
          <button key={item.id} className={`sidebar-item session-item ${agentID === item.id ? 'selected' : ''}`} onClick={() => setAgentID(item.id)}>
            {item.name}
            <span className="agent-profile-hint"> · {profileName(item.profileId)}</span>
          </button>
        ))}
        <div className="sidebar-label sessions-label">会话</div>
        <button className="new-session" onClick={createSession} disabled={creating || !!error || !status?.storageReady}>＋ 新建会话</button>
        {sessions.map((session) => (
          <button key={session.id} className={`sidebar-item session-item ${selected === session.id ? 'selected' : ''}`} onClick={() => setSelected(session.id)}>{session.title}</button>
        ))}
        <div className="sidebar-label sessions-label">MCP 服务</div>
        {mcpServers.map((server) => (
          <div key={server.name} className="sidebar-item muted" title={server.changed ? `${server.command}${(server.args ?? []).join(' ')}\n指纹变化：二进制 ${server.binarySha256.slice(0, 12)}… schema ${server.schemaSha256.slice(0, 12)}…\n自动执行已暂停，需要重新确认` : `${server.command}${(server.args ?? []).join(' ')}\n版本 ${server.version || '未知'} · 二进制 ${server.binarySha256.slice(0, 12)}… · schema ${server.schemaSha256.slice(0, 12)}…`}>
            MCP · {server.name}{server.changed ? ' ⚠' : ''}
          </div>
        ))}
        <div className="sidebar-spacer" />
        <div className="sidebar-foot">本地桌面 Agent</div>
      </aside>

      <main className="content">
        <div className="eyebrow">LOCAL AGENT / DESKTOP</div>
        <h1>{activeAgent?.name ?? 'ICEPick'}</h1>
        <p className="intro">Agent 是稳定实例（会话与授权随它走），Profile 是可共享的配置组合；同一 Profile 可被多个 Agent 引用，编辑只影响后续运行。</p>

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

        {agentDraft && <details className="session-card agent-config" open>
          <summary className="card-title">Agent「{agentDraft.name}」 <span className="revision">{profileName(agentDraft.profileId)} v{profiles.find((item) => item.id === agentDraft.profileId)?.revision ?? '?'}</span></summary>
          <form className="model-form" onSubmit={saveAgent}>
            <label>Agent 名称<input value={agentDraft.name} onChange={(event) => setAgentDraft({ ...agentDraft, name: event.target.value })} required /></label>
            <label>审批模式<select value={agentDraft.policy.approvalMode} onChange={(event) => updatePolicy({ approvalMode: event.target.value })}>
              <option value="manual">全人工</option><option value="semi">半自动</option><option value="automatic">全自动</option>
            </select></label>
            <label className="checkbox-label"><input type="checkbox" checked={agentDraft.policy.permissions.unrestricted} onChange={(event) => updatePermissions({ unrestricted: event.target.checked })} />不限工作区、命令和网络目标</label>
            {!agentDraft.policy.permissions.unrestricted && <><label>允许的工作区路径（每行一个）<textarea rows={2} value={(agentDraft.policy.permissions.workspacePaths ?? []).join('\n')} onChange={(event) => updatePermissions({ workspacePaths: event.target.value.split('\n').map((path) => path.trim()).filter(Boolean) })} /></label><label>允许的命令（每行一个）<textarea rows={2} value={(agentDraft.policy.permissions.commands ?? []).join('\n')} onChange={(event) => updatePermissions({ commands: event.target.value.split('\n').map((cmd) => cmd.trim()).filter(Boolean) })} /></label><label>允许的网络目标（每行一个）<textarea rows={2} value={(agentDraft.policy.permissions.networkHosts ?? []).join('\n')} onChange={(event) => updatePermissions({ networkHosts: event.target.value.split('\n').map((host) => host.trim()).filter(Boolean) })} /></label></>}
            <div className="model-actions"><button type="submit" disabled={agentBusy}>保存 Agent（名称与策略）</button><button type="button" className="secondary" disabled={agentBusy || agentDraft.id === 'default'} onClick={deleteAgent}>删除 Agent</button></div>
          </form>
          <form className="model-form" onSubmit={switchProfile}>
            <label>切换 Profile（会话与授权保留，下一个运行生效）
              <select name="switchProfile" defaultValue={agentDraft.profileId}>
                {profiles.map((item) => <option key={item.id} value={item.id}>{item.name}（v{item.revision}）</option>)}
              </select>
            </label>
            <div className="model-actions"><button type="submit" disabled={agentBusy}>切换 Profile</button></div>
          </form>
          <form className="model-form" onSubmit={createAgent}>
            <label>新建 Agent（引用已有 Profile，授权独立初始化）
              <input name="newAgentName" placeholder="新 Agent 名称" required />
            </label>
            <label>引用 Profile
              <select name="newAgentProfile" defaultValue={agentDraft.profileId}>
                {profiles.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select>
            </label>
            <div className="model-actions"><button type="submit" disabled={agentBusy}>创建 Agent</button></div>
          </form>
          {agentError && <p className="error">{agentError}</p>}
          <p className="hint">审批与资源范围属于 Agent 本机策略；Profile 只承载提示词、模型和能力选择。切换 Profile 不会清空会话历史，也不会扩大授权。</p>
        </details>}

        {profileDraft && <section className="session-card">
          <div className="card-title">Profile「{profileDraft.name}」 <span className="revision">v{profileDraft.revision}{profileDraft.isDefault ? ' · 内置默认' : ` · 被 ${referencesOf(profileDraft.id).length} 个 Agent 引用`}</span></div>
          <label className="model-form">编辑目标
            <select value={profileEditID ?? ''} onChange={(event) => setProfileEditID(event.target.value)}>
              {profiles.map((item) => <option key={item.id} value={item.id}>{item.name}{item.isDefault ? '（内置默认）' : `（${referencesOf(item.id).length} 个引用）`}</option>)}
            </select>
          </label>
          <div className="model-actions">
            <button type="button" className="secondary" disabled={profileBusy} onClick={() => createProfile('')}>＋ 空白 Profile</button>
            <button type="button" className="secondary" disabled={profileBusy} onClick={() => createProfile(profileDraft.id)}>复制当前</button>
            <button type="button" className="secondary" disabled={profileBusy || profileDraft.isDefault} onClick={deleteProfile}>删除</button>
            {profileDraft.isDefault && <button type="button" className="secondary" disabled={profileBusy} onClick={restoreProfileDefault}>恢复内置内容</button>}
          </div>
          <form className="model-form" onSubmit={saveProfile}>
            <label>Profile 名称<input value={profileDraft.name} onChange={(event) => setProfileDraft({ ...profileDraft, name: event.target.value })} required /></label>
            <label>说明<input value={profileDraft.data.description} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, description: event.target.value } })} /></label>
            <label>系统提示词<textarea rows={4} value={profileDraft.data.systemPrompt} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, systemPrompt: event.target.value } })} /></label>
            <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.allMcpServers} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, allMcpServers: event.target.checked } })} />启用全部已接入的 MCP 服务</label>
            {!profileDraft.data.allMcpServers && <label>启用的 MCP 服务名（每行一个）<textarea rows={2} value={(profileDraft.data.enabledMcpServers ?? []).join('\n')} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, enabledMcpServers: event.target.value.split('\n').map((name) => name.trim()).filter(Boolean) } })} /></label>}
            <label>工具白名单（每行一个，留空表示启用服务的全部工具）<textarea rows={3} value={(profileDraft.data.allowedTools ?? []).join('\n')} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, allowedTools: event.target.value.split('\n').map((name) => name.trim()).filter(Boolean) } })} /></label>
            <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.allSkills} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, allSkills: event.target.checked } })} />启用全部已安装的 Skills</label>
            {!profileDraft.data.allSkills && <label>启用 Skills（每行一个名称）<textarea rows={2} value={(profileDraft.data.enabledSkills ?? []).join('\n')} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, enabledSkills: event.target.value.split('\n').map((name) => name.trim()).filter(Boolean) } })} /></label>}
            <label>模型来源<select value={profileDraft.data.model.provider || 'default'} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, model: event.target.value === 'default' ? { provider: '', baseUrl: '', name: '' } : { provider: event.target.value, baseUrl: providerDefaults[event.target.value] ?? '', name: '' } } })}>
              <option value="default">使用应用默认模型</option><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option>
              <option value="openai">OpenAI</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option>
            </select></label>
            {profileDraft.data.model.provider && <><label>Profile 模型地址{remoteProviders.includes(profileDraft.data.model.provider as (typeof remoteProviders)[number]) ? '（留空使用官方端点）' : ''}
              <input value={profileDraft.data.model.baseUrl} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, model: { ...profileDraft.data.model, baseUrl: event.target.value } } })} placeholder={providerDefaults[profileDraft.data.model.provider] ?? ''} /></label><label>Profile 模型 ID<input value={profileDraft.data.model.name} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, model: { ...profileDraft.data.model, name: event.target.value } } })} /></label></>}
            <label>上下文预算（token）<input type="number" min={1024} max={1000000} value={profileDraft.data.contextBudget} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, contextBudget: Number(event.target.value) } })} /></label>
            <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.delegation.enabled} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, delegation: { ...profileDraft.data.delegation, enabled: event.target.checked } } })} />允许委派子 Agent（尚未接通）</label>
            {profileDraft.data.delegation.enabled && <label>最大委派深度<input type="number" min={0} max={4} value={profileDraft.data.delegation.maxDepth} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, delegation: { ...profileDraft.data.delegation, maxDepth: Number(event.target.value) } } })} /></label>}
            <div className="model-actions"><button type="submit" disabled={profileBusy}>保存 Profile</button></div>
          </form>
          {profileError && <p className="error">{profileError}</p>}
          <p className="hint">Profile 可复制分享、不含实例数据与授权；保存后立即原子生效，被引用时编辑影响所有引用 Agent 的后续运行。</p>
        </section>}

        <section className="session-card">
          <div className="card-title">模型服务</div>
          <form className="model-form" onSubmit={saveModel}>
            <label>提供方
              <select value={modelDraft.provider} onChange={(event) => setModelDraft({ provider: event.target.value, baseUrl: providerDefaults[event.target.value] ?? '', name: '' })}>
                <option value="ollama">Ollama（本机）</option>
                <option value="lmstudio">LM Studio（本机）</option>
                <option value="openai">OpenAI（远端）</option>
                <option value="anthropic">Anthropic（远端）</option>
                <option value="gemini">Gemini（远端）</option>
              </select>
            </label>
            {remoteProviders.includes(modelDraft.provider as (typeof remoteProviders)[number]) ? (
              <>
                <label>API 地址（留空使用官方端点）
                  <input value={modelDraft.baseUrl} onChange={(event) => setModelDraft((current) => ({ ...current, baseUrl: event.target.value }))} placeholder={providerDefaults[modelDraft.provider] ?? ''} spellCheck={false} />
                </label>
                <p className="hint">远端发送受数据外发授权约束；请在下方凭据面板保存 API 密钥，并在外发授权面板允许该端点。</p>
              </>
            ) : (
              <label>本机地址
                <input value={modelDraft.baseUrl} onChange={(event) => setModelDraft((current) => ({ ...current, baseUrl: event.target.value }))} spellCheck={false} required />
              </label>
            )}
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
          <div className="card-title">远端凭据</div>
          <p className="hint">密钥只保存在本机数据目录（0600），不进入配置、Profile、运行记录或日志；绑定只显示是否已配置，不会回显密钥。</p>
          {credStatus.map((item) => (
            <div className="credential-row" key={item.provider}>
              <strong>{providerLabels[item.provider] ?? item.provider}</strong>
              <span className={`credential-state ${item.configured ? 'ok' : ''}`}>{item.configured ? '已配置' : '未配置'}</span>
              <input type="password" placeholder="API 密钥" value={credDraft[item.provider] ?? ''} onChange={(event) => setCredDraft((current) => ({ ...current, [item.provider]: event.target.value }))} autoComplete="off" />
              <button type="button" disabled={credBusy || !(credDraft[item.provider] ?? '').trim()} onClick={() => saveCredential(item.provider)}>保存</button>
              <button type="button" className="secondary" disabled={credBusy || !item.configured} onClick={() => deleteCredential(item.provider)}>删除</button>
            </div>
          ))}
          {credError && <p className="error">{credError}</p>}
        </section>

        <section className="session-card">
          <div className="card-title">数据外发授权</div>
          <p className="hint">远端模型发送前会核对本会话累计数据范围与该端点的授权；本机模型不经过此检查。未知范围必须显式授权。</p>
          {(grants ?? []).length === 0 && <p className="hint">还没有外发授权。配置远端模型并首次发送时，会在此提示需要授权。</p>}
          {(grants ?? []).map((grant) => (
            <div className="grant-row" key={grant.destination}>
              <code>{grant.destination}</code>
              <span className="grant-scopes">{(grant.scopes ?? []).map((scope) => scopeLabels[scope] ?? scope).join('、') || '无范围'}</span>
              <button type="button" className="secondary" disabled={grantBusy} onClick={() => revokeGrant(grant.destination)}>撤销</button>
            </div>
          ))}
          <form className="model-form" onSubmit={saveGrant}>
            <label>目的地端点（如 https://api.openai.com）
              <input value={grantForm.destination} onChange={(event) => setGrantForm((current) => ({ ...current, destination: event.target.value }))} placeholder="https://…" spellCheck={false} required />
            </label>
            <div className="scope-checks">
              {Object.entries(scopeLabels).map(([scope, label]) => (
                <label className="checkbox-label" key={scope}>
                  <input type="checkbox" checked={grantForm.scopes.includes(scope)} onChange={(event) => setGrantForm((current) => ({
                    ...current,
                    scopes: event.target.checked ? [...current.scopes, scope] : current.scopes.filter((item) => item !== scope),
                  }))} />
                  {label}
                </label>
              ))}
            </div>
            <div className="model-actions"><button type="submit" disabled={grantBusy || !grantForm.destination.trim() || grantForm.scopes.length === 0}>保存授权</button></div>
          </form>
          {grantError && <p className="error">{grantError}</p>}
        </section>

        <section className="session-card">
          <div className="card-title">{selected ? sessions.find((item) => item.id === selected)?.title : '空会话'}</div>
          {!selected ? <p className="hint">先新建或选择会话。</p> : <>
            {(pendingCalls.length > 0 || unknownCalls.length > 0) && (
              <div className="approval-panel">
                <div className="card-title">待处理工具调用</div>
                {pendingCalls.map((call) => (
                  <div className="approval-row" key={call.id}>
                    <div className="approval-info">
                      <code>{call.toolName}</code>
                      <pre>{call.arguments}</pre>
                    </div>
                    <div className="approval-actions">
                      <button disabled={approvalBusy} onClick={() => approveCall(call)}>批准执行</button>
                      <button className="secondary" disabled={approvalBusy} onClick={() => denyCall(call)}>拒绝</button>
                    </div>
                  </div>
                ))}
                {unknownCalls.map((call) => (
                  <div className="approval-row" key={call.id}>
                    <div className="approval-info">
                      <code>{call.toolName}</code>
                      <p className="error">{call.error || '结果未知，等待处置'}</p>
                    </div>
                    <div className="approval-actions">
                      <button disabled={approvalBusy} onClick={() => resolveUnknown(call, true)}>确认为成功</button>
                      <button className="secondary" disabled={approvalBusy} onClick={() => resolveUnknown(call, false)}>确认为失败</button>
                    </div>
                  </div>
                ))}
                <p className="hint">批准只对当前这条调用的参数生效；结果未知的调用需要你说明依据后处置，会话才能继续。</p>
              </div>
            )}
            {activeRun && runs[0] && <p className="hint profile-run-hint">本次运行使用 Profile「{profileName(runs[0].profileId)}」快照 v{runs[0].profileRevision}；后续运行将使用「{activeAgent ? profileName(activeAgent.profileId) : '…'}」当前内容。</p>}
            <div className="message-list" aria-live="polite">
              {messages.length === 0 && !streamText && <p className="hint">还没有消息。可以试试“现在几点？”来验证 MCP 时间工具。</p>}
              {messages.map((item) => <div className={`message message-${item.role}`} key={item.id}><span className="message-role">{item.role === 'user' ? '你' : item.role === 'tool' ? 'MCP 工具' : 'ICEPick'}</span><p>{item.content}</p></div>)}
              {streamText && <div className="message message-assistant streaming"><span className="message-role">ICEPick · 生成中</span><p>{streamText}</p></div>}
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

        <p className="next-step">内置 MCP 工具：local__current_time、local__read_file、local__load_skill。工具调用受权限网关与审批约束；远端模型发送前会检查数据外发授权。</p>
      </main>
    </div>
  )
}

export default App
