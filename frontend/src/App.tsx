import { useEffect, useRef } from "react";
import { useApp } from "./hooks/useApp";
import { Sidebar } from "./components/Sidebar";
import { ConversationPanel } from "./components/ConversationPanel";
import { ProfileTemplates } from "./components/ProfileTemplates";
import { ProfileList } from "./components/ProfileList";
import { ProfileAdvanced } from "./components/ProfileAdvanced";
import { ProfileEditor } from "./components/ProfileEditor";
import { ProfileImport } from "./components/ProfileImport";
import { ModelSettings } from "./components/ModelSettings";
import { MemoryPanel } from "./components/MemoryPanel";
import { PrivacySettings } from "./components/PrivacySettings";
import { ContextPolicyPanel } from "./components/ContextPolicyPanel";
import { ExfilPanel } from "./components/ExfilPanel";
import { MCPServersPanel } from "./components/MCPServersPanel";
import { HostActivityPanel } from "./components/HostActivityPanel";
import { ProfileDrafts } from "./components/ProfileDrafts";
import { SkillDrafts } from "./components/SkillDrafts";
import { ErrorNotice } from "./shared/errors";
import { StatusService } from "../bindings/icepick";
function App() {
  const app = useApp();
  const boot = useRef(false);
  const w = app.workspace;
  useEffect(() => {
    if (!w.loaded || boot.current || !app.runtime.status?.storageReady) return;
    boot.current = true;
    if (!w.sessions.length) void app.createSession();
  }, [w.loaded, app.runtime.status?.storageReady]);
  const openModel = () => {
    app.setSettingsTab("model");
    app.setPage("settings");
  };
  const tasks = app.conversation.runs
    .filter(
      (r) =>
        r.component === "main" &&
        !r.parentRunId &&
        r.status !== "running" &&
        app.conversation.messages.some(
          (m) => m.runId === r.id && m.role === "user",
        ),
    )
    .map((r) => ({
      id: r.id,
      label:
        (app.conversation.messages
          .find((m) => m.runId === r.id && m.role === "user")
          ?.content.slice(0, 64) || "最近的工作") +
        " · " +
        new Date(r.createdAt).toLocaleString(),
    }));
  async function improveLatest() {
    const target = w.activeConversation;
    const run = app.conversation.runs[0];
    if (!target || target.profileArchived || !run) return;
    if (!(await app.profileState.setProfileEditID(target.profileId))) return;
    app.drafts.setImprove(true);
    app.drafts.setEvidenceIDs(run.id);
    app.drafts.setRequest(
      "根据这次工作的实际过程，提出能检查、可调整的助手改进建议。",
    );
    app.setPage("assistants");
  }
  async function useMethod(name: string) {
    const profile = app.profileState.profileDraft;
    if (!profile) return;
    if (app.profileState.dirty) {
      await app.profileState.reportError("请先保存助手修改，再添加工作方法。");
      return;
    }
    try {
      await StatusService.UpdateProfile({
        ...profile,
        data: {
          ...profile.data,
          enabledSkills: [
            ...new Set([...(profile.data.enabledSkills ?? []), name]),
          ],
        },
      });
      await app.profileState.refreshProfiles();
    } catch (e) {
      await app.profileState.reportError(e);
    }
  }
  return (
    <div className="shell">
      <Sidebar
        workspace={w}
        page={app.page}
        setPage={app.setPage}
        createSession={app.createSession}
      />
      <main className="content">
        {app.page === "chat" ? (
          <>
            <ErrorNotice
              error={
                app.runtime.status && !app.runtime.status.storageReady
                  ? app.runtime.status.message
                  : app.error
              }
            />
            <ConversationPanel
              workspace={w}
              conversation={app.conversation}
              profileState={app.profileState}
              runtime={app.runtime}
              openModel={openModel}
              openAssistants={() => app.setPage("assistants")}
              onImprove={improveLatest}
            />
          </>
        ) : (
          <div className="settings-page">
            <header className="page-header">
              <div>
                <p className="eyebrow">ICEPick</p>
                <h1>{app.page === "assistants" ? "你的助手" : "设置"}</h1>
              </div>
              <button className="secondary" onClick={() => app.setPage("chat")}>
                返回对话
              </button>
            </header>
            <ErrorNotice
              error={
                app.runtime.status && !app.runtime.status.storageReady
                  ? app.runtime.status.message
                  : app.error
              }
            />
            {app.page === "assistants" ? (
              <>
                <ProfileList
                  profileState={app.profileState}
                  startConversation={(id) => void app.createSession(id)}
                />
                <ProfileTemplates profileState={app.profileState} />
                <ProfileEditor
                  profileState={app.profileState}
                  skillCatalog={app.skillCatalog}
                  mcpCatalog={app.mcpCatalog}
                  openDeveloper={() => {
                    app.setSettingsTab("developer");
                    app.setPage("settings");
                  }}
                  openExtensions={() => {
                    app.setSettingsTab("extensions");
                    app.setPage("settings");
                  }}
                />
                <button
                  disabled={
                    !app.profileState.profileEditID ||
                    app.profileState.dirty ||
                    w.creating
                  }
                  onClick={() =>
                    void app.createSession(
                      app.profileState.profileEditID ?? undefined,
                    )
                  }
                >
                  使用这个助手开始新对话
                </button>
                <p className="hint">
                  编辑助手不会切换当前对话。聊天顶部可以切换当前助手；这里会开启独立的新对话。
                </p>
                {app.profileState.dirty && (
                  <p className="unsaved-note" role="status">
                    有未保存的助手修改，切换页面时会保留草稿。
                  </p>
                )}
                <ProfileImport profileState={app.profileState} />
                <ProfileDrafts
                  state={app.drafts}
                  hasConversation={!!w.selected}
                  targetName={app.profileState.profileDraft?.name ?? ""}
                  tasks={tasks}
                />
                <SkillDrafts
                  state={app.skillDrafts}
                  hasConversation={!!w.selected}
                  tasks={tasks}
                  useMethod={useMethod}
                  skillNames={app.skillCatalog.map((item) => item.name)}
                />
              </>
            ) : (
              <>
                <nav className="settings-tabs" aria-label="设置分类">
                  {[
                    ["model", "模型连接"],
                    ["extensions", "技能与服务"],
                    ["privacy", "权限与记忆"],
                    ["developer", "开发者设置"],
                  ].map(([id, label]) => (
                    <button
                      className={
                        app.settingsTab === id ? "selected" : "secondary"
                      }
                      key={id}
                      onClick={() => app.setSettingsTab(id)}
                    >
                      {label}
                    </button>
                  ))}
                </nav>
                {app.settingsTab === "model" ? (
                  <ModelSettings
                    runtime={app.runtime}
                    onConnected={() => app.setPage("chat")}
                  />
                ) : app.settingsTab === "extensions" ? (
                  <>
                    <section className="session-card">
                      <h2>已安装的 Skills</h2>
                      {app.skillCatalog.length ? (
                        <div className="capability-list">
                          {app.skillCatalog.map((skill) => (
                            <div className="checkbox-label" key={skill.name}>
                              {skill.name}
                              {skill.managed && (
                                <span className="memory-meta">（AI 生成）</span>
                              )}
                              <span className="memory-meta">
                                {skill.description}
                              </span>
                            </div>
                          ))}
                        </div>
                      ) : (
                        <p className="hint">
                          本机还没有已安装的 Skills。AI
                          可以在对话中起草并发布新的 Skill。
                        </p>
                      )}
                      <p className="hint">
                        每个助手能用哪些 Skills
                        和服务，在助手编辑页的「技能与扩展」里勾选。
                      </p>
                    </section>
                    <MCPServersPanel
                      servers={app.mcpCatalog}
                      runtime={app.runtime}
                      onChanged={app.refreshCapabilities}
                    />
                    {app.catalogError && (
                      <ErrorNotice error={app.catalogError} />
                    )}
                  </>
                ) : app.settingsTab === "privacy" ? (
                  <>
                    <PrivacySettings
                      local={app.local}
                      workspace={w}
                      searchAvailability={app.conversation.searchAvailability}
                    />
                    <MemoryPanel local={app.local} workspace={w} />
                  </>
                ) : (
                  <>
                    <p>集中管理助手的执行配置与本机高级选项。</p>
                    <ProfileAdvanced profileState={app.profileState} />
                    <button
                      className="secondary"
                      onClick={() => app.setPage("assistants")}
                    >
                      返回助手编辑
                    </button>
                    <HostActivityPanel activity={app.activity} />
                    <details>
                      <summary>本机权限的完整配置</summary>
                      <ContextPolicyPanel local={app.local} workspace={w} />
                    </details>
                    <details>
                      <summary>内容来源与目的地授权</summary>
                      <ExfilPanel local={app.local} workspace={w} />
                    </details>
                    <label className="checkbox-label">
                      <input
                        type="checkbox"
                        checked={w.reuseContext}
                        onChange={(e) => w.setReuseContext(e.target.checked)}
                      />
                      下一个新对话沿用当前记忆与权限
                    </label>
                  </>
                )}
              </>
            )}
          </div>
        )}
      </main>
    </div>
  );
}
export default App;
