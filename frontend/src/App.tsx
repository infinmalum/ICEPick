import { useApp } from "./hooks/useApp";
import { Sidebar } from "./components/Sidebar";
import { RuntimeStatus } from "./components/RuntimeStatus";
import { ConversationPanel } from "./components/ConversationPanel";
import { ProfileTemplates } from "./components/ProfileTemplates";
import { ConversationProfile } from "./components/ConversationProfile";
import { ContextPolicyPanel } from "./components/ContextPolicyPanel";
import { SkillDrafts } from "./components/SkillDrafts";
import { ProfileDrafts } from "./components/ProfileDrafts";
import { ProfileEditor } from "./components/ProfileEditor";
import { ProfileImport } from "./components/ProfileImport";
import { ModelSettings } from "./components/ModelSettings";
import { CredentialsPanel } from "./components/CredentialsPanel";
import { ExfilPanel } from "./components/ExfilPanel";
import { MemoryPanel } from "./components/MemoryPanel";
function App() {
  const app = useApp();
  return (
    <div className="shell">
      <Sidebar
        workspace={app.workspace}
        runtime={app.runtime}
        profileState={app.profileState}
        createSession={app.createSession}
      />
      <main className="content">
        <div className="eyebrow">AGENT HARNESS / DESKTOP</div>
        <h1>{app.workspace.activeConversation?.title ?? "ICEPick"}</h1>
        <p className="intro">
          选择 Profile
          开始对话，或从场景模板创建自己的配置。新对话默认隔离历史、记忆与权限。
        </p>
        <RuntimeStatus runtime={app.runtime} error={app.error} />
        <ConversationPanel
          workspace={app.workspace}
          conversation={app.conversation}
          profileState={app.profileState}
          runtime={app.runtime}
        />

        <ConversationProfile
          workspace={app.workspace}
          profileState={app.profileState}
          conversation={app.conversation}
        />
        <details className="settings-group">
          <summary>本次对话的权限与记忆</summary>
          <ContextPolicyPanel local={app.local} workspace={app.workspace} />
          <ExfilPanel workspace={app.workspace} local={app.local} />
          <MemoryPanel workspace={app.workspace} local={app.local} />
        </details>
        <details className="settings-group">
          <summary>Profile 管理 · 创建、编辑、导入与导出</summary>
          <ProfileTemplates profileState={app.profileState} />
          <ProfileEditor profileState={app.profileState} />
          <ProfileImport profileState={app.profileState} />
        </details>
        <details className="settings-group">
          <summary>改进建议 · Profile 与 Skill 草案</summary>
          <ProfileDrafts
            state={app.drafts}
            hasConversation={!!app.workspace.selected}
            targetName={app.profileState.profileDraft?.name ?? ""}
          />
          <SkillDrafts
            state={app.skillDrafts}
            hasConversation={!!app.workspace.selected}
          />
        </details>
        <details className="settings-group">
          <summary>模型与服务商设置</summary>
          <ModelSettings runtime={app.runtime} />
          <CredentialsPanel runtime={app.runtime} />
        </details>
      </main>
    </div>
  );
}
export default App;
