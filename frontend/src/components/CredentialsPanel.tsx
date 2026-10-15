import { ErrorNotice } from "../shared/errors";
import type { RuntimeSettings } from "../hooks/useRuntimeSettings";
import { providerLabels } from "../shared/config";
type Props = {
  runtime: Pick<
    RuntimeSettings,
    | "credBusy"
    | "credDraft"
    | "credError"
    | "credStatus"
    | "deleteCredential"
    | "saveCredential"
    | "setCredDraft"
  >;
};
export function CredentialsPanel(props: Props) {
  const {
    credBusy,
    credDraft,
    credError,
    credStatus,
    deleteCredential,
    saveCredential,
    setCredDraft,
  } = props.runtime;
  return (
    <section className="session-card">
      <div className="card-title">远端凭据</div>
      <p className="hint">
        密钥只保存在本机数据目录（0600），不进入配置、Profile、运行记录或日志；绑定只显示是否已配置，不会回显密钥。
      </p>
      {credStatus.map((item) => (
        <div className="credential-row" key={item.provider}>
          <strong>{providerLabels[item.provider] ?? item.provider}</strong>
          <span className={`credential-state ${item.configured ? "ok" : ""}`}>
            {item.configured ? "已配置" : "未配置"}
          </span>
          <input
            type="password"
            placeholder="API 密钥"
            value={credDraft[item.provider] ?? ""}
            onChange={(event) =>
              setCredDraft((current) => ({
                ...current,
                [item.provider]: event.target.value,
              }))
            }
            autoComplete="off"
          />
          <button
            type="button"
            disabled={credBusy || !(credDraft[item.provider] ?? "").trim()}
            onClick={() => saveCredential(item.provider)}
          >
            保存
          </button>
          <button
            type="button"
            className="secondary"
            disabled={credBusy || !item.configured}
            onClick={() => deleteCredential(item.provider)}
          >
            删除
          </button>
        </div>
      ))}
      <ErrorNotice error={credError} />
    </section>
  );
}
