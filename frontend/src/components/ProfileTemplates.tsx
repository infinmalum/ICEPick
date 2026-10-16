import { ErrorNotice } from "../shared/errors";
import type { Profiles } from "../hooks/useProfiles";
const descriptions: Record<string, string> = {
  daily: "解答问题、整理信息，处理日常事务。",
  writing: "起草文章、润色表达，按你的语气改写。",
  research: "比较资料、整理来源，形成研究报告。",
  development: "理解代码、修改项目并检查结果。",
};
export function ProfileTemplates({
  profileState: p,
}: {
  profileState: Profiles;
}) {
  return (
    <section className="session-card">
      <h2>添加适合你的助手</h2>
      <div className="template-grid">
        {p.templates.map((t) => (
          <button
            type="button"
            key={t.id}
            className="template-card"
            disabled={p.profileBusy}
            onClick={() => p.createTemplate(t.id)}
          >
            <strong>{t.name.replace("（试验）", "")}</strong>
            <span>{descriptions[t.id] ?? t.data.description}</span>
            <span>
              {p.creatingTemplate === t.id ? "正在添加…" : "添加并编辑 →"}
            </span>
            {["research", "development"].includes(t.id) && (
              <small>试用 · 处理文件或执行操作前会请求授权</small>
            )}
          </button>
        ))}
      </div>
      <button
        type="button"
        className="secondary"
        disabled={p.profileBusy}
        onClick={() => p.createProfile("")}
      >
        ＋ 从空白创建
      </button>
      <ErrorNotice error={p.creationError} />
      {p.creationNotice && (
        <p className="success" role="status">
          {p.creationNotice}
        </p>
      )}
      {p.creationNotice && (
        <button
          className="secondary"
          onClick={() =>
            document
              .getElementById("assistant-editor")
              ?.scrollIntoView({ block: "start", behavior: "smooth" })
          }
        >
          查看新助手
        </button>
      )}
      <p className="hint">
        你可以编辑、复制或分享自己的助手。实际表现取决于所连接的模型。
      </p>
    </section>
  );
}
