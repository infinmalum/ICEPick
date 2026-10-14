import type { LocalContextState } from "../hooks/useLocalContext";
import type { Workspace } from "../hooks/useWorkspace";
type Props = {
  local: Pick<
    LocalContextState,
    | "contextBusy"
    | "contextDraft"
    | "contextError"
    | "saveContext"
    | "updatePermissions"
    | "updatePolicy"
  >;
  workspace: Pick<Workspace, "selected">;
};
export function ContextPolicyPanel(props: Props) {
  const {
    contextBusy,
    contextDraft,
    contextError,
    saveContext,
    updatePermissions,
    updatePolicy,
  } = props.local;
  const { selected } = props.workspace;
  return (
    contextDraft &&
    selected && (
      <details className="session-card context-config">
        <summary className="card-title">此对话的本机权限与记忆授权</summary>
        <form className="model-form" onSubmit={saveContext}>
          <label>
            审批模式
            <select
              value={contextDraft.policy.approvalMode}
              onChange={(event) =>
                updatePolicy({ approvalMode: event.target.value })
              }
            >
              <option value="manual">全人工</option>
              <option value="semi">半自动</option>
              <option value="automatic">全自动</option>
            </select>
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={contextDraft.policy.permissions.unrestricted}
              onChange={(event) =>
                updatePermissions({ unrestricted: event.target.checked })
              }
            />
            允许一般资源范围不限（敏感访问与外发仍单独检查）
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={contextDraft.policy.memory}
              onChange={(event) =>
                updatePolicy({ memory: event.target.checked })
              }
            />
            允许长期记忆（Profile 也须开启）
          </label>
          {!contextDraft.policy.permissions.unrestricted && (
            <>
              <label>
                允许的工作区路径（每行一个）
                <textarea
                  rows={2}
                  value={(
                    contextDraft.policy.permissions.workspacePaths ?? []
                  ).join("\n")}
                  onChange={(event) =>
                    updatePermissions({
                      workspacePaths: event.target.value
                        .split("\n")
                        .map((path) => path.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </label>
              <label>
                允许的可执行文件（每行一个精确名称或绝对路径）
                <textarea
                  rows={2}
                  value={(contextDraft.policy.permissions.commands ?? []).join(
                    "\n",
                  )}
                  onChange={(event) =>
                    updatePermissions({
                      commands: event.target.value
                        .split("\n")
                        .map((cmd) => cmd.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </label>
              <label>
                允许的网络目标（每行一个）
                <textarea
                  rows={2}
                  value={(
                    contextDraft.policy.permissions.networkHosts ?? []
                  ).join("\n")}
                  onChange={(event) =>
                    updatePermissions({
                      networkHosts: event.target.value
                        .split("\n")
                        .map((host) => host.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </label>
            </>
          )}
          <button type="submit" disabled={contextBusy}>
            保存本机权限
          </button>
        </form>
        {contextError && <p className="error">{contextError}</p>}
        <p className="hint">
          这些授权保存在本机，不进入 Profile
          分享包。显式沿用记忆与权限的对话共享此处设置。
        </p>
      </details>
    )
  );
}
