import type { Profiles } from "../hooks/useProfiles";
export function ProfileList({
  profileState,
  startConversation,
}: {
  profileState: Profiles;
  startConversation: (id: string) => void;
}) {
  const p = profileState;
  return (
    <section className="session-card" aria-label="你的助手">
      <h2>你的助手</h2>
      {p.profiles.length === 0 && (
        <p className="hint">还没有自己的助手；可以从下面的模板快速添加。</p>
      )}
      <div className="assistant-list">
        {p.profiles.map((item) => (
          <div
            className={`assistant-row ${p.profileEditID === item.id ? "selected" : ""}`}
            key={item.id}
          >
            <div className="assistant-row-main">
              <strong>
                {item.name}
                {item.isDefault && <span className="memory-meta">（默认）</span>}
                {item.data.templateId && (
                  <span className="memory-meta">（模板副本）</span>
                )}
              </strong>
              <span className="memory-meta">
                {item.data.description || "暂无说明"}
              </span>
              <span className="memory-meta">
                技能：
                {item.data.allSkills
                  ? "全部"
                  : `${item.data.enabledSkills?.length ?? 0} 个`}{" "}
                · MCP：
                {item.data.allMcpServers
                  ? "全部"
                  : `${item.data.enabledMcpServers?.length ?? 0} 个`}
              </span>
            </div>
            <div className="assistant-row-actions">
              <button
                type="button"
                className="text-button"
                disabled={p.profileBusy}
                onClick={() => startConversation(item.id)}
              >
                开始新对话
              </button>
              <button
                type="button"
                className="text-button"
                disabled={p.profileBusy}
                onClick={() => {
                  void p.setProfileEditID(item.id).then((ok) => {
                    if (ok)
                      document
                        .getElementById("assistant-editor")
                        ?.scrollIntoView({ block: "start", behavior: "smooth" });
                  });
                }}
              >
                编辑
              </button>
              <button
                type="button"
                className="text-button"
                disabled={p.profileBusy || item.isDefault}
                title={item.isDefault ? "默认助手不能删除" : undefined}
                onClick={() => void p.deleteProfile(item.id)}
              >
                删除
              </button>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
