import { useState, useCallback } from 'react';
import type { PrincipleResponse, UpdatePrincipleRequest } from '../../types/personality';

interface PrincipleEditorProps {
  principle: PrincipleResponse;
  color: string;
  onSave: (data: UpdatePrincipleRequest) => Promise<void>;
  saving: boolean;
}

const colorMap: Record<string, { bg: string; text: string; border: string; badge: string }> = {
  'sky-500':    { bg: 'rgba(14, 165, 233, 0.08)', text: '#38bdf8', border: 'rgba(14, 165, 233, 0.3)', badge: 'bg-sky-500/20 text-sky-400' },
  'amber-500':  { bg: 'rgba(245, 158, 11, 0.08)', text: '#fbbf24', border: 'rgba(245, 158, 11, 0.3)', badge: 'bg-amber-500/20 text-amber-400' },
  'emerald-500': { bg: 'rgba(16, 185, 129, 0.08)', text: '#34d399', border: 'rgba(16, 185, 129, 0.3)', badge: 'bg-emerald-500/20 text-emerald-400' },
};

const lockIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
    <path d="M7 11V7a5 5 0 0110 0v4" />
  </svg>
);

const editIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7" />
    <path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z" />
  </svg>
);

export function PrincipleEditor({ principle, color, onSave, saving }: PrincipleEditorProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [title, setTitle] = useState(principle.title);
  const [description, setDescription] = useState(principle.description);
  const [rules, setRules] = useState<string[]>(principle.rules);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const colors = colorMap[color] ?? colorMap['sky-500'];
  const isEtica = principle.kind === 'etica';

  const handleEditToggle = useCallback(() => {
    if (isEditing) {
      // Cancel edit
      setTitle(principle.title);
      setDescription(principle.description);
      setRules(principle.rules);
    }
    setIsEditing((prev) => !prev);
  }, [isEditing, principle]);

  const handleSave = useCallback(async () => {
    setConfirmOpen(false);
    const data: UpdatePrincipleRequest = {};
    if (title !== principle.title) data.title = title;
    if (description !== principle.description) data.description = description;
    if (JSON.stringify(rules) !== JSON.stringify(principle.rules)) data.rules = rules;

    try {
      await onSave(data);
      setIsEditing(false);
    } catch {
      // Error handled by parent
    }
  }, [title, description, rules, principle, onSave]);

  const handleRuleChange = useCallback((index: number, value: string) => {
    setRules((prev) => {
      const next = [...prev];
      next[index] = value;
      return next;
    });
  }, []);

  const handleAddRule = useCallback(() => {
    setRules((prev) => [...prev, '']);
  }, []);

  const handleRemoveRule = useCallback((index: number) => {
    if (isEtica && rules.length <= 1) return;
    setRules((prev) => prev.filter((_, i) => i !== index));
  }, [isEtica, rules.length]);

  return (
    <div
      className="rounded-xl border p-4 transition-all duration-200"
      style={{
        backgroundColor: colors.bg,
        borderColor: colors.border,
      }}
    >
      {/* Header */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wider ${colors.badge}`}
            style={{ fontFamily: "'JetBrains Mono', monospace" }}
          >
            {isEtica && <span className="opacity-70">{lockIcon}</span>}
            {principle.kind}
          </span>
        </div>

        <button
          onClick={handleEditToggle}
          className="flex items-center gap-1.5 px-2 py-1 rounded-lg text-[11px] font-medium transition-colors duration-150"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: isEditing ? 'var(--text-muted)' : colors.text,
            backgroundColor: isEditing ? 'rgba(255,255,255,0.05)' : 'transparent',
            border: `1px solid ${isEditing ? 'var(--border-dim)' : 'transparent'}`,
          }}
          onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = 'rgba(255,255,255,0.08)'; }}
          onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = isEditing ? 'rgba(255,255,255,0.05)' : 'transparent'; }}
        >
          {editIcon}
          {isEditing ? 'Cancel' : 'Edit'}
        </button>
      </div>

      {/* Title */}
      {isEditing ? (
        <input
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          className="w-full mb-2 px-3 py-1.5 rounded-lg text-sm font-semibold outline-none transition-colors"
          style={{
            backgroundColor: 'var(--bg-input)',
            color: 'var(--text-primary)',
            border: '1px solid var(--border-dim)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
          onFocus={(e) => { e.currentTarget.style.borderColor = colors.border; }}
          onBlur={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; }}
        />
      ) : (
        <h3
          className="text-sm font-semibold mb-1"
          style={{ color: colors.text, fontFamily: "'JetBrains Mono', monospace" }}
        >
          {principle.title}
        </h3>
      )}

      {/* Description */}
      {isEditing ? (
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={2}
          className="w-full mb-3 px-3 py-1.5 rounded-lg text-xs outline-none transition-colors resize-none"
          style={{
            backgroundColor: 'var(--bg-input)',
            color: 'var(--text-secondary)',
            border: '1px solid var(--border-dim)',
          }}
          onFocus={(e) => { e.currentTarget.style.borderColor = colors.border; }}
          onBlur={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; }}
        />
      ) : (
        <p className="text-xs mb-3" style={{ color: 'var(--text-secondary)' }}>
          {principle.description}
        </p>
      )}

      {/* Rules */}
      <div className="space-y-1.5">
        <span
          className="text-[10px] font-semibold uppercase tracking-wider"
          style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          Rules
        </span>
        {isEditing ? (
          <div className="space-y-1.5">
            {rules.map((rule, i) => (
              <div key={i} className="flex items-center gap-2">
                <input
                  type="text"
                  value={rule}
                  onChange={(e) => handleRuleChange(i, e.target.value)}
                  className="flex-1 px-2.5 py-1 rounded text-xs outline-none transition-colors"
                  style={{
                    backgroundColor: 'var(--bg-input)',
                    color: 'var(--text-primary)',
                    border: '1px solid var(--border-dim)',
                  }}
                  onFocus={(e) => { e.currentTarget.style.borderColor = colors.border; }}
                  onBlur={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; }}
                />
                {!(isEtica && rules.length <= 1) && (
                  <button
                    onClick={() => handleRemoveRule(i)}
                    className="shrink-0 w-6 h-6 flex items-center justify-center rounded text-xs transition-colors"
                    style={{ color: 'var(--text-muted)' }}
                    onMouseEnter={(e) => { e.currentTarget.style.color = '#ef4444'; }}
                    onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-muted)'; }}
                    aria-label="Remove rule"
                  >
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                    </svg>
                  </button>
                )}
              </div>
            ))}
            <button
              onClick={handleAddRule}
              className="text-[11px] px-2 py-1 rounded transition-colors"
              style={{ color: colors.text, fontFamily: "'JetBrains Mono', monospace" }}
              onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = 'rgba(255,255,255,0.05)'; }}
              onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = 'transparent'; }}
            >
              + Add rule
            </button>
          </div>
        ) : (
          <ul className="space-y-1">
            {principle.rules.map((rule, i) => (
              <li key={i} className="flex items-start gap-2 text-xs" style={{ color: 'var(--text-secondary)' }}>
                <span className="shrink-0 mt-1 w-1 h-1 rounded-full" style={{ backgroundColor: colors.text }} />
                {rule}
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Save button */}
      {isEditing && (
        <div className="mt-4 flex items-center gap-2">
          {!confirmOpen ? (
            <button
              onClick={() => setConfirmOpen(true)}
              disabled={saving}
              className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-all duration-150"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                backgroundColor: colors.border,
                color: colors.text,
                opacity: saving ? 0.5 : 1,
              }}
              onMouseEnter={(e) => { if (!saving) e.currentTarget.style.boxShadow = `0 0 12px -2px ${colors.border}`; }}
              onMouseLeave={(e) => { e.currentTarget.style.boxShadow = 'none'; }}
            >
              {saving ? 'Saving...' : 'Save Changes'}
            </button>
          ) : (
            <div className="flex items-center gap-2">
              <span className="text-[11px]" style={{ color: 'var(--text-muted)' }}>Confirm save?</span>
              <button
                onClick={handleSave}
                disabled={saving}
                className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  backgroundColor: 'rgba(16, 185, 129, 0.2)',
                  color: '#34d399',
                }}
              >
                {saving ? 'Saving...' : 'Yes, save'}
              </button>
              <button
                onClick={() => setConfirmOpen(false)}
                className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-muted)',
                }}
              >
                Cancel
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
