import { useEffect, useState } from "react";
import { StatusService, type AppStatus } from "../../bindings/icepick";
import type {
  CredentialStatus,
  MCPEndpointStatus,
} from "../../bindings/icepick/internal/app";
import { type ModelDraft } from "../shared/config";
export function useRuntimeSettings() {
  const [status, setStatus] = useState<AppStatus | null>(null);
  const [error, setError] = useState("");
  const [mcpServers, setMcpServers] = useState<MCPEndpointStatus[]>([]);
  const [modelDraft, setModelDraft] = useState<ModelDraft>({
    provider: "ollama",
    baseUrl: "http://127.0.0.1:11434/v1",
    name: "",
  });
  const [modelBusy, setModelBusy] = useState(false);
  const [modelError, setModelError] = useState("");
  const [credStatus, setCredStatus] = useState<CredentialStatus[]>([]);
  const [credDraft, setCredDraft] = useState<Record<string, string>>({});
  const [apiKey, setApiKey] = useState("");
  const [connectedMessage, setConnectedMessage] = useState("");
  const [credBusy, setCredBusy] = useState(false);
  const [credError, setCredError] = useState("");
  async function refreshModel() {
    setModelBusy(true);
    try {
      const value = await StatusService.GetStatus();
      setStatus(value);
      setModelError("");
    } catch (reason) {
      setModelError(String(reason));
    } finally {
      setModelBusy(false);
    }
  }
  async function saveModel(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setModelBusy(true);
    setConnectedMessage("");
    try {
      if (apiKey.trim())
        await StatusService.SaveCredential(modelDraft.provider, apiKey.trim());
      setApiKey("");
      const value = await StatusService.SaveModelConfig(modelDraft);
      setStatus(value);
      setCredStatus((await StatusService.ListCredentialStatus()) ?? []);
      if (!value.modelConnected) {
        setModelError(value.message);
        return;
      }
      setModelError("");
      setConnectedMessage(
        value.model.name
          ? "模型已连接，可以开始聊天。"
          : "服务已连接，请选择一个模型后保存。",
      );
    } catch (e) {
      setModelError(String(e));
    } finally {
      setModelBusy(false);
    }
  }
  async function saveCredential(provider: string) {
    const apiKey = (credDraft[provider] ?? "").trim();
    if (!apiKey) return;
    setCredBusy(true);
    try {
      await StatusService.SaveCredential(provider, apiKey);
      setCredDraft((current) => ({ ...current, [provider]: "" }));
      setCredStatus((await StatusService.ListCredentialStatus()) ?? []);
      setCredError("");
    } catch (reason) {
      setCredError(String(reason));
    } finally {
      setCredBusy(false);
    }
  }
  async function deleteCredential(provider: string) {
    setCredBusy(true);
    try {
      await StatusService.DeleteCredential(provider);
      setCredStatus((await StatusService.ListCredentialStatus()) ?? []);
      setCredError("");
    } catch (reason) {
      setCredError(String(reason));
    } finally {
      setCredBusy(false);
    }
  }
  useEffect(() => {
    StatusService.GetStatus()
      .then((value) => {
        setStatus(value);
        setModelDraft(value.model);
      })
      .catch((reason) => setError(String(reason)));
    StatusService.ListMCPEndpoints()
      .then((items) => setMcpServers(items ?? []))
      .catch((reason) => setError(String(reason)));
    StatusService.ListCredentialStatus()
      .then((items) => setCredStatus(items ?? []))
      .catch((reason) => setCredError(String(reason)));
  }, []);
  return {
    status,
    error,
    mcpServers,
    modelDraft,
    setModelDraft,
    modelBusy,
    modelError,
    credStatus,
    credDraft,
    setCredDraft,
    credBusy,
    credError,
    refreshModel,
    saveModel,
    apiKey,
    setApiKey,
    connectedMessage,
    saveCredential,
    deleteCredential,
  };
}
export type RuntimeSettings = ReturnType<typeof useRuntimeSettings>;
