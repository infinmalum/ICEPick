import { useApp } from "./hooks/useApp";
import { Sidebar } from "./components/Sidebar";
import { RuntimeStatus } from "./components/RuntimeStatus";
import { ConversationPanel } from "./components/ConversationPanel";
import { ProfileTemplates } from "./components/ProfileTemplates";
import { ConversationProfile } from "./components/ConversationProfile";
import { ContextPolicyPanel } from "./components/ContextPolicyPanel";
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
        <ProfileTemplates profileState={app.profileState} />
        <ConversationProfile
          workspace={app.workspace}
          profileState={app.profileState}
          conversation={app.conversation}
        />
        <ContextPolicyPanel local={app.local} workspace={app.workspace} />
        <ProfileEditor profileState={app.profileState} />
        <ProfileImport profileState={app.profileState} />
        <ModelSettings runtime={app.runtime} />
        <CredentialsPanel runtime={app.runtime} />
        <ExfilPanel workspace={app.workspace} local={app.local} />
        <MemoryPanel workspace={app.workspace} local={app.local} />
        <p className="next-step">
          内置工具：local__current_time、local__read_file、local__write_file、local__edit_file、local__exec、local__tool_search、local__load_skill、local__memory_save、local__update_plan、local__task_plan、local__delegate_task，以及同根任务的
          worker
          过程检索与观察工具。工具调用受权限网关与审批约束；委派每次需确认；远端模型发送前会检查数据外发授权；长会话超预算时自动压缩旧轮次并保留原文。
        </p>
      </main>
    </div>
  );
}
export default App;
