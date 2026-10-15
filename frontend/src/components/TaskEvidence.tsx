export type EvidenceOption = { id: string; label: string };
export function TaskEvidence({
  options,
  value,
  onChange,
}: {
  options: EvidenceOption[];
  value: string;
  onChange: (v: string) => void;
}) {
  const selected = value.split(/[\s,]+/).filter(Boolean);
  return (
    <details>
      <summary>根据最近的工作改进（可选）</summary>
      <p>只使用你明确勾选的工作记录，最多3项。</p>
      {options.length ? (
        options.map((v) => (
          <label className="checkbox-label" key={v.id}>
            <input
              type="checkbox"
              checked={selected.includes(v.id)}
              disabled={!selected.includes(v.id) && selected.length >= 3}
              onChange={(e) =>
                onChange(
                  (e.target.checked
                    ? [...selected, v.id]
                    : selected.filter((id) => id !== v.id)
                  ).join(","),
                )
              }
            />
            {v.label}
          </label>
        ))
      ) : (
        <p>完成一次工作后，可以在这里选择它作为参考。</p>
      )}
    </details>
  );
}
