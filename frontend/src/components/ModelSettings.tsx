import type { RuntimeSettings } from "../hooks/useRuntimeSettings";
import { providerDefaults, remoteProviders } from "../shared/config";
type Props = {
  runtime: Pick<
    RuntimeSettings,
    | "modelBusy"
    | "modelDraft"
    | "modelError"
    | "refreshModel"
    | "saveModel"
    | "setModelDraft"
    | "status"
  >;
};
export function ModelSettings(props: Props) {
  const {
    modelBusy,
    modelDraft,
    modelError,
    refreshModel,
    saveModel,
    setModelDraft,
    status,
  } = props.runtime;
  return (
    <section className="session-card">
      <div className="card-title">模型服务</div>
      <form className="model-form" onSubmit={saveModel}>
        <label>
          提供方
          <select
            value={modelDraft.provider}
            onChange={(event) =>
              setModelDraft({
                provider: event.target.value,
                baseUrl: providerDefaults[event.target.value] ?? "",
                name: "",
              })
            }
          >
            <option value="ollama">Ollama（本机）</option>
            <option value="lmstudio">LM Studio（本机）</option>
            <option value="openai">OpenAI（远端）</option>
            <option value="anthropic">Anthropic（远端）</option>
            <option value="gemini">Gemini（远端）</option>
          </select>
        </label>
        {remoteProviders.includes(
          modelDraft.provider as (typeof remoteProviders)[number],
        ) ? (
          <>
            <label>
              API 地址（留空使用官方端点）
              <input
                value={modelDraft.baseUrl}
                onChange={(event) =>
                  setModelDraft((current) => ({
                    ...current,
                    baseUrl: event.target.value,
                  }))
                }
                placeholder={providerDefaults[modelDraft.provider] ?? ""}
                spellCheck={false}
              />
            </label>
            <p className="hint">
              远端发送受数据外发授权约束；请在下方凭据面板保存 API
              密钥，并在外发授权面板允许该端点。
            </p>
          </>
        ) : (
          <label>
            本机地址
            <input
              value={modelDraft.baseUrl}
              onChange={(event) =>
                setModelDraft((current) => ({
                  ...current,
                  baseUrl: event.target.value,
                }))
              }
              spellCheck={false}
              required
            />
          </label>
        )}
        <label>
          模型 ID（可稍后选择）
          <input
            value={modelDraft.name}
            onChange={(event) =>
              setModelDraft((current) => ({
                ...current,
                name: event.target.value,
              }))
            }
            list="local-models"
            spellCheck={false}
          />
          <datalist id="local-models">
            {status?.availableModels?.map((name) => (
              <option key={name} value={name} />
            ))}
          </datalist>
        </label>
        <div className="model-actions">
          <button type="submit" disabled={modelBusy || !status?.storageReady}>
            保存并检测
          </button>
          <button
            type="button"
            className="secondary"
            onClick={refreshModel}
            disabled={modelBusy}
          >
            刷新连接
          </button>
        </div>
      </form>
      {modelError && <p className="error">{modelError}</p>}
      {status?.modelConnected && (
        <p className="hint">
          可见模型：
          {status.availableModels?.length
            ? status.availableModels.join("、")
            : "暂无"}
        </p>
      )}
    </section>
  );
}
