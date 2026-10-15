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
import { downloadBase64, bytesToBase64 } from "../shared/download";
export function useProfiles(refreshConversations: () => Promise<void>) {
  const [templates, setTemplates] = useState<ProfileTemplate[]>([]);
  const [restoreGroup, setRestoreGroup] = useState("");
  const [deleteReplacement, setDeleteReplacement] = useState("__own__");
  const [profileRefs, setProfileRefs] = useState<ProfileReference[]>([]);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [profileEditID, setProfileEditID] = useState<string | null>(null);
  const [profileDraft, setProfileDraft] = useState<Profile | null>(null);
  const [profileBusy, setProfileBusy] = useState(false);
  const [profileError, setProfileError] = useState("");
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(
    null,
  );
  const [importData, setImportData] = useState("");
  const [importBusy, setImportBusy] = useState(false);
  const importInputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const target = profiles.find((item) => item.id === profileEditID) ?? null;
    setProfileDraft(target ? structuredClone(target) : null);
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
  function profileName(id: string) {
    return (
      profiles.find((item) => item.id === id)?.name ??
      `Profile ${id.slice(0, 8)}…`
    );
  }
  async function createTemplate(id: string) {
    setProfileBusy(true);
    try {
      const created = await StatusService.CreateTemplateProfile(id, "");
      await refreshProfiles();
      setProfileEditID(created.id);
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
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
      setProfileError("");
      await refreshProfiles();
      setProfileDraft(saved);
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function createProfile(fromID = "") {
    let name = "";
    if (!fromID) {
      name = window.prompt("新 Profile 名称（空白模板）", "") ?? "";
      if (!name.trim()) return;
    }
    setProfileBusy(true);
    try {
      const created = await StatusService.CreateProfile(name.trim(), fromID);
      await refreshProfiles();
      setProfileEditID(created.id);
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function deleteProfile() {
    if (!profileDraft) return;
    setProfileBusy(true);
    try {
      const refs =
        (await StatusService.ProfileReferences(profileDraft.id)) ?? [];
      const own = deleteReplacement === "__own__";
      const replacement = own ? "" : deleteReplacement;
      const consequence = refs.length
        ? `\n被引用的 worker：${refs.map((ref) => `${ref.name}（${(ref.workers ?? []).join("、")}）`).join("；")}。\n这些来源将${own ? "恢复为各自 Profile 的 worker 配置" : `替换为「${profileName(replacement)}」`}。`
        : "";
      if (
        !window.confirm(
          `删除 Profile「${profileDraft.name}」？历史与在途任务保留，后续任务需选择可用配置。${consequence}`,
        )
      )
        return;
      if (refs.length)
        await StatusService.ArchiveProfileReplacing(
          profileDraft.id,
          replacement,
          own,
          refs,
        );
      else await StatusService.ArchiveProfile(profileDraft.id);
      await refreshProfiles();
      await refreshConversations();
      const remaining = (await StatusService.ListProfiles()) ?? [];
      setProfileEditID(remaining[0]?.id ?? null);
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function restoreProfileDefault() {
    if (!profileDraft) return;
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
      setProfileDraft(restored);
      setProfileError("");
    } catch (reason) {
      setProfileError(String(reason));
    } finally {
      setProfileBusy(false);
    }
  }
  async function exportProfile() {
    if (!profileDraft) return;
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
        ...(refs.length ? [`分享包包含独立 worker 配置：${refs.map((source) => `${source.name} → ${profileName(source.profileId)}`).join("；")}。`] : []),
        ...(result.reviewNotes ?? []),
      ];
      if (reviewNotes.length && !window.confirm(reviewNotes.join("\n") + "\n已检查内容，继续保存分享包？")) return;
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
    const bytes = new Uint8Array(await file.arrayBuffer());
    setImportBusy(true);
    try {
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
