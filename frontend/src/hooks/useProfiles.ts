import { useEffect, useRef, useState } from "react";
import { StatusService } from "../../bindings/icepick";
import type {
  ProfileTemplate,
  ProfileReference,
  Profile,
} from "../../bindings/icepick/internal/storage";
import type {
  ExportResult,
  ImportPreview,
} from "../../bindings/icepick/internal/app";
import { useDialog } from "../shared/dialogs";
import { downloadBase64, bytesToBase64 } from "../shared/download";
export function useProfiles(refreshConversations: () => Promise<void>) {
  const dialog = useDialog();
  const cached = useRef(new Map<string, Profile>());
  const [templates, setTemplates] = useState<ProfileTemplate[]>([]);
  const [restoreGroup, setRestoreGroup] = useState("");
  const [deleteReplacement, setDeleteReplacement] = useState("__own__");
  const [profileRefs, setProfileRefs] = useState<ProfileReference[]>([]);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [profileEditID, chooseID] = useState<string | null>(null);
  const [profileDraft, setDraft] = useState<Profile | null>(null);
  const [profileBusy, setProfileBusy] = useState(false);
  const creating = useRef(false);
  const [creatingTemplate, setCreatingTemplate] = useState("");
  const [creationNotice, setCreationNotice] = useState("");
  const [creationError, setCreationError] = useState("");
  const [profileError, setProfileError] = useState("");
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(
    null,
  );
  const [importData, setImportData] = useState("");
  const [importBusy, setImportBusy] = useState(false);
  const importInputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const target = profiles.find((item) => item.id === profileEditID) ?? null;
    setDraft(
      target ? structuredClone(cached.current.get(target.id) ?? target) : null,
    );
  }, [profileEditID, profiles]);
  useEffect(() => {
    let current = true;
    setProfileRefs([]);
    if (profileEditID)
      StatusService.ProfileReferences(profileEditID)
        .then((refs) => {
          if (current) setProfileRefs(refs ?? []);
        })
        .catch(() => {});
    return () => {
      current = false;
    };
  }, [profileEditID, profiles]);
  const dirty =
    !!profileDraft &&
    JSON.stringify(profileDraft) !==
      JSON.stringify(profiles.find((p) => p.id === profileDraft.id));
  function setProfileDraft(value: Profile | null) {
    if (value) {
      if (
        JSON.stringify(value) ===
        JSON.stringify(profiles.find((p) => p.id === value.id))
      )
        cached.current.delete(value.id);
      else cached.current.set(value.id, structuredClone(value));
    }
    setDraft(value);
  }
  async function setProfileEditID(id: string | null) {
    if (id === profileEditID) return true;
    if (
      dirty &&
      !(await dialog.confirm({
        title: "保留未保存的修改？",
        body: "当前助手的修改会留在本机草稿中，稍后切回来可继续编辑。",
        confirm: "保留并切换",
      }))
    )
      return false;
    chooseID(id);
    return true;
  }
  function profileName(id: string) {
    return (
      profiles.find((item) => item.id === id)?.name ??
      `Profile ${id.slice(0, 8)}…`
    );
  }
  async function confirmCreation() {
    return (
      !dirty ||
      (await dialog.confirm({
        title: "保留修改并添加助手？",
        body: "当前助手的修改会保留为草稿。添加后打开新助手，稍后可以切回来继续编辑。",
        confirm: "保留并添加",
      }))
    );
  }
  function acceptCreated(created: Profile) {
    setProfiles((current) => [
      ...current.filter((p) => p.id !== created.id),
      created,
    ]);
    chooseID(created.id);
    setCreationNotice(`已添加「${created.name}」，可在下方编辑或开始新对话。`);
    setProfileError("");
  }
  async function createTemplate(id: string) {
    if (creating.current || profileBusy) return;
    creating.current = true;
    setCreatingTemplate(id);
    setProfileBusy(true);
    setCreationError("");
    setCreationNotice("");
    try {
      if (!(await confirmCreation())) return;
      // Clicking a template twice should not silently pile up copies: offer
      // to continue with the assistant this template already created.
      const existing = profiles.find((item) => item.data.templateId === id);
      if (existing) {
        const pick = await dialog.ask({
          title: `已有「${existing.name}」`,
          body: "这个模板已经添加过一个助手。可以继续编辑现有的，也可以再新建一个同模板的助手。",
          choices: [
            { label: "编辑现有的", value: "edit" },
            { label: "再新建一个", value: "new" },
          ],
        });
        if (pick === null) return;
        if (pick === "edit") {
          if (!(await setProfileEditID(existing.id))) return;
          setCreationNotice(
            `已选择「${existing.name}」，可在下方编辑或开始新对话。`,
          );
          return;
        }
      }
      acceptCreated(await StatusService.CreateTemplateProfile(id, ""));
    } catch (reason) {
      setCreationError(String(reason));
    } finally {
      creating.current = false;
      setCreatingTemplate("");
      setProfileBusy(false);
    }
  }
  async function refreshProfiles() {
    const items = await StatusService.ListProfiles();
    setProfiles(items ?? []);
  }
  async function saveProfile(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!profileDraft) return;
    setProfileBusy(true);
    try {
      const saved = await StatusService.UpdateProfile(profileDraft);
      cached.current.delete(saved.id);
      setProfileError("");
      await refreshProfiles();
      setDraft(saved);
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function createProfile(fromID = "") {
    if (creating.current || profileBusy) return;
    creating.current = true;
    setProfileBusy(true);
    setCreationError("");
    setCreationNotice("");
    try {
      if (!(await confirmCreation())) return;
      let name = "";
      if (!fromID) {
        name =
          (await dialog.ask({
            title: "创建助手",
            label: "助手名称",
            confirm: "创建",
            singleLine: true,
          })) ?? "";
        if (!name.trim()) return;
      }
      acceptCreated(await StatusService.CreateProfile(name.trim(), fromID));
    } catch (reason) {
      setCreationError(String(reason));
    } finally {
      creating.current = false;
      setProfileBusy(false);
    }
  }
  async function deleteProfile(targetID?: string) {
    const target = targetID
      ? profiles.find((item) => item.id === targetID)
      : profileDraft;
    if (!target) return;
    if (target.isDefault) {
      setProfileError("默认助手不能删除；可以先复制一个助手，再删除多余的那个。");
      return;
    }
    setProfileBusy(true);
    try {
      const refs = (await StatusService.ProfileReferences(target.id)) ?? [];
      const own = deleteReplacement === "__own__";
      const replacement = own ? "" : deleteReplacement;
      const consequence = refs.length
        ? `\n这些助手正在使用它：${refs.map((ref) => ref.name).join("、")}。删除后将${own ? "使用各自的执行配置" : `改用「${profileName(replacement)}」的执行配置`}。`
        : "";
      if (
        !(await dialog.confirm({
          title: `删除助手「${target.name}」？`,
          body: `历史记录会保留。正在进行的工作不受影响，后续对话需选择其他助手。${consequence}`,
          confirm: "删除助手",
          danger: true,
        }))
      )
        return;
      if (refs.length)
        await StatusService.ArchiveProfileReplacing(
          target.id,
          replacement,
          own,
          refs,
        );
      else await StatusService.ArchiveProfile(target.id);
      await refreshProfiles();
      await refreshConversations();
      // The assistant no longer exists: drop its cached edits and move on
      // directly instead of routing through setProfileEditID, whose
      // unsaved-changes prompt would promise a draft that can never be
      // revisited.
      cached.current.delete(target.id);
      if (target.id === profileEditID) {
        const remaining = (await StatusService.ListProfiles()) ?? [];
        setDraft(null);
        chooseID(remaining[0]?.id ?? null);
      }
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function restoreProfileDefault() {
    if (!profileDraft) return;
    if (
      !(await dialog.confirm({
        title: "恢复助手设置？",
        body: "这会替换当前选中的配置内容和未保存的修改。已进行的工作会保留。",
        confirm: "恢复设置",
        danger: true,
      }))
    )
      return;
    setProfileBusy(true);
    try {
      const restored = profileDraft.data.templateId
        ? await StatusService.RestoreTemplateProfile(
            profileDraft.id,
            profileDraft.revision,
            restoreGroup ? [restoreGroup] : [],
          )
        : await StatusService.RestoreProfileDefault(profileDraft.revision);
      await refreshProfiles();
      cached.current.delete(restored.id);
      setDraft(restored);
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function exportProfile() {
    if (!profileDraft) return;
    if (dirty) {
      setProfileError("请先保存修改，再导出助手。");
      return;
    }
    setProfileBusy(true);
    try {
      const saved = await StatusService.GetProfile(profileDraft.id);
      const refs = (saved.data.execution.workerSources ?? []).filter(
        (source) => source.profileId,
      );
      const result: ExportResult = refs.length
        ? await StatusService.ExportProfileIncludingWorkers(profileDraft.id)
        : await StatusService.ExportProfile(profileDraft.id);
      const reviewNotes = [
        ...(refs.length
          ? [
              `分享包也包含协作工作设置：${refs.map((source) => `${source.name} → ${profileName(source.profileId)}`).join("；")}。`,
            ]
          : []),
        ...(result.reviewNotes ?? []),
      ];
      if (
        reviewNotes.length &&
        !(await dialog.confirm({
          title: "导出前检查",
          body: reviewNotes.join("\n"),
          confirm: "已核对，导出",
        }))
      )
        return;
      downloadBase64(result.data, result.filename, "application/zip");
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function onImportFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setImportBusy(true);
    try {
      if (file.size > 32 * 1024 * 1024) throw new Error("助手分享包最多32MiB");
      const bytes = new Uint8Array(await file.arrayBuffer());
      const data = bytesToBase64(bytes);
      setImportPreview(await StatusService.PreviewProfileImport(data));
      setImportData(data);
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setImportBusy(false);
    }
  }
  async function confirmImport() {
    if (!importData) return;
    setImportBusy(true);
    try {
      const result = await StatusService.ImportProfile(importData);
      await refreshProfiles();
      setProfileEditID(result.profile.id);
      setImportPreview(null);
      setImportData("");
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setImportBusy(false);
    }
  }
  useEffect(() => {
    StatusService.ListProfileTemplates()
      .then((items) => setTemplates(items ?? []))
      .catch((reason) => setProfileError(String(reason)));
    StatusService.ListProfiles()
      .then((items) => {
        setProfiles(items ?? []);
        setProfileEditID(items?.[0]?.id ?? null);
      })
      .catch((reason) => setProfileError(String(reason)));
  }, []);
  return {
    templates,
    creatingTemplate,
    creationNotice,
    creationError,
    refreshProfiles,
    restoreGroup,
    setRestoreGroup,
    deleteReplacement,
    setDeleteReplacement,
    profileRefs,
    profiles,
    profileEditID,
    setProfileEditID,
    profileDraft,
    dirty,
    reportError: async (reason: unknown) => setProfileError(String(reason)),
    setProfileDraft,
    profileBusy,
    profileError,
    importPreview,
    setImportPreview,
    setImportData,
    importBusy,
    importInputRef,
    profileName,
    createTemplate,
    saveProfile,
    createProfile,
    deleteProfile,
    restoreProfileDefault,
    exportProfile,
    onImportFile,
    confirmImport,
  };
}
export type Profiles = ReturnType<typeof useProfiles>;
