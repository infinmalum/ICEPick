import { ErrorNotice } from "../shared/errors";
import type { Workspace } from "../hooks/useWorkspace";
import type { LocalContextState } from "../hooks/useLocalContext";
import { scopeLabels } from "../shared/config";
type Props = {
  workspace: Pick<Workspace, "activeConversation" | "selected">;
  local: Pick<
    LocalContextState,
    | "addMemory"
    | "deleteMemory"
    | "exportMemory"
    | "memories"
    | "memoryBusy"
    | "memoryDraft"
    | "memoryEdit"
    | "memoryError"
    | "saveMemoryEdit"
    | "setMemoryDraft"
    | "setMemoryEdit"
  >;
};
export function MemoryPanel(props: Props) {
  const { activeConversation, selected } = props.workspace;
  const {
    addMemory,
    deleteMemory,
    exportMemory,
    memories,
    memoryBusy,
    memoryDraft,
    memoryEdit,
    memoryError,
    saveMemoryEdit,
    setMemoryDraft,
    setMemoryEdit,
  } = props.local;
  return (
    selected && (
      <section className="session-card">
        <div className="card-title">
          长期记忆 · {activeConversation?.title ?? "未选择对话"}
        </div>
        <p className="hint">
          ICEPick
          会在你允许时使用这些记忆。你可以随时查看、修改或删除；关闭记忆不会自动删除已有内容。
        </p>
        {(memories ?? []).length === 0 && (
          <p className="hint">
            还没有记忆。你可以告诉助手希望它记住什么，或在下方添加。
          </p>
        )}
        {(memories ?? []).map((memory) => (
          <div className="memory-row" key={memory.id}>
            {memoryEdit && memoryEdit.id === memory.id ? (
              <div className="memory-edit">
                <textarea
                  rows={2}
                  value={memoryEdit.content}
                  onChange={(event) =>
                    setMemoryEdit({
                      id: memory.id,
                      content: event.target.value,
                    })
                  }
                />
                <div className="model-actions">
                  <button
                    type="button"
                    disabled={memoryBusy || !memoryEdit.content.trim()}
                    onClick={saveMemoryEdit}
                  >
                    保存修改
                  </button>
                  <button
                    type="button"
                    className="secondary"
                    onClick={() => setMemoryEdit(null)}
                  >
                    取消
                  </button>
                </div>
              </div>
            ) : (
              <>
                <p className="memory-content">{memory.content}</p>
                <span
                  className={`memory-meta ${memory.scope === "credential" || memory.scope === "unknown" ? "sensitive" : ""}`}
                >
                  {scopeLabels[memory.scope] ?? memory.scope} ·{" "}
                  {memory.source === "model" ? "模型保存" : "手动添加"}
                </span>
                <span className="memory-actions">
                  <button
                    type="button"
                    className="secondary"
                    onClick={() =>
                      setMemoryEdit({ id: memory.id, content: memory.content })
                    }
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    className="secondary"
                    disabled={memoryBusy}
                    onClick={() => deleteMemory(memory.id)}
                  >
                    删除
                  </button>
                </span>
              </>
            )}
          </div>
        ))}
        <form className="model-form" onSubmit={addMemory}>
          <label>
            添加记忆
            <textarea
              rows={2}
              value={memoryDraft}
              onChange={(event) => setMemoryDraft(event.target.value)}
              placeholder="例如：回复尽量使用简体中文"
            />
          </label>
          <div className="model-actions">
            <button type="submit" disabled={memoryBusy || !memoryDraft.trim()}>
              添加记忆
            </button>
            <button
              type="button"
              className="secondary"
              disabled={memoryBusy || (memories ?? []).length === 0}
              onClick={exportMemory}
            >
              导出 Markdown
            </button>
          </div>
        </form>
        <ErrorNotice error={memoryError} />
      </section>
    )
  );
}
