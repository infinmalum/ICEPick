import { ErrorNotice } from "../shared/errors";
import type { Profiles } from "../hooks/useProfiles";
import { ProfileExecution } from "./ProfileExecution";
import { providerDefaults, remoteProviders } from "../shared/config";
type Props = {
  profileState: Pick<
    Profiles,
    | "createProfile"
    | "deleteProfile"
    | "deleteReplacement"
    | "exportProfile"
    | "importInputRef"
    | "onImportFile"
    | "profileBusy"
    | "profileDraft"
    | "profileEditID"
    | "profileError"
    | "profileRefs"
    | "profiles"
    | "restoreGroup"
    | "restoreProfileDefault"
    | "saveProfile"
    | "setDeleteReplacement"
    | "setProfileDraft"
    | "setProfileEditID"
    | "setRestoreGroup"
  >;
};
export function ProfileEditor(props: Props) {
  const {
    createProfile,
    deleteProfile,
    deleteReplacement,
    exportProfile,
    importInputRef,
    onImportFile,
    profileBusy,
    profileDraft,
    profileEditID,
    profileError,
    profileRefs,
    profiles,
    restoreGroup,
    restoreProfileDefault,
    saveProfile,
    setDeleteReplacement,
    setProfileDraft,
    setProfileEditID,
    setRestoreGroup,
  } = props.profileState;
  return (
    profileDraft && (
      <section className="session-card">
        <div className="card-title">
          助手「{profileDraft.name}」{" "}
          <span className="revision" hidden>
            v{profileDraft.revision}
            {profileDraft.data.templateId ? " · 场景模板副本" : ""}
          </span>
        </div>
        <label className="model-form">
          选择要编辑的助手
          <select
            value={profileEditID ?? ""}
            onChange={(event) => setProfileEditID(event.target.value)}
          >
            {profiles.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
                {item.data.templateId ? "（模板副本）" : ""}
              </option>
            ))}
          </select>
        </label>
        <div className="model-actions">
          <button
            type="button"
            className="secondary"
            disabled={profileBusy}
            onClick={() => createProfile("")}
          >
            ＋ 创建助手
          </button>
          <button
            type="button"
            className="secondary"
            disabled={profileBusy}
            onClick={() => createProfile(profileDraft.id)}
          >
            复制当前
          </button>
          <button
            type="button"
            className="secondary"
            disabled={profileBusy}
            onClick={exportProfile}
          >
            导出助手
          </button>
          <button
            type="button"
            className="secondary"
            disabled={profileBusy}
            onClick={() => importInputRef.current?.click()}
          >
            导入助手…
          </button>
          <input
            ref={importInputRef}
            type="file"
            accept=".zip,application/zip"
            style={{ display: "none" }}
            onChange={onImportFile}
          />
          {profileRefs.length > 0 && (
            <label>
              其他助手正在使用此配置，删除后改用
              <select
                value={deleteReplacement}
                onChange={(event) => setDeleteReplacement(event.target.value)}
              >
                <option value="__own__">各助手自己的执行配置</option>
                {profiles
                  .filter((item) => item.id !== profileDraft.id)
                  .map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
              </select>
            </label>
          )}
          <button
            type="button"
            className="secondary"
            disabled={profileBusy}
            onClick={deleteProfile}
          >
            删除
          </button>
          {(profileDraft.data.templateId || profileDraft.isDefault) && (
            <>
              {profileDraft.data.templateId && (
                <select
                  aria-label="恢复配置组"
                  value={restoreGroup}
                  onChange={(event) => setRestoreGroup(event.target.value)}
                >
                  <option value="">全部配置</option>
                  {[
                    "persona",
                    "prompt",
                    "model",
                    "capabilities",
                    "runtime",
                    "execution",
                  ].map((group) => (
                    <option key={group} value={group}>
                      {
                        (
                          {
                            persona: "人格与说明",
                            prompt: "工作说明",
                            model: "模型",
                            capabilities: "工具与工作方法",
                            runtime: "处理偏好",
                            execution: "组件设置",
                          } as Record<string, string>
                        )[group]
                      }
                    </option>
                  ))}
                </select>
              )}
              <button
                type="button"
                className="secondary"
                disabled={profileBusy}
                onClick={restoreProfileDefault}
              >
                恢复出厂配置
              </button>
            </>
          )}
        </div>
        <form className="model-form" onSubmit={saveProfile}>
          <label>
            助手名称
            <input
              value={profileDraft.name}
              onChange={(event) =>
                setProfileDraft({ ...profileDraft, name: event.target.value })
              }
              required
            />
          </label>
          <label>
            说明
            <input
              value={profileDraft.data.description}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    description: event.target.value,
                  },
                })
              }
            />
          </label>
          <label>
            希望助手怎么做
            <textarea
              rows={4}
              value={profileDraft.data.systemPrompt}
              onChange={(event) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    systemPrompt: event.target.value,
                  },
                })
              }
            />
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={profileDraft.data.memoryEnabled}
              onChange={(e) =>
                setProfileDraft({
                  ...profileDraft,
                  data: {
                    ...profileDraft.data,
                    memoryEnabled: e.target.checked,
                  },
                })
              }
            />
            使用记忆（仍需在对话中允许）
          </label>
          <details className="developer-settings">
            <summary>开发者设置 · 工具、模型与工作流程</summary>
            <p className="hint">
              这些设置面向需要自定义执行方式的用户。普通聊天无需修改。
            </p>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={profileDraft.data.allMcpServers}
                onChange={(event) =>
                  setProfileDraft({
                    ...profileDraft,
                    data: {
                      ...profileDraft.data,
                      allMcpServers: event.target.checked,
                    },
                  })
                }
              />
              启用全部已接入的 MCP 服务
            </label>
            {!profileDraft.data.allMcpServers && (
              <label>
                启用的 MCP 服务名（每行一个）
                <textarea
                  rows={2}
                  value={(profileDraft.data.enabledMcpServers ?? []).join("\n")}
                  onChange={(event) =>
                    setProfileDraft({
                      ...profileDraft,
                      data: {
                        ...profileDraft.data,
                        enabledMcpServers: event.target.value
                          .split("\n")
                          .map((name) => name.trim())
                          .filter(Boolean),
                      },
                    })
                  }
                />
              </label>
            )}
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
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={profileDraft.data.allSkills}
                onChange={(event) =>
                  setProfileDraft({
                    ...profileDraft,
                    data: {
                      ...profileDraft.data,
                      allSkills: event.target.checked,
                    },
                  })
                }
              />
              启用全部已安装的 Skills
            </label>
            {!profileDraft.data.allSkills && (
              <label>
                启用 Skills（每行一个名称）
                <textarea
                  rows={2}
                  value={(profileDraft.data.enabledSkills ?? []).join("\n")}
                  onChange={(event) =>
                    setProfileDraft({
                      ...profileDraft,
                      data: {
                        ...profileDraft.data,
                        enabledSkills: event.target.value
                          .split("\n")
                          .map((name) => name.trim())
                          .filter(Boolean),
                      },
                    })
                  }
                />
              </label>
            )}
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
                              baseUrl:
                                providerDefaults[event.target.value] ?? "",
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
                  Profile 模型地址
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
                  Profile 模型 ID
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
                checked={profileDraft.data.memoryEnabled}
                onChange={(event) =>
                  setProfileDraft({
                    ...profileDraft,
                    data: {
                      ...profileDraft.data,
                      memoryEnabled: event.target.checked,
                    },
                  })
                }
              />
              启用长期记忆（运行开始时检索，模型可用 local__memory_save 写入）
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
            <ProfileExecution profileState={props.profileState} />
          </details>
          <div className="model-actions">
            <button type="submit" disabled={profileBusy}>
              保存助手
            </button>
          </div>
        </form>
        <ErrorNotice error={profileError} />
        <p className="hint">
          保存后用于后续工作。导出助手不包含聊天记录、记忆、权限或密钥。
        </p>
      </section>
    )
  );
}
