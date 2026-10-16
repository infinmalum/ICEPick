import type { Profiles } from "../hooks/useProfiles";
import { ErrorNotice } from "../shared/errors";
import { ProfileExecution } from "./ProfileExecution";
import { providerDefaults, remoteProviders } from "../shared/config";
export function ProfileAdvanced({ profileState }: { profileState: Profiles }) {
  const { profileDraft, setProfileDraft } = profileState;
  return (
    <section className="session-card">
      <h2>助手的执行配置</h2>
      <p>
        只影响下面选中的助手。与助手页面共用同一份草稿，保存后用于后续任务。
      </p>
      <label>
        要配置的助手
        <select
          aria-label="开发者设置中的助手"
          value={profileState.profileEditID ?? ""}
          disabled={profileState.profileBusy}
          onChange={(e) => void profileState.setProfileEditID(e.target.value)}
        >
          {!profileState.profileEditID && (
            <option value="" disabled>
              请选择助手
            </option>
          )}
          {profileState.profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
              {p.data.templateId ? "（模板副本）" : ""}
            </option>
          ))}
        </select>
      </label>
      {profileDraft && (
        <form className="model-form" onSubmit={profileState.saveProfile}>
          <label>
            工具白名单（每行一个，留空表示启用服务的全部工具）
            <textarea
              rows={3}
              value={(profileDraft.data.allowedTools ?? []).join("\n")}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    allowedTools: event.target.value
                      .split("\n")
                      .map((name) => name.trim())
                      .filter(Boolean),
                  },
                })
              }
            />
          </label>
          <label>
            模型来源
            <select
              value={profileDraft.data.model.provider || "default"}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    model:
                      event.target.value === "default"
                        ? { provider: "", baseUrl: "", name: "" }
                        : {
                            provider: event.target.value,
                            baseUrl: providerDefaults[event.target.value] ?? "",
                            name: "",
                          },
                  },
                })
              }
            >
              <option value="default">使用应用默认模型</option>
              <option value="ollama">Ollama</option>
              <option value="lmstudio">LM Studio</option>
              <option value="openai">OpenAI</option>
              <option value="anthropic">Anthropic</option>
              <option value="gemini">Gemini</option>
            </select>
          </label>
          {profileDraft.data.model.provider && (
            <>
              <label>
                这个助手的模型地址
                {remoteProviders.includes(
                  profileDraft.data.model
                    .provider as (typeof remoteProviders)[number],
                )
                  ? "（留空使用官方端点）"
                  : ""}
                <input
                  value={profileDraft.data.model.baseUrl}
                  onChange={(event) =>
                    setProfileDraft({
                      ...profileDraft,
                      data: {
                        ...profileDraft.data,
                        model: {
                          ...profileDraft.data.model,
                          baseUrl: event.target.value,
                        },
                      },
                    })
                  }
                  placeholder={
                    providerDefaults[profileDraft.data.model.provider] ?? ""
                  }
                />
              </label>
              <label>
                这个助手的模型名称
                <input
                  value={profileDraft.data.model.name}
                  onChange={(event) =>
                    setProfileDraft({
                      ...profileDraft,
                      data: {
                        ...profileDraft.data,
                        model: {
                          ...profileDraft.data.model,
                          name: event.target.value,
                        },
                      },
                    })
                  }
                />
              </label>
            </>
          )}
          <label>
            人格
            <textarea
              value={profileDraft.data.persona}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: { ...profileDraft.data, persona: event.target.value },
                })
              }
            />
          </label>
          <label>
            上下文预算（token）
            <input
              type="number"
              min={1024}
              max={1000000}
              value={profileDraft.data.contextBudget}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    contextBudget: Number(event.target.value),
                  },
                })
              }
            />
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={profileDraft.data.planEnabled}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    planEnabled: event.target.checked,
                  },
                })
              }
            />
            启用任务规划（会话待办；配置 worker 后可声明本次执行计划）
          </label>
          <ProfileExecution profileState={profileState} />

          <button disabled={profileState.profileBusy}>保存执行配置</button>
          {profileState.dirty && (
            <p role="status">
              有未保存的助手修改，保存会包含这份草稿中的全部修改。
            </p>
          )}
        </form>
      )}
      <ErrorNotice error={profileState.profileError} />
    </section>
  );
}
