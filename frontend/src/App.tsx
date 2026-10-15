import { useEffect, useRef } from "react";
import { useApp } from "./hooks/useApp";
import { Sidebar } from "./components/Sidebar";
import { ConversationPanel } from "./components/ConversationPanel";
import { ProfileTemplates } from "./components/ProfileTemplates";
import { ProfileEditor } from "./components/ProfileEditor";
import { ProfileImport } from "./components/ProfileImport";
import { ModelSettings } from "./components/ModelSettings";
import { MemoryPanel } from "./components/MemoryPanel";
import { PrivacySettings } from "./components/PrivacySettings";
import { ContextPolicyPanel } from "./components/ContextPolicyPanel";
import { ExfilPanel } from "./components/ExfilPanel";
import { CredentialsPanel } from "./components/CredentialsPanel";
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
                <ProfileTemplates profileState={app.profileState} />
                <ProfileEditor profileState={app.profileState} />
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
                />
              </>
            ) : (
              <>
                <nav className="settings-tabs" aria-label="设置分类">
                  {[
                    ["model", "模型连接"],
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
                ) : app.settingsTab === "privacy" ? (
                  <>
                    <PrivacySettings local={app.local} workspace={w} />
                    <MemoryPanel local={app.local} workspace={w} />
                  </>
                ) : (
                  <>
                    <p>以下设置面向自定义工具和执行环境。普通使用无需修改。</p>
                    <details>
                      <summary>本机权限的完整配置</summary>
                      <ContextPolicyPanel local={app.local} workspace={w} />
                    </details>
                    <details>
                      <summary>内容来源与目的地授权</summary>
                      <ExfilPanel local={app.local} workspace={w} />
                    </details>
                    <details>
                      <summary>已保存的模型密钥</summary>
                      <CredentialsPanel runtime={app.runtime} />
                    </details>
                    <details>
                      <summary>扩展服务与运行状态</summary>
                      <pre>
                        {JSON.stringify(
                          {
                            status: app.runtime.status,
                            services: app.runtime.mcpServers,
                          },
                          null,
                          2,
                        )}
                      </pre>
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
