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
          记忆按本机上下文归属：切换 Profile 不清空、共享 Profile 不共享、复制
          Profile
          不携带。记忆会注入后续运行的提示词，其数据范围计入外发检查；关闭记忆只停止读写，内容保留到你手动删除。
        </p>
        {(memories ?? []).length === 0 && (
          <p className="hint">
            还没有长期记忆。模型在获准运行中可调用 local__memory_save
            保存，也可以在下方手动添加。
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
        {memoryError && <p className="error">{memoryError}</p>}
      </section>
    )
  );
}
