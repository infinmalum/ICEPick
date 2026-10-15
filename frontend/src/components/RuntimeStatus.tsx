import type { RuntimeSettings } from "../hooks/useRuntimeSettings";
type Props = {
  runtime: Pick<RuntimeSettings, "status">;
  error: string;
};
export function RuntimeStatus(props: Props) {
  const { status } = props.runtime;
  const { error } = props;
  return (
    <details
      className="settings-group"
      aria-live="polite"
      open={
        !!error ||
        (!!status &&
          (!status.storageReady ||
            !status.runtimeReady ||
            !status.modelConnected))
      }
    >
      <summary>
        运行状态 ·{" "}
        {status?.storageReady && status?.runtimeReady && status?.modelConnected
          ? "已就绪"
          : "需要检查"}
      </summary>
      {error ? (
        <p className="error">无法读取本地服务状态：{error}</p>
      ) : status ? (
        <>
          <div className="status-row">
            <span>本地存储</span>
            <strong className={status.storageReady ? "ok" : ""}>
              {status.storageReady ? "已就绪" : "异常"}
            </strong>
          </div>
          <div className="status-row">
            <span>任务执行</span>
            <strong className={status.runtimeReady ? "ok" : ""}>
              {status.runtimeReady ? "已就绪" : "请配置模型"}
            </strong>
          </div>
          <div className="status-row">
            <span>模型服务</span>
            <strong className={status.modelConnected ? "ok" : ""}>
              {status.modelConnected ? "已连接" : "未连接"}
            </strong>
          </div>
          <p className="hint">{status.message}</p>
        </>
      ) : (
        <p className="hint">正在读取本地服务状态…</p>
      )}
    </details>
  );
}
