import { ErrorNotice } from "../shared/errors";
import type { Workspace } from "../hooks/useWorkspace";
import type { LocalContextState } from "../hooks/useLocalContext";
import { scopeLabels } from "../shared/config";
type Props = {
  workspace: Pick<Workspace, "activeConversation" | "selected">;
  local: Pick<
    LocalContextState,
    | "grantBusy"
    | "grantError"
    | "grantForm"
    | "grants"
    | "revokeGrant"
    | "saveGrant"
    | "setGrantForm"
  >;
};
export function ExfilPanel(props: Props) {
  const { activeConversation, selected } = props.workspace;
  const {
    grantBusy,
    grantError,
    grantForm,
    grants,
    revokeGrant,
    saveGrant,
    setGrantForm,
  } = props.local;
  return (
    selected && (
      <section className="session-card">
        <div className="card-title">
          数据外发授权 · {activeConversation?.title ?? "未选择对话"}
        </div>
        <p className="hint">
          授权用于当前对话的本机上下文。远端发送检查会话累计来源、提示词及工具定义；本机处理也保留来源限制，供之后切换远端时检查。显式沿用权限的对话共享此设置。
        </p>
        {(grants ?? []).length === 0 && (
          <p className="hint">
            还没有外发授权。配置远端模型并首次发送时，会在此提示需要授权。
          </p>
        )}
        {(grants ?? []).map((grant) => (
          <div className="grant-row" key={grant.destination}>
            <code>{grant.destination}</code>
            <span className="grant-scopes">
              {(grant.scopes ?? [])
                .map((scope) => scopeLabels[scope] ?? scope)
                .join("、") || "无范围"}
            </span>
            <button
              type="button"
              className="secondary"
              disabled={grantBusy}
              onClick={() => revokeGrant(grant.destination)}
            >
              撤销
            </button>
          </div>
        ))}
        <form className="model-form" onSubmit={saveGrant}>
          <label>
            目的地端点（如 https://api.openai.com）
            <input
              value={grantForm.destination}
              onChange={(event) =>
                setGrantForm((current) => ({
                  ...current,
                  destination: event.target.value,
                }))
              }
              placeholder="https://…"
              spellCheck={false}
              required
            />
          </label>
          <div className="scope-checks">
            {Object.entries(scopeLabels).map(([scope, label]) => (
              <label className="checkbox-label" key={scope}>
                <input
                  type="checkbox"
                  checked={grantForm.scopes.includes(scope)}
                  onChange={(event) =>
                    setGrantForm((current) => ({
                      ...current,
                      scopes: event.target.checked
                        ? [...current.scopes, scope]
                        : current.scopes.filter((item) => item !== scope),
                    }))
                  }
                />
                {label}
              </label>
            ))}
          </div>
          <div className="model-actions">
            <button
              type="submit"
              disabled={
                grantBusy ||
                !grantForm.destination.trim() ||
                grantForm.scopes.length === 0
              }
            >
              保存授权
            </button>
          </div>
        </form>
        <ErrorNotice error={grantError} />
      </section>
    )
  );
}
