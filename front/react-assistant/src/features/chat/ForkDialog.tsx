import { useState, useCallback, useEffect } from 'react';
import type { ChatMessage } from '../../types/chat';

interface ForkDialogProps {
  messages: ChatMessage[];
  onFork: (messageIndex: number) => void;
  onClose: () => void;
}

export function ForkDialog({ messages, onFork, onClose }: ForkDialogProps) {
  const maxIndex = messages.length - 1;
  const [forkIndex, setForkIndex] = useState(maxIndex);

  // Close on escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [onClose]);

  const handleConfirm = useCallback(() => {
    onFork(forkIndex);
  }, [onFork, forkIndex]);

  if (messages.length === 0) return null;

  const copiedCount = forkIndex + 1;

  return (
    // Backdrop
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ backgroundColor: 'rgba(0, 0, 0, 0.6)', backdropFilter: 'blur(4px)' }}
      onClick={onClose}
    >
      {/* Dialog card */}
      <div
        className="glass-surface rounded-xl border shadow-2xl w-full max-w-lg mx-4 flex flex-col max-h-[80vh]"
        style={{ borderColor: 'var(--border-dim)' }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-5 py-4 border-b flex-none" style={{ borderColor: 'var(--border-dim)' }}>
          <h3
            className="text-base font-semibold"
            style={{
              color: 'var(--text-primary)',
              fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
            }}
          >
            Fork Conversation
          </h3>
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Select the fork point. Messages up to this point will be copied.
          </p>
        </div>

        {/* Message list */}
        <div className="flex-1 overflow-y-auto chat-scroll px-5 py-3 space-y-1.5">
          {messages.map((msg, idx) => {
            const included = idx <= forkIndex;
            return (
              <button
                key={msg.id}
                onClick={() => setForkIndex(idx)}
                className="w-full text-left px-3 py-2 rounded-lg border transition-all duration-150 flex items-start gap-2"
                style={{
                  backgroundColor: included
                    ? (idx === forkIndex ? 'rgba(14, 165, 233, 0.12)' : 'rgba(14, 165, 233, 0.04)')
                    : 'transparent',
                  borderColor: idx === forkIndex ? 'var(--accent)' : (included ? 'var(--border-dim)' : 'transparent'),
                  opacity: included ? 1 : 0.35,
                }}
              >
                {/* Role indicator */}
                <span
                  className="flex-none text-[10px] font-semibold uppercase mt-0.5 w-10"
                  style={{
                    color: msg.role === 'user' ? 'var(--accent)' : 'var(--text-muted)',
                    fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
                  }}
                >
                  {msg.role === 'user' ? 'You' : 'AI'}
                </span>
                {/* Content preview */}
                <span
                  className="flex-1 min-w-0 truncate text-xs leading-relaxed"
                  style={{ color: 'var(--text-secondary)' }}
                >
                  {msg.content.slice(0, 120)}{msg.content.length > 120 ? '...' : ''}
                </span>
                {/* Index badge */}
                <span
                  className="flex-none text-[9px] mt-0.5"
                  style={{
                    color: 'var(--text-muted)',
                    fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
                  }}
                >
                  #{idx + 1}
                </span>
              </button>
            );
          })}
        </div>

        {/* Slider */}
        <div className="px-5 py-3 border-t flex-none" style={{ borderColor: 'var(--border-dim)' }}>
          <input
            type="range"
            min={0}
            max={maxIndex}
            value={forkIndex}
            onChange={(e) => setForkIndex(Number(e.target.value))}
            className="w-full accent-[var(--accent)]"
            aria-label="Fork point selector"
          />
          <p className="text-xs mt-2 text-center" style={{ color: 'var(--text-secondary)' }}>
            <span style={{ color: 'var(--accent)', fontWeight: 600 }}>{copiedCount}</span>
            {' '}message{copiedCount !== 1 ? 's' : ''} will be copied.
            {' '}Cost resets to{' '}
            <span
              style={{
                color: 'var(--accent)',
                fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
              }}
            >
              $0.00
            </span>
          </p>
        </div>

        {/* Actions */}
        <div
          className="px-5 py-3 border-t flex items-center justify-end gap-3 flex-none"
          style={{ borderColor: 'var(--border-dim)' }}
        >
          <button
            onClick={onClose}
            className="px-4 py-1.5 rounded-lg text-xs font-medium transition-colors hover:bg-white/5"
            style={{ color: 'var(--text-secondary)' }}
          >
            Cancel
          </button>
          <button
            onClick={handleConfirm}
            className="px-4 py-1.5 rounded-lg text-xs font-semibold transition-colors"
            style={{
              backgroundColor: 'var(--accent)',
              color: '#fff',
            }}
          >
            Fork
          </button>
        </div>
      </div>
    </div>
  );
}
