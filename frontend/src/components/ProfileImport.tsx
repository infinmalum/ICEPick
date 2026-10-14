import type { Profiles } from "../hooks/useProfiles";
type Props = {
  profileState: Pick<
    Profiles,
    | "confirmImport"
    | "importBusy"
    | "importPreview"
    | "setImportData"
    | "setImportPreview"
  >;
};
export function ProfileImport(props: Props) {
  const {
    confirmImport,
    importBusy,
    importPreview,
    setImportData,
    setImportPreview,
  } = props.profileState;
  return (
    importPreview && (
      <section className="session-card">
        <div className="card-title">导入预览「{importPreview.name}」</div>
        <div className="import-preview">
          <p>
            模型：{importPreview.model.provider || "（应用默认）"}
            {importPreview.model.name ? `/${importPreview.model.name}` : ""} ·
            上下文预算 {importPreview.contextBudget} tokens · 人格{" "}
            {importPreview.personaChars} 字符 · 提示词{" "}
            {importPreview.promptChars} 字符
          </p>
          <p>
            委派：
            {importPreview.delegation.enabled
              ? `启用（深度 ${importPreview.delegation.maxDepth} · 并发 ${importPreview.delegation.maxConcurrency || 2} · ${importPreview.delegation.maxSeconds > 0 ? `${importPreview.delegation.maxSeconds} 秒` : "随运行上限"}）`
              : "关闭"}{" "}
            · 记忆偏好：
            {importPreview.memoryEnabled
              ? "开（需接收方 本机记忆授权才生效）"
              : "关"}{" "}
            · 规划：{importPreview.planEnabled ? "开" : "关"}
          </p>
          {importPreview.mcpServers && importPreview.mcpServers.length > 0 && (
            <ul className="import-list">
              {importPreview.mcpServers.map((server) => (
                <li key={server.name}>
                  MCP {server.name}（命令 {server.command}）
                  {server.configured
                    ? " · 已接入本机"
                    : " · 未接入本机，需自行配置"}
                </li>
              ))}
            </ul>
          )}
          {importPreview.skills && importPreview.skills.length > 0 && (
            <ul className="import-list">
              {importPreview.skills.map((skill) => (
                <li key={skill.name}>
                  Skill {skill.name}
                  {skill.bundled ? " · 包内包含文件" : ""}
                  {skill.installed
                    ? " · 已安装"
                    : " · 未安装，导入不会自动安装"}
                </li>
              ))}
            </ul>
          )}
          {(importPreview.workers ?? []).length > 0 && (
            <ul className="import-list">
              {(importPreview.workers ?? []).map((worker) => (
                <li key={worker.name}>
                  Worker {worker.name} → {worker.source}
                  {worker.bundled ? "（独立配置副本）" : ""}
                </li>
              ))}
            </ul>
          )}
          {(importPreview.warnings ?? []).map((warning, index) => (
            <p className="hint" key={index}>
              注意：{warning}
            </p>
          ))}
        </div>
        <div className="model-actions">
          <button type="button" disabled={importBusy} onClick={confirmImport}>
            确认导入为新 Profile
          </button>
          <button
            type="button"
            className="secondary"
            disabled={importBusy}
            onClick={() => {
              setImportPreview(null);
              setImportData("");
            }}
          >
            取消
          </button>
        </div>
      </section>
    )
  );
}
