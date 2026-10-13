import { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { StatusService, type AppStatus } from '../bindings/icepick'
import type { TaskReview, TaskGraph, AgentPolicy, Conversation, LocalContext, ProfileTemplate, ProfileReference, ExfilGrant, Memory, Message, Profile, Run, SessionSummary, ToolCall } from '../bindings/icepick/internal/storage'
import type { CredentialStatus, ExportResult, ImportPreview, MCPEndpointStatus, SessionPlanView } from '../bindings/icepick/internal/app'

const blankRootBudget = {autoRetry:false,maxSeconds:0,maxModelCalls:0,maxToolCalls:0,maxTokens:0,maxPlannerRounds:0,maxAttempts:0,maxRetries:0}

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

function downloadBase64(data: string, filename: string, mime: string) {
  const binary = atob(data)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  const blob = new Blob([bytes], { type: mime })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

function App() {
  const [status, setStatus] = useState<AppStatus | null>(null)
  const [error, setError] = useState('')
  const [sessions, setSessions] = useState<Conversation[]>([])
  const [templates, setTemplates] = useState<ProfileTemplate[]>([])
  const [reuseContext, setReuseContext] = useState(false)
  const [restoreGroup, setRestoreGroup] = useState('')
  const [deleteReplacement, setDeleteReplacement] = useState('__own__')
  const [profileRefs, setProfileRefs] = useState<ProfileReference[]>([])
  const [agentID, setAgentID] = useState('default')
  const [agentDraft, setAgentDraft] = useState<LocalContext | null>(null)
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
  const [memories, setMemories] = useState<Memory[]>([])
  const [memoryDraft, setMemoryDraft] = useState('')
  const [memoryEdit, setMemoryEdit] = useState<{ id: string; content: string } | null>(null)
  const [memoryBusy, setMemoryBusy] = useState(false)
  const [memoryError, setMemoryError] = useState('')
  const [sessionSummary, setSessionSummary] = useState<SessionSummary | null>(null)
  const [sessionPlan, setSessionPlan] = useState<SessionPlanView | null>(null)
  const [reviews, setReviews] = useState<TaskReview[]>([])
  const [reviewBusy, setReviewBusy] = useState(false)
  const [taskGraph, setTaskGraph] = useState<TaskGraph | null>(null)
  const [childRuns, setChildRuns] = useState<Run[]>([])
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(null)
  const [importData, setImportData] = useState('')
  const [importBusy, setImportBusy] = useState(false)
  const importInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    StatusService.GetStatus()
      .then((value) => {
        setStatus(value)
        setModelDraft(value.model)
      })
      .catch((reason: unknown) => setError(String(reason)))
    StatusService.ListProfileTemplates().then((items) => setTemplates(items ?? [])).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListConversations().then((items) => setSessions(items ?? [])).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListProfiles().then((items) => { setProfiles(items ?? []); setProfileEditID(items?.[0]?.id ?? null) }).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListMCPEndpoints().then((items) => setMcpServers(items ?? [])).catch((reason: unknown) => setError(String(reason)))
    StatusService.ListCredentialStatus().then((items) => setCredStatus(items ?? [])).catch((reason: unknown) => setCredError(String(reason)))
  }, [])

  useEffect(() => {
 let current=true; setGrants([]); setGrantError('');
 StatusService.ListExfilGrants(agentID).then(items=>{if(current)setGrants(items ?? [])}).catch(reason=>{if(current)setGrantError(String(reason))});
 return ()=>{current=false};
 }, [agentID])

  // Long-term memory belongs to the agent; reload whenever the agent changes.
  useEffect(() => {
    let current = true
    setMemories([]); setMemoryError(''); setMemoryEdit(null)
    StatusService.ListMemories(agentID).then((items) => { if (current) setMemories(items ?? []) }).catch((reason) => { if (current) setMemoryError(String(reason)) })
    return () => { current = false }
  }, [agentID])

  // The compaction summary and the resumable plan are session facts; they
  // can appear or change while a run compacts or updates the plan.
  function refreshSessionKnowledge(id: string) {
    const current = () => activeAgentID.current === agentID && activeSessionID.current === id
    StatusService.GetSessionSummary(agentID, id).then(value => { if (current()) setSessionSummary(value) }).catch(() => { if (current()) setSessionSummary(null) })
    StatusService.GetSessionPlan(agentID, id).then(value => { if (current()) setSessionPlan(value) }).catch(() => { if (current()) setSessionPlan(null) })
  }

  const activeAgentID = useRef(agentID)
  activeAgentID.current = agentID
  const activeSessionID = useRef(selected)
  activeSessionID.current = selected

  const activeConversation = sessions.find((item) => item.id === selected) ?? null

  useEffect(() => {
    let current = true
    setAgentDraft(null)
    if (selected) StatusService.GetLocalContext(agentID).then((item) => { if (current) setAgentDraft(item) }).catch(reason => { if (current) setAgentError(String(reason)) })
    return () => { current = false }
  }, [agentID, selected])

  useEffect(() => {
    if (activeConversation && !activeConversation.profileArchived) setProfileEditID(activeConversation.profileId)
  }, [activeConversation?.id, activeConversation?.profileId])

  useEffect(() => {
    const target = profiles.find((item) => item.id === profileEditID) ?? null
    setProfileDraft(target ? structuredClone(target) : null)
  }, [profileEditID, profiles])

  useEffect(() => {
    let current = true
    setProfileRefs([])
    if (profileEditID) StatusService.ProfileReferences(profileEditID).then(refs => { if (current) setProfileRefs(refs ?? []) }).catch(() => {})
    return () => { current = false }
  }, [profileEditID, profiles])

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
    setTaskGraph(null); setReviews([])
    Promise.all([StatusService.ListMessages(agentID, selected), StatusService.ListRuns(agentID, selected), StatusService.ListToolCalls(agentID, selected)])
      .then(([history, previousRuns, calls]) => {
        if (cancelled) return
        setMessages(history ?? [])
        setRuns(previousRuns ?? [])
        setToolCalls(calls ?? [])
        setActiveRun((previousRuns ?? []).find((item) => item.status === 'running')?.id ?? null)
        setLoadingSession(false)
        refreshChildRuns((previousRuns ?? [])[0]?.id)
      })
      .catch((reason: unknown) => { if (!cancelled) { setTaskError(String(reason)); setLoadingSession(false) } })
    refreshSessionKnowledge(selected)
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
        refreshChildRuns(run.id)
        refreshSessionKnowledge(selected)
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

  async function refreshContext() {
    if (selected) { const item = await StatusService.GetLocalContext(agentID); if (activeAgentID.current === agentID) setAgentDraft(item) }
  }

  async function refreshConversations() {
    setSessions((await StatusService.ListConversations()) ?? [])
  }

  function openConversation(item: Conversation) {
    setAgentID(item.contextId)
    setSelected(item.id)
    setMessages([])
    setRuns([])
    setActiveRun(null)
    setStreamText('')
    setToolCalls([])
    setChildRuns([])
    setSessionSummary(null)
    setSessionPlan(null)
    setTaskError('')
    setAgentError('')
  }

  async function createTemplate(id: string) {
    setProfileBusy(true)
    try {
      const created = await StatusService.CreateTemplateProfile(id, '')
      await refreshProfiles()
      setProfileEditID(created.id)
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
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

  async function saveAgent(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!agentDraft || !selected) return
    setAgentBusy(true)
    try {
      await StatusService.UpdateContextPolicy(agentID, agentDraft.policy)
      await refreshContext()
      setAgentError('')
    } catch (reason) { setAgentError(String(reason)) } finally { setAgentBusy(false) }
  }

  async function selectConversationProfile(profileID: string) {
    if (!selected) return
    try {
      await StatusService.SelectConversationProfile(selected, profileID)
      await refreshConversations()
      setTaskError('')
    } catch (reason) { setTaskError(String(reason)) }
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
    if (!profileDraft) return
    setProfileBusy(true)
    try {
      const refs = (await StatusService.ProfileReferences(profileDraft.id)) ?? []
      const own = deleteReplacement === '__own__'
      const replacement = own ? '' : deleteReplacement
      const consequence = refs.length ? `\n被引用的 worker：${refs.map(ref => `${ref.name}（${(ref.workers ?? []).join('、')}）`).join('；')}。\n这些来源将${own ? '恢复为各自 Profile 的 worker 配置' : `替换为「${profileName(replacement)}」`}。` : ''
      if (!window.confirm(`删除 Profile「${profileDraft.name}」？历史与在途任务保留，后续任务需选择可用配置。${consequence}`)) return
      if (refs.length) await StatusService.ArchiveProfileReplacing(profileDraft.id, replacement, own, refs)
      else await StatusService.ArchiveProfile(profileDraft.id)
      await refreshProfiles()
      await refreshConversations()
      const remaining = (await StatusService.ListProfiles()) ?? []
      setProfileEditID(remaining[0]?.id ?? null)
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
  }

  async function restoreProfileDefault() {
    if (!profileDraft) return
    setProfileBusy(true)
    try {
      const restored = profileDraft.data.templateId
        ? await StatusService.RestoreTemplateProfile(profileDraft.id, profileDraft.revision, restoreGroup ? [restoreGroup] : [])
        : await StatusService.RestoreProfileDefault(profileDraft.revision)
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
      await StatusService.GrantExfil(agentID,grantForm.destination, grantForm.scopes)
      const updated = (await StatusService.ListExfilGrants(agentID)) ?? []
      if (activeAgentID.current !== agentID) return
      setGrants(updated)
      setGrantForm({ destination: '', scopes: [] })
      setGrantError('')
    } catch (reason) { setGrantError(String(reason)) } finally { setGrantBusy(false) }
  }

  async function revokeGrant(destination: string) {
    if (!window.confirm(`撤销向 ${destination} 发送会话数据的授权？后续远端发送将被拒绝。`)) return
    setGrantBusy(true)
    try {
      await StatusService.RevokeExfil(agentID,destination)
      const updated = (await StatusService.ListExfilGrants(agentID)) ?? []
      if (activeAgentID.current !== agentID) return
      setGrants(updated)
      setGrantError('')
    } catch (reason) { setGrantError(String(reason)) } finally { setGrantBusy(false) }
  }

  async function createSession() {
    setCreating(true)
    try {
      if (!profileEditID) throw new Error('请先创建或选择一个 Profile')
      const session = await StatusService.CreateConversation(profileEditID, reuseContext && selected && activeConversation?.kind !== 'delegation' ? selected : '', '')
      await refreshConversations()
      openConversation(session)
      setReuseContext(false)
      setError('')
    } catch (reason) {
      setError(String(reason))
    } finally {
      setCreating(false)
    }
  }

  async function refreshMemories(contextID: string) {
    const items = (await StatusService.ListMemories(contextID)) ?? []
    if (activeAgentID.current === contextID) setMemories(items)
  }

  async function addMemory(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!memoryDraft.trim()) return
    setMemoryBusy(true)
    try {
      await StatusService.AddMemory(agentID, memoryDraft)
      await refreshMemories(agentID)
      setMemoryDraft('')
      setMemoryError('')
    } catch (reason) { setMemoryError(String(reason)) } finally { setMemoryBusy(false) }
  }

  async function saveMemoryEdit() {
    if (!memoryEdit || !memoryEdit.content.trim()) return
    setMemoryBusy(true)
    try {
      await StatusService.UpdateMemory(agentID, memoryEdit.id, memoryEdit.content)
      await refreshMemories(agentID)
      setMemoryEdit(null)
      setMemoryError('')
    } catch (reason) { setMemoryError(String(reason)) } finally { setMemoryBusy(false) }
  }

  async function deleteMemory(id: string) {
    if (!window.confirm('删除这条记忆？删除后不可恢复。')) return
    setMemoryBusy(true)
    try {
      await StatusService.DeleteMemory(agentID, id)
      await refreshMemories(agentID)
      setMemoryError('')
    } catch (reason) { setMemoryError(String(reason)) } finally { setMemoryBusy(false) }
  }

  async function exportMemory() {
    try {
      const markdown = await StatusService.ExportMemories(agentID)
      const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `icepick-memory-${agentID}.md`
      anchor.click()
      URL.revokeObjectURL(url)
      setMemoryError('')
    } catch (reason) { setMemoryError(String(reason)) }
  }

  async function clearPlan() {
    if (!selected) return
    try {
      await StatusService.ClearSessionPlan(agentID, selected)
      setSessionPlan(null)
      setTaskError('')
    } catch (reason) { setTaskError(String(reason)) }
  }

  async function refreshChildRuns(latestRunID: string | undefined) {
    if (!latestRunID) {
      setTaskGraph(null); setReviews([])
      setChildRuns([])
      return
    }
    try {
      const sessionID = activeSessionID.current
      const [items, graph, reviewItems] = await Promise.all([StatusService.ListChildRuns(agentID, latestRunID), StatusService.GetTaskGraph(agentID, latestRunID).catch(() => null), StatusService.ListTaskReviews(agentID, latestRunID).catch(() => [])])
      if (activeAgentID.current === agentID && activeSessionID.current === sessionID) { setChildRuns(items ?? []); setTaskGraph(graph); setReviews(reviewItems ?? []) }
    } catch { setChildRuns([]) }
  }

  // A delegated subtask lives in a hidden session of the target Agent; the
  // task tree jumps there so its transcript and approvals become visible.
  async function openChildSession(child: Run) {
    try {
      const item = await StatusService.GetConversation(child.sessionId)
      setSessions(current => current.some(c => c.id === item.id) ? current : [item, ...current])
      openConversation(item)
    } catch (reason) { setTaskError(String(reason)) }
  }

  async function exportProfile() {
    if (!profileDraft) return
    setProfileBusy(true)
    try {
      const saved = await StatusService.GetProfile(profileDraft.id)
      const refs = (saved.data.execution.workerSources ?? []).filter(source => source.profileId)
      if (refs.length && !window.confirm(`分享包将包含这些 worker 来源的独立配置：${refs.map(source => `${source.name} → ${profileName(source.profileId)}`).join('；')}。不携带会话、记忆或本机授权。继续导出？`)) return
      const result: ExportResult = refs.length
        ? await StatusService.ExportProfileIncludingWorkers(profileDraft.id)
        : await StatusService.ExportProfile(profileDraft.id)
      downloadBase64(result.data, result.filename, 'application/zip')
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setProfileBusy(false) }
  }

  async function onImportFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    const bytes = new Uint8Array(await file.arrayBuffer())
    setImportBusy(true)
    try {
      const data = bytesToBase64(bytes)
      setImportPreview(await StatusService.PreviewProfileImport(data))
      setImportData(data)
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setImportBusy(false) }
  }

  async function confirmImport() {
    if (!importData) return
    setImportBusy(true)
    try {
      const result = await StatusService.ImportProfile(importData)
      await refreshProfiles()
      setProfileEditID(result.profile.id)
      setImportPreview(null)
      setImportData('')
      setProfileError('')
    } catch (reason) { setProfileError(String(reason)) } finally { setImportBusy(false) }
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


  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">◈</span> ICEPick</div>
        <div className="sidebar-label">工作区</div>
        <div className="sidebar-item active">概览</div>
        <div className="sidebar-label sessions-label">Profiles</div>
        {profiles.map((item) => (
          <button key={item.id} className={`sidebar-item session-item ${profileEditID === item.id ? 'selected' : ''}`} onClick={() => setProfileEditID(item.id)}>{item.name}</button>
        ))}
        <div className="sidebar-label sessions-label">对话</div>
        <button className="new-session" onClick={createSession} disabled={creating || !profileEditID || !status?.storageReady}>＋ 使用所选 Profile 开始对话</button>
        {selected && activeConversation?.kind !== 'delegation' && <label className="checkbox-label"><input type="checkbox" checked={reuseContext} onChange={event => setReuseContext(event.target.checked)} />新对话沿用当前记忆与权限</label>}
        {sessions.map((session) => (
          <button key={session.id} className={`sidebar-item session-item ${selected === session.id ? 'selected' : ''}`} onClick={() => openConversation(session)}>{session.title}<span className="agent-profile-hint"> · {session.profileName}{session.profileArchived ? '（已删除）' : ''}</span></button>
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
        <h1>{activeConversation?.title ?? 'ICEPick'}</h1>
        <p className="intro">选择 Profile 开始对话，或从场景模板创建自己的配置。新对话默认隔离历史、记忆与权限。</p>

        <section className="status-card" aria-live="polite">
          <div className="card-title">运行状态</div>
          {error ? (
            <p className="error">无法读取本地服务状态：{error}</p>
          ) : status ? (
            <>
              <div className="status-row"><span>本地存储</span><strong className={status.storageReady ? 'ok' : ''}>{status.storageReady ? '已就绪' : '异常'}</strong></div>
              <div className="status-row"><span>任务执行</span><strong className={status.agentReady ? 'ok' : ''}>{status.agentReady ? '已就绪' : '请配置模型'}</strong></div>
              <div className="status-row"><span>模型服务</span><strong className={status.modelConnected ? 'ok' : ''}>{status.modelConnected ? '已连接' : '未连接'}</strong></div>
              <p className="hint">{status.message}</p>
            </>
          ) : (
            <p className="hint">正在读取本地服务状态…</p>
          )}
        </section>

        <section className="session-card">
          <div className="card-title">{selected ? sessions.find((item) => item.id === selected)?.title : '空会话'}</div>
          {!selected ? <p className="hint">先新建或选择会话。</p> : <>
            {sessionSummary && <details className="summary-banner" open>
              <summary>已压缩历史 · 覆盖至消息 #{sessionSummary.coversSeq} · 范围 {scopeLabels[sessionSummary.scope] ?? sessionSummary.scope}（原文仍在下方记录中）</summary>
              <pre className="summary-text">{sessionSummary.summary}</pre>
            </details>}
            {sessionPlan && sessionPlan.steps && <div className="plan-panel">
              <div className="plan-head">
                <span className="card-title">当前计划 · 修订 {sessionPlan.revision}</span>
                <button type="button" className="secondary" onClick={clearPlan}>清除计划</button>
              </div>
              <ul className="plan-steps">
                {sessionPlan.steps.map((step, index) => (
                  <li key={step.id || index} className={`plan-step plan-${step.status}`}>
                    <span className="plan-marker">{step.status === 'done' ? '✓' : step.status === 'in_progress' ? '▶' : '·'}</span>
                    {step.title}{step.note && <span className="plan-note">（{step.note}）</span>}
                    <small className="plan-note"> ID: {step.id}</small>
                    {!!step.dependsOn?.length && <small className="plan-note"> 前置：{step.dependsOn.join('、')}</small>}
                    {step.acceptance && <small className="plan-note"> 验收：{step.acceptance}</small>}
                    {!!step.inputRefs?.length && <small className="plan-note"> 材料：{step.inputRefs.join('、')}</small>}
                  </li>
                ))}
              </ul>
              <p className="hint">前置步骤完成后才能开始后继，更新有修订保护。状态由模型记录，尚未接入自动调度或独立验收；工具调用仍走审批。</p>
            </div>}
            {runs[0] && runs[0].status !== 'running' && <button type="button" className="secondary" disabled={reviewBusy} onClick={async()=>{
              setReviewBusy(true);try { const run = await StatusService.StartConversationReview(selected!,runs[0].id); setRuns(current=>[run,...current]); setActiveRun(run.id) } catch(e) {setError(String(e))} finally {setReviewBusy(false)}
            }}>单次审查此结果</button>}
            {reviews.length > 0 && <div className="plan-panel">{reviews.map(review=><details key={review.id}><summary>独立审查：{review.result.status} · {review.current ? '当前交付' : '已失效'}</summary><p>{review.result.summary}</p><p>绑定指纹：{review.binding}</p>{(review.result.defects ?? []).map((d,i)=><p key={i}>{d.reference}：{d.description}</p>)}</details>)}</div>}
            {taskGraph && (!!taskGraph.tasks?.length || (taskGraph.coordination?.rounds ?? 0) > 0) && <div className="plan-panel task-graph">
              <span className="card-title">本次执行计划 · 修订 {taskGraph.revision}</span>
              <p>{taskGraph.goal}</p>
              {taskGraph.coordination && <p>调度：{taskGraph.coordination.state} · 规划 {taskGraph.coordination.rounds} 轮 · 事件 {taskGraph.coordination.plannedSeq}/{taskGraph.coordination.eventSeq}{taskGraph.coordination.reason && ` · ${taskGraph.coordination.reason}`}</p>}
              {taskGraph.budget && <p>根预算：模型 {taskGraph.budget.modelCalls}/{taskGraph.budget.limits.maxModelCalls} 次 · 工具 {taskGraph.budget.toolCalls}/{taskGraph.budget.limits.maxToolCalls} 次 · token {taskGraph.budget.tokens}{taskGraph.budget.unknownUsage > 0 && `（${taskGraph.budget.unknownUsage} 次用量未知，保留预留额度）`}</p>}
              {(taskGraph.tasks ?? []).map(item => <details key={item.spec.id}>
                <summary>{item.spec.id} · {item.spec.worker} · {item.state} · {item.spec.accessMode || 'unknown'}</summary>
                <p>{item.spec.task}</p>
                {item.spec.materials && <pre>{item.spec.materials}</pre>}
                {!!item.spec.dependsOn?.length && <p>前置任务：{item.spec.dependsOn.join('、')}</p>}
                {item.spec.acceptance && <p>验收条件：{item.spec.acceptance}</p>}
                {!!item.spec.resources?.length && <p>共享资源：{item.spec.resources.join('、')}</p>}
                {item.spec.replaces && <p>替代失败任务：{item.spec.replaces}</p>}
                {(taskGraph.artifacts ?? []).filter(a=>a.taskId===item.spec.id).map(a=><p key={a.id}>交付物 {a.id} · {a.verification} · SHA256 {a.sha256}</p>)}
                {(taskGraph.attempts ?? []).filter(a => a.taskId === item.spec.id).map(a => <p key={a.id}>执行尝试 {a.id} · {a.status} · 运行 {a.runId}</p>)}
              </details>)}
              <p className="hint">状态来自实际执行；succeeded 表示执行结束，独立审查尚未接入。领取前可展开查看任务条件；中断或结果未知的任务不会自动重跑。</p>
            </div>}
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
            {activeRun && runs[0] && <p className="hint profile-run-hint">本次运行使用 Profile「{profileName(runs[0].profileId)}」快照 v{runs[0].profileRevision}；后续运行将使用「{activeConversation?.profileArchived ? '请选择新 Profile' : activeConversation?.profileName ?? '…'}」当前内容。</p>}
            {(childRuns ?? []).length > 0 && <div className="task-tree">
              <span className="card-title">子任务（任务树）</span>
              {(childRuns ?? []).map((child) => (
                <div className="tree-child" key={child.id}>
                  <span className={`tree-state tree-${child.status}`}>{child.status}</span>
                  <span>{profileName(child.profileId)} · 深度 {child.depth}</span>
                  {child.error && <span className="tree-error">{child.error}</span>}
                  <button type="button" className="secondary" onClick={() => openChildSession(child)}>查看子会话</button>
                </div>
              ))}
              <p className="hint">子任务使用目标配置快照与独立上下文：不写入其普通会话，不访问其长期记忆；权限与外发取父任务与旧委派目标授权的交集，审批取更严格者。</p>
            </div>}
            <div className="message-list" aria-live="polite">
              {messages.length === 0 && !streamText && <p className="hint">还没有消息。可以试试“现在几点？”来验证 MCP 时间工具。</p>}
              {messages.map((item) => <div className={`message message-${item.role}`} key={item.id}><span className="message-role">{item.role === 'user' ? '你' : item.role === 'tool' ? 'MCP 工具' : 'ICEPick'}</span><p>{item.content}</p></div>)}
              {streamText && <div className="message message-assistant streaming"><span className="message-role">ICEPick · 生成中</span><p>{streamText}</p></div>}
            </div>
            <form className="task-form" onSubmit={sendTask}>
              <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="给 ICEPick 一个任务…" rows={3} />
              <div className="model-actions">
                <button type="submit" disabled={sending || loadingSession || !!activeRun || activeConversation?.kind === 'delegation' || activeConversation?.profileArchived || !status?.storageReady || !prompt.trim()}>发送任务</button>
                {activeRun && <button type="button" className="secondary" onClick={cancelTask}>取消任务</button>}
              </div>
            </form>
            {activeRun && <p className="hint">任务正在运行…</p>}
            {taskError && <p className="error">{taskError}</p>}
            {runs[0]?.status === 'failed' && taskGraph?.coordination?.state !== 'waiting' && <p className="error">任务失败：{runs[0].error}</p>}
            {taskGraph?.coordination?.state === 'waiting' && <p className="hint">等待补充信息；发送补充内容可继续对话，此前任务和执行记录会保留。</p>}
            {runs[0]?.status === 'cancelled' && <p className="hint">任务已取消。</p>}
          </>}
        </section>

        <section className="session-card">
          <div className="card-title">场景模板</div>
          <p className="hint">模板由程序提供，删除本地 Profile 后仍可重建。研究与开发为动态流程试验配置，包含必需独立审查；所有模板均待真实模型验收。</p>
          <div className="model-actions">
            {templates.map(item => <button key={item.id} type="button" className="secondary" disabled={profileBusy} onClick={() => createTemplate(item.id)}>＋ {item.name}</button>)}
            <button type="button" className="secondary" disabled={profileBusy} onClick={() => createProfile('')}>＋ 空白 Profile</button>
          </div>
          {!profileDraft && <p className="hint">请创建或选择 Profile；研究与开发模板需要提供实际材料或配置工具依赖。</p>}
          {!profileDraft && profileError && <p className="error">{profileError}</p>}
        </section>

        {activeConversation && <section className="session-card">
          <div className="card-title">此对话后续使用的 Profile</div>
          <select disabled={activeConversation.kind === 'delegation'} value={activeConversation.profileArchived ? '' : activeConversation.profileId} onChange={event => selectConversationProfile(event.target.value)}>
            <option value="" disabled>请选择可用 Profile</option>
            {profiles.map(item => <option key={item.id} value={item.id}>{item.name}（v{item.revision}）</option>)}
          </select>
          <p className="hint">切换保留此对话历史、记忆与权限；正在执行的任务使用启动时的配置。</p>
          {activeConversation.profileArchived && <p className="error">原 Profile 已删除；历史可继续查看，启动新任务前请选择可用配置。</p>}
        </section>}

        {agentDraft && selected && <details className="session-card agent-config">
          <summary className="card-title">此对话的本机权限与记忆授权</summary>
          <form className="model-form" onSubmit={saveAgent}>
            <label>审批模式<select value={agentDraft.policy.approvalMode} onChange={event => updatePolicy({ approvalMode: event.target.value })}>
              <option value="manual">全人工</option><option value="semi">半自动</option><option value="automatic">全自动</option>
            </select></label>
            <label className="checkbox-label"><input type="checkbox" checked={agentDraft.policy.permissions.unrestricted} onChange={event => updatePermissions({ unrestricted: event.target.checked })} />允许一般资源范围不限（敏感访问与外发仍单独检查）</label>
            <label className="checkbox-label"><input type="checkbox" checked={agentDraft.policy.memory} onChange={event => updatePolicy({ memory: event.target.checked })} />允许长期记忆（Profile 也须开启）</label>
            {!agentDraft.policy.permissions.unrestricted && <>
              <label>允许的工作区路径（每行一个）<textarea rows={2} value={(agentDraft.policy.permissions.workspacePaths ?? []).join('\n')} onChange={event => updatePermissions({ workspacePaths: event.target.value.split('\n').map(path => path.trim()).filter(Boolean) })} /></label>
              <label>允许的命令（每行一个）<textarea rows={2} value={(agentDraft.policy.permissions.commands ?? []).join('\n')} onChange={event => updatePermissions({ commands: event.target.value.split('\n').map(cmd => cmd.trim()).filter(Boolean) })} /></label>
              <label>允许的网络目标（每行一个）<textarea rows={2} value={(agentDraft.policy.permissions.networkHosts ?? []).join('\n')} onChange={event => updatePermissions({ networkHosts: event.target.value.split('\n').map(host => host.trim()).filter(Boolean) })} /></label>
            </>}
            <button type="submit" disabled={agentBusy}>保存本机权限</button>
          </form>
          {agentError && <p className="error">{agentError}</p>}
          <p className="hint">这些授权保存在本机，不进入 Profile 分享包。显式沿用记忆与权限的对话共享此处设置。</p>
        </details>}

        {profileDraft && <section className="session-card">
          <div className="card-title">Profile「{profileDraft.name}」 <span className="revision">v{profileDraft.revision}{profileDraft.data.templateId ? ' · 场景模板副本' : ''}</span></div>
          <label className="model-form">编辑目标
            <select value={profileEditID ?? ''} onChange={(event) => setProfileEditID(event.target.value)}>
              {profiles.map((item) => <option key={item.id} value={item.id}>{item.name}{item.data.templateId ? '（模板副本）' : ''}</option>)}
            </select>
          </label>
          <div className="model-actions">
            <button type="button" className="secondary" disabled={profileBusy} onClick={() => createProfile('')}>＋ 空白 Profile</button>
            <button type="button" className="secondary" disabled={profileBusy} onClick={() => createProfile(profileDraft.id)}>复制当前</button>
            <button type="button" className="secondary" disabled={profileBusy} onClick={exportProfile}>导出分享包</button>
            <button type="button" className="secondary" disabled={profileBusy} onClick={() => importInputRef.current?.click()}>导入分享包…</button>
            <input ref={importInputRef} type="file" accept=".zip,application/zip" style={{ display: 'none' }} onChange={onImportFile} />
            {profileRefs.length > 0 && <label>删除时替代引用<select value={deleteReplacement} onChange={event => setDeleteReplacement(event.target.value)}>
              <option value="__own__">恢复引用者自身的 worker 配置</option>
              {profiles.filter(item => item.id !== profileDraft.id).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select></label>}
            <button type="button" className="secondary" disabled={profileBusy} onClick={deleteProfile}>删除</button>
            {(profileDraft.data.templateId || profileDraft.isDefault) && <>
              {profileDraft.data.templateId && <select aria-label="恢复配置组" value={restoreGroup} onChange={event => setRestoreGroup(event.target.value)}><option value="">全部配置</option>{['persona', 'prompt', 'model', 'capabilities', 'runtime', 'execution'].map(group => <option key={group} value={group}>{({persona:'人格与说明',prompt:'提示词',model:'模型',capabilities:'工具与 Skills',runtime:'运行偏好',execution:'组件设置'} as Record<string,string>)[group]}</option>)}</select>}
              <button type="button" className="secondary" disabled={profileBusy} onClick={restoreProfileDefault}>恢复出厂配置</button>
            </>}
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
            <label>人格<textarea value={profileDraft.data.persona} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, persona: event.target.value } })} /></label>
            <label>上下文预算（token）<input type="number" min={1024} max={1000000} value={profileDraft.data.contextBudget} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, contextBudget: Number(event.target.value) } })} /></label>
            <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.memoryEnabled} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, memoryEnabled: event.target.checked } })} />启用长期记忆（运行开始时检索，模型可用 local__memory_save 写入）</label>
            <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.planEnabled} onChange={(event) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, planEnabled: event.target.checked } })} />启用任务规划（会话待办；配置 worker 后可声明本次执行计划）</label>
            <details><summary>高级：执行组件</summary>
              <p className="hint">当前执行流程为直接执行。main 和 worker 共用运行循环；规划、独立审查、并行调度与配置改进将在后续阶段提供。</p>
              <label>执行流程<select value={profileDraft.data.execution.workflow} onChange={event => {
                const dynamic = event.target.value === 'dynamic'
                setProfileDraft({...profileDraft,data:{...profileDraft.data,planEnabled:dynamic || profileDraft.data.planEnabled,delegation:{...profileDraft.data.delegation,enabled:dynamic || profileDraft.data.delegation.enabled,maxDepth:dynamic ? Math.max(1,profileDraft.data.delegation.maxDepth):profileDraft.data.delegation.maxDepth},execution:{...profileDraft.data.execution,workflow:event.target.value,workerSources:dynamic && !(profileDraft.data.execution.workerSources ?? []).length ? [{name:'worker1',profileId:'',configuration:null}] : profileDraft.data.execution.workerSources}}})
              }}><option value="direct">直接执行</option><option value="dynamic">动态规划与协作</option></select></label>
              <p className="hint">动态流程由 planner 规划、宿主按依赖调度、main 汇总。只读任务可以并行；写入和未知副作用任务串行。每次派发 worker 仍需审批。</p>
              <div className="card-title">根任务预算（0 使用默认值；token 为 0 时仅统计）</div>
              <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.execution.budget?.autoRetry ?? false} onChange={event=>setProfileDraft({...profileDraft,data:{...profileDraft.data,execution:{...profileDraft.data.execution,budget:{...(profileDraft.data.execution.budget ?? blankRootBudget),autoRetry:event.target.checked}}}})} />允许无副作用失败任务的有界自动重试</label>
              {(['maxSeconds','maxModelCalls','maxToolCalls','maxTokens','maxPlannerRounds','maxAttempts','maxRetries'] as const).map(key => <label key={key}>{({maxSeconds:'总秒数',maxModelCalls:'模型调用次数',maxToolCalls:'工具调用次数',maxTokens:'token 预算',maxPlannerRounds:'规划轮次',maxAttempts:'执行尝试次数',maxRetries:'每条链的重试次数'})[key]}<input type="number" min={0} value={profileDraft.data.execution.budget?.[key] ?? 0} onChange={event => setProfileDraft({...profileDraft,data:{...profileDraft.data,execution:{...profileDraft.data.execution,budget:{...(profileDraft.data.execution.budget ?? blankRootBudget),[key]:Number(event.target.value)}}}})} /></label>)}
              <label>独立审查<select value={profileDraft.data.execution.review?.mode || 'off'} onChange={event=>setProfileDraft({...profileDraft,data:{...profileDraft.data,execution:{...profileDraft.data.execution,review:{...(profileDraft.data.execution.review ?? {requiredChecks:[]}),mode:event.target.value}}}})}><option value="off">关闭</option><option value="on_demand">按需（手动审查结果）</option><option value="required">必需（未通过不能完成）</option></select></label>
              <label>必要检查工具（逗号分隔）<input value={(profileDraft.data.execution.review?.requiredChecks ?? []).join(',')} onChange={event=>setProfileDraft({...profileDraft,data:{...profileDraft.data,execution:{...profileDraft.data.execution,review:{mode:profileDraft.data.execution.review?.mode || 'off',requiredChecks:event.target.value.split(',').map(x=>x.trim()).filter(Boolean)}}}})} placeholder="例如 tests__run" /></label>
              <p className="hint">审查依据交付物和已有工具记录。必要检查需由执行组件完成并返回 JSON passed:true；未执行或结果未知时无法判定。当前版本不会自动返工。</p>
              {(['main', 'planner', 'worker', 'reviewer'] as const).map(role => {
                const component = profileDraft.data.execution[role] ?? {systemPrompt:"",restrictTools:false,allowedTools:[],model:null}
                const update = (patch: Partial<typeof component>) => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, execution: { ...profileDraft.data.execution, [role]: { ...component, ...patch } } } })
                return <div key={role} className="model-form">
                  <strong>{role === 'main' ? '主执行' : role === 'planner' ? '规划（仅生成计划）' : role === 'reviewer' ? '独立审查（无工具）' : '子任务执行'}</strong>
                  <label>组件提示词（留空使用公共提示词）<textarea rows={3} value={component.systemPrompt} onChange={event => update({ systemPrompt: event.target.value })} /></label>
                  <label>组件模型<select value={component.model?.provider ?? ''} onChange={event => update({ model: event.target.value ? { provider: event.target.value, baseUrl: providerDefaults[event.target.value] ?? '', name: '' } : null })}>
                    <option value="">使用 Profile 公共模型</option>{Object.entries(providerLabels).map(([id, name]) => <option key={id} value={id}>{name}</option>)}
                  </select></label>
                  {component.model && <><label>模型地址<input value={component.model.baseUrl} onChange={event => update({ model: { ...component.model!, baseUrl: event.target.value } })} /></label><label>模型 ID<input value={component.model.name} onChange={event => update({ model: { ...component.model!, name: event.target.value } })} /></label></>}
                  {role !== 'planner' && role !== 'reviewer' && <><label className="checkbox-label"><input type="checkbox" checked={component.restrictTools} onChange={event => update({ restrictTools: event.target.checked })} />限制为公共能力中的工具子集</label>
                  {component.restrictTools && <label>允许的工具（每行一个，空列表表示不允许工具）<textarea rows={2} value={(component.allowedTools ?? []).join('\n')} onChange={event => update({ allowedTools: event.target.value.split('\n').map(name => name.trim()).filter(Boolean) })} /></label>}</>}
                </div>
              })}
              <label className="checkbox-label"><input type="checkbox" checked={profileDraft.data.delegation.enabled} onChange={event => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, delegation: { ...profileDraft.data.delegation, enabled: event.target.checked, maxDepth: event.target.checked ? Math.max(1, profileDraft.data.delegation.maxDepth) : profileDraft.data.delegation.maxDepth } } })} />允许调用 worker（受对话授权与审批策略约束）</label>
              <div className="card-title">Worker 配置来源</div>
              <p className="hint">选择当前 Profile 的 worker 或其他 Profile 的 worker 配置。任务开始时固定配置和 Skill 内容；子任务只使用明确材料和此对话的权限，不读取配置来源的历史或记忆。</p>
              {(profileDraft.data.execution.workerSources ?? []).map((source, index) => <div key={index} className="model-form">
                <label>Worker 名称<input value={source.name} onChange={event => {
                  const sources = [...(profileDraft.data.execution.workerSources ?? [])]; sources[index] = { ...source, name: event.target.value }
                  setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, execution: { ...profileDraft.data.execution, workerSources: sources } } })
                }} /></label>
                <label>配置来源<select value={source.configuration ? '__bundled__' : source.profileId} onChange={event => {
                  const sources = [...(profileDraft.data.execution.workerSources ?? [])]; sources[index] = { ...source, profileId: event.target.value, configuration: null }
                  setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, execution: { ...profileDraft.data.execution, workerSources: sources } } })
                }}>
                  {source.configuration && <option value="__bundled__" disabled>包内配置：{source.configuration.name}</option>}
                  <option value="">当前 Profile 的 worker</option>
                  {profiles.filter(item => item.id !== profileDraft.id).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select></label>
                <button type="button" className="secondary" onClick={() => setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, execution: { ...profileDraft.data.execution, workerSources: (profileDraft.data.execution.workerSources ?? []).filter((_, i) => i !== index) } } })}>移除此 worker</button>
              </div>)}
              <button type="button" className="secondary" disabled={(profileDraft.data.execution.workerSources ?? []).length >= 8} onClick={() => {
                const current = profileDraft.data.execution.workerSources ?? []; let n = 1; while (current.some(source => source.name === `worker${n}`)) n++
                setProfileDraft({ ...profileDraft, data: { ...profileDraft.data, execution: { ...profileDraft.data.execution, workerSources: [...current, { name: `worker${n}`, profileId: '', configuration: null }] } } })
              }}>＋ 添加 worker</button>
              <p className="hint">当前委派按调用顺序同步执行。旧委派目标继续保留原本机授权；新的 worker 来源不需要创建持久 Agent。</p>
            </details>
            <div className="model-actions"><button type="submit" disabled={profileBusy}>保存 Profile</button></div>
          </form>
          {profileError && <p className="error">{profileError}</p>}
          <p className="hint">Profile 可复制分享；保存以新修订原子发布，仅影响后续任务。分享包只承载配置与 Skills 文件，不含授权、会话、记忆、密钥或本机委派目标。</p>
        </section>}

        {importPreview && <section className="session-card">
          <div className="card-title">导入预览「{importPreview.name}」</div>
          <div className="import-preview">
            <p>模型：{importPreview.model.provider || '（应用默认）'}{importPreview.model.name ? `/${importPreview.model.name}` : ''} · 上下文预算 {importPreview.contextBudget} tokens · 人格 {importPreview.personaChars} 字符 · 提示词 {importPreview.promptChars} 字符</p>
            <p>委派：{importPreview.delegation.enabled ? `启用（深度 ${importPreview.delegation.maxDepth} · 并发 ${importPreview.delegation.maxConcurrency || 2} · ${importPreview.delegation.maxSeconds > 0 ? `${importPreview.delegation.maxSeconds} 秒` : '随运行上限'}）` : '关闭'} · 记忆偏好：{importPreview.memoryEnabled ? '开（需接收方 本机记忆授权才生效）' : '关'} · 规划：{importPreview.planEnabled ? '开' : '关'}</p>
            {importPreview.mcpServers && importPreview.mcpServers.length > 0 && <ul className="import-list">
              {importPreview.mcpServers.map((server) => <li key={server.name}>MCP {server.name}（命令 {server.command}）{server.configured ? ' · 已接入本机' : ' · 未接入本机，需自行配置'}</li>)}
            </ul>}
            {importPreview.skills && importPreview.skills.length > 0 && <ul className="import-list">
              {importPreview.skills.map((skill) => <li key={skill.name}>Skill {skill.name}{skill.bundled ? ' · 包内包含文件' : ''}{skill.installed ? ' · 已安装' : ' · 未安装，导入不会自动安装'}</li>)}
            </ul>}
            {(importPreview.workers ?? []).length > 0 && <ul className="import-list">{(importPreview.workers ?? []).map(worker => <li key={worker.name}>Worker {worker.name} → {worker.source}{worker.bundled ? '（独立配置副本）' : ''}</li>)}</ul>}
            {(importPreview.warnings ?? []).map((warning, index) => <p className="hint" key={index}>注意：{warning}</p>)}
          </div>
          <div className="model-actions">
            <button type="button" disabled={importBusy} onClick={confirmImport}>确认导入为新 Profile</button>
            <button type="button" className="secondary" disabled={importBusy} onClick={() => { setImportPreview(null); setImportData('') }}>取消</button>
          </div>
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

        {selected && <section className="session-card">
          <div className="card-title">数据外发授权 · {activeConversation?.title ?? '未选择对话'}</div>
          <p className="hint">授权用于当前对话的本机上下文。远端发送检查会话累计来源、提示词及工具定义；本机处理也保留来源限制，供之后切换远端时检查。显式沿用权限的对话共享此设置。</p>
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
        </section>}

        {selected && <section className="session-card">
          <div className="card-title">长期记忆 · {activeConversation?.title ?? '未选择对话'}</div>
          <p className="hint">记忆按本机上下文归属：切换 Profile 不清空、共享 Profile 不共享、复制 Profile 不携带。记忆会注入后续运行的提示词，其数据范围计入外发检查；关闭记忆只停止读写，内容保留到你手动删除。</p>
          {(memories ?? []).length === 0 && <p className="hint">还没有长期记忆。模型在获准运行中可调用 local__memory_save 保存，也可以在下方手动添加。</p>}
          {(memories ?? []).map((memory) => (
            <div className="memory-row" key={memory.id}>
              {memoryEdit && memoryEdit.id === memory.id ? (
                <div className="memory-edit">
                  <textarea rows={2} value={memoryEdit.content} onChange={(event) => setMemoryEdit({ id: memory.id, content: event.target.value })} />
                  <div className="model-actions">
                    <button type="button" disabled={memoryBusy || !memoryEdit.content.trim()} onClick={saveMemoryEdit}>保存修改</button>
                    <button type="button" className="secondary" onClick={() => setMemoryEdit(null)}>取消</button>
                  </div>
                </div>
              ) : (
                <>
                  <p className="memory-content">{memory.content}</p>
                  <span className={`memory-meta ${memory.scope === 'credential' || memory.scope === 'unknown' ? 'sensitive' : ''}`}>{scopeLabels[memory.scope] ?? memory.scope} · {memory.source === 'model' ? '模型保存' : '手动添加'}</span>
                  <span className="memory-actions">
                    <button type="button" className="secondary" onClick={() => setMemoryEdit({ id: memory.id, content: memory.content })}>编辑</button>
                    <button type="button" className="secondary" disabled={memoryBusy} onClick={() => deleteMemory(memory.id)}>删除</button>
                  </span>
                </>
              )}
            </div>
          ))}
          <form className="model-form" onSubmit={addMemory}>
            <label>添加记忆<textarea rows={2} value={memoryDraft} onChange={(event) => setMemoryDraft(event.target.value)} placeholder="例如：回复尽量使用简体中文" /></label>
            <div className="model-actions">
              <button type="submit" disabled={memoryBusy || !memoryDraft.trim()}>添加记忆</button>
              <button type="button" className="secondary" disabled={memoryBusy || (memories ?? []).length === 0} onClick={exportMemory}>导出 Markdown</button>
            </div>
          </form>
          {memoryError && <p className="error">{memoryError}</p>}
        </section>}



        <p className="next-step">内置工具：local__current_time、local__read_file、local__load_skill、local__memory_save、local__update_plan、local__task_plan、local__delegate_task，以及同根任务的 worker 过程检索与观察工具。工具调用受权限网关与审批约束；委派每次需确认；远端模型发送前会检查数据外发授权；长会话超预算时自动压缩旧轮次并保留原文。</p>
      </main>
    </div>
  )
}

export default App
