import { ErrorNotice } from "../shared/errors";
import type { Profiles } from "../hooks/useProfiles";
import type { MCPServer } from "../../bindings/icepick/internal/config";
import type { SkillInfo } from "../../bindings/icepick/internal/app";
import { ProfileCapabilities } from "./ProfileCapabilities";
type Props = {
  openDeveloper: () => void;
  openExtensions: () => void;
  skillCatalog: SkillInfo[];
  mcpCatalog: MCPServer[];
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
      <section className="session-card" id="assistant-editor">
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
            disabled={profileBusy || profileDraft.isDefault}
            title={profileDraft.isDefault ? "默认助手不能删除" : undefined}
            onClick={() => void deleteProfile()}
          >
            删除
          </button>
          {profileDraft.isDefault && (
            <span className="memory-meta">默认助手不能删除</span>
          )}
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
          <ProfileCapabilities
            profileState={props.profileState}
            skills={props.skillCatalog}
            mcpServers={props.mcpCatalog}
            openExtensions={props.openExtensions}
          />
          <button
            type="button"
            className="secondary"
            onClick={props.openDeveloper}
          >
            配置模型覆盖与工作流程 →
          </button>

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
