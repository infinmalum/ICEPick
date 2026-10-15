import type { LocalContextState } from "../hooks/useLocalContext";
import type { Workspace } from "../hooks/useWorkspace";
import { ErrorNotice } from "../shared/errors";
export function PrivacySettings({
  local: l,
  workspace: w,
}: {
  local: LocalContextState;
  workspace: Workspace;
}) {
  return (
    <section className="session-card">
      <h2>当前对话的权限与记忆</h2>
      {!w.selected ? (
        <p>开始对话后，可以在这里管理它的权限。</p>
      ) : (
        <>
          <p>{w.activeConversation?.title}</p>
          {l.contextDraft && (
            <form className="model-form" onSubmit={l.saveContext}>
              <label>
                操作确认方式
                <select
                  value={l.contextDraft.policy.approvalMode}
                  onChange={(e) =>
                    l.updatePolicy({ approvalMode: e.target.value })
                  }
                >
                  <option value="semi">推荐：重要操作先询问</option>
                  <option value="manual">每次操作都询问</option>
                  {l.contextDraft.policy.approvalMode === "automatic" && (
                    <option value="automatic">自定义自动执行策略</option>
                  )}
                </select>
                <small>
                  文件修改、命令与敏感操作继续受本机权限和逐次检查限制。
                </small>
              </label>
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={l.contextDraft.policy.memory}
                  onChange={(e) => l.updatePolicy({ memory: e.target.checked })}
                />
                允许这个对话使用记忆
              </label>
              <p>
                已允许的文件夹：
                {l.contextDraft.policy.permissions.workspacePaths?.join("、") ||
                  "尚未选择。可在聊天输入框旁选择文件夹。"}
              </p>
              <button disabled={l.contextBusy}>保存偏好</button>
            </form>
          )}
          <h3>内容发送授权</h3>
          <p>发送到远端模型前会说明接收地址。你可以随时撤销。</p>
          {!l.grants.length && <p>尚未允许向远端发送此对话。</p>}
          {l.grants.map((g) => (
            <div className="permission-row" key={g.destination}>
              <span>{g.destination}</span>
              <button
                className="secondary"
                disabled={l.grantBusy}
                onClick={() => l.revokeGrant(g.destination)}
              >
                撤销
              </button>
            </div>
          ))}
          <ErrorNotice error={l.contextError || l.grantError} />
        </>
      )}
    </section>
  );
}
