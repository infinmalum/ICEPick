import type { Profiles } from "../hooks/useProfiles";
type Props = {
  profileState: Pick<
    Profiles,
    | "createProfile"
    | "createTemplate"
    | "profileBusy"
    | "profileDraft"
    | "profileError"
    | "templates"
  >;
};
export function ProfileTemplates(props: Props) {
  const {
    createProfile,
    createTemplate,
    profileBusy,
    profileDraft,
    profileError,
    templates,
  } = props.profileState;
  return (
    <section className="session-card">
      <div className="card-title">场景模板</div>
      <p className="hint">
        模板由程序提供，删除本地 Profile
        后仍可重建。研究与开发为动态流程试验配置，包含必需独立审查；所有模板均待真实模型验收。
      </p>
      <div className="model-actions">
        {templates.map((item) => (
          <button
            key={item.id}
            type="button"
            className="secondary"
            disabled={profileBusy}
            onClick={() => createTemplate(item.id)}
          >
            ＋ {item.name}
          </button>
        ))}
        <button
          type="button"
          className="secondary"
          disabled={profileBusy}
          onClick={() => createProfile("")}
        >
          ＋ 空白 Profile
        </button>
      </div>
      {!profileDraft && (
        <p className="hint">
          请创建或选择 Profile；研究与开发模板需要提供实际材料或配置工具依赖。
        </p>
      )}
      {!profileDraft && profileError && <p className="error">{profileError}</p>}
    </section>
  );
}
