import type { RuntimeSettings } from "../hooks/useRuntimeSettings";
import {
  providerDefaults,
  remoteProviders,
  providerLabels,
} from "../shared/config";
import { ErrorNotice } from "../shared/errors";
export function ModelSettings({
  runtime: r,
  onConnected,
}: {
  runtime: RuntimeSettings;
  onConnected?: () => void;
}) {
  const remote = remoteProviders.includes(
    r.modelDraft.provider as (typeof remoteProviders)[number],
  );
  return (
    <section className="session-card">
      <h2>连接模型</h2>
      <p>
        选择服务商，填写模型和密钥。你的密钥只保存在本机，不进入聊天或分享包。
      </p>
      <form className="model-form" onSubmit={r.saveModel}>
        <label>
          模型服务
          <select
            value={r.modelDraft.provider}
            onChange={(e) => {
              r.setApiKey("");
              r.setModelDraft({
                provider: e.target.value,
                baseUrl: providerDefaults[e.target.value],
                name: "",
              });
            }}
          >
            {Object.entries(providerLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        {remote && (
          <label>
            API 密钥
            {r.credStatus.find((v) => v.provider === r.modelDraft.provider)
              ?.configured && <small>已保存；留空继续使用</small>}
            <input
              type="password"
              autoComplete="off"
              value={r.apiKey}
              onChange={(e) => r.setApiKey(e.target.value)}
              placeholder="输入服务商提供的 API Key"
            />
          </label>
        )}
        <label>
          模型名称
          <input
            value={r.modelDraft.name}
            onChange={(e) =>
              r.setModelDraft({ ...r.modelDraft, name: e.target.value })
            }
            list="available-models"
            placeholder="输入模型名称，或连接后从列表选择"
            autoComplete="off"
          />
          <datalist id="available-models">
            {r.status?.availableModels?.map((n) => (
              <option key={n}>{n}</option>
            ))}
          </datalist>
        </label>
        {!remote && (
          <label>
            本机服务地址
            <input
              value={r.modelDraft.baseUrl}
              onChange={(e) =>
                r.setModelDraft({ ...r.modelDraft, baseUrl: e.target.value })
              }
            />
          </label>
        )}
        {remote && (
          <details>
            <summary>自定义服务地址</summary>
            <label>
              API 地址
              <input
                value={r.modelDraft.baseUrl}
                onChange={(e) =>
                  r.setModelDraft({ ...r.modelDraft, baseUrl: e.target.value })
                }
              />
            </label>
            <p>使用代理或兼容服务时填写。发送前会显示实际接收地址。</p>
          </details>
        )}
        <div className="actions">
          <button disabled={r.modelBusy || !r.status?.storageReady}>
            {r.modelBusy ? "正在连接…" : "保存并连接"}
          </button>
          <button
            type="button"
            className="secondary"
            disabled={r.modelBusy}
            onClick={r.refreshModel}
          >
            检测连接
          </button>
        </div>
      </form>
      <ErrorNotice error={r.modelError} />
      {r.connectedMessage && <p className="success">{r.connectedMessage}</p>}
      {r.status?.runtimeReady && (
        <button onClick={onConnected}>开始聊天 →</button>
      )}
    </section>
  );
}
