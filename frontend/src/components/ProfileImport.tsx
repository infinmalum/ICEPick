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
        <div className="card-title">导入助手「{importPreview.name}」</div>
        <div className="import-preview">
          <p>将添加独立助手，不会替换当前助手或自动授予文件、联网权限。</p>
          <p>
            使用{importPreview.model.name || "你已连接的默认模型"}。
            {importPreview.memoryEnabled ? "支持在你允许时使用记忆。" : ""}
          </p>
          <p>
            需要 {importPreview.mcpServers?.length ?? 0} 项扩展服务与{" "}
            {importPreview.skills?.length ?? 0} 项工作方法。
          </p>
          {(importPreview.mcpServers ?? [])
            .filter((item) => !item.configured)
            .map((item) => (
              <p className="hint" key={item.name}>
                扩展服务「{item.name}」尚未连接，相关能力暂不可用。
              </p>
            ))}
          <details>
            <summary>开发者详情</summary>
            <pre>{JSON.stringify(importPreview, null, 2)}</pre>
          </details>
          {(importPreview.warnings ?? []).map((warning, index) => (
            <p className="hint" key={index}>
              注意：{warning}
            </p>
          ))}
        </div>
        <div className="model-actions">
          <button type="button" disabled={importBusy} onClick={confirmImport}>
            添加这个助手
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
