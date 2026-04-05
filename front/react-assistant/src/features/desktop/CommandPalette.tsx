import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';

export interface PaletteAction {
  id: string;
  label: string;
  category: string;
  shortcut?: string;
  icon?: React.ReactNode;
  keywords?: string[];
  handler: () => void;
}

interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  actions: PaletteAction[];
}

export function CommandPalette({ isOpen, onClose, actions }: CommandPaletteProps) {
  const { t } = useTranslation('desktop');
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [animState, setAnimState] = useState<'entering' | 'visible' | 'exiting' | 'hidden'>('hidden');

  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<Element | null>(null);

  // Filter actions with fuzzy (substring) matching
  const filtered = useMemo(() => {
    const q = query.toLowerCase().trim();
    if (!q) return actions;
    return actions.filter((a) => {
      if (a.label.toLowerCase().includes(q)) return true;
      if (a.category.toLowerCase().includes(q)) return true;
      if (a.keywords?.some((kw) => kw.toLowerCase().includes(q))) return true;
      return false;
    });
  }, [actions, query]);

  // Group filtered actions by category in stable order (preserving insertion order)
  const grouped = useMemo(() => {
    const map = new Map<string, PaletteAction[]>();
    for (const action of filtered) {
      const list = map.get(action.category) ?? [];
      list.push(action);
      map.set(action.category, list);
    }
    const result: { category: string; items: PaletteAction[] }[] = [];
    for (const [cat, items] of map) {
      if (items.length > 0) result.push({ category: cat, items });
    }
    return result;
  }, [filtered]);

  // Flat list for keyboard navigation
  const flatItems = useMemo(() => grouped.flatMap((g) => g.items), [grouped]);

  // Reset selection when filtered results change
  useEffect(() => {
    setSelectedIndex(0);
  }, [filtered]);

  // Animation control
  useEffect(() => {
    if (isOpen) {
      previousFocusRef.current = document.activeElement;
      setAnimState('entering');
      const timer = requestAnimationFrame(() => {
        setAnimState('visible');
      });
      return () => cancelAnimationFrame(timer);
    } else if (animState === 'visible' || animState === 'entering') {
      setAnimState('exiting');
      const timer = setTimeout(() => {
        setAnimState('hidden');
        setQuery('');
        setSelectedIndex(0);
        // Restore focus
        if (previousFocusRef.current instanceof HTMLElement) {
          previousFocusRef.current.focus();
        }
      }, 100);
      return () => clearTimeout(timer);
    }
  }, [isOpen]); // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-focus input when opened
  useEffect(() => {
    if (animState === 'entering' || animState === 'visible') {
      inputRef.current?.focus();
    }
  }, [animState]);

  // Execute the selected action
  const executeAction = useCallback(
    (action: PaletteAction) => {
      action.handler();
      onClose();
    },
    [onClose],
  );

  // Keyboard handling
  useEffect(() => {
    if (animState === 'hidden' || animState === 'exiting') return;

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }

      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((prev) => (flatItems.length === 0 ? 0 : (prev + 1) % flatItems.length));
        return;
      }

      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((prev) =>
          flatItems.length === 0 ? 0 : (prev - 1 + flatItems.length) % flatItems.length,
        );
        return;
      }

      if (e.key === 'Enter') {
        e.preventDefault();
        if (flatItems[selectedIndex]) {
          executeAction(flatItems[selectedIndex]);
        }
        return;
      }

      // Focus trap
      if (e.key === 'Tab') {
        const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        );
        if (!focusable || focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [animState, flatItems, selectedIndex, onClose, executeAction]);

  // Scroll selected item into view
  useEffect(() => {
    if (!listRef.current || flatItems.length === 0) return;
    const selectedId = flatItems[selectedIndex]?.id;
    if (!selectedId) return;
    const el = listRef.current.querySelector(`[data-action-id="${selectedId}"]`);
    el?.scrollIntoView({ block: 'nearest' });
  }, [selectedIndex, flatItems]);

  if (animState === 'hidden') return null;

  const isVisible = animState === 'visible';
  const selectedAction = flatItems[selectedIndex];

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
      style={{
        backgroundColor: 'rgba(0, 0, 0, 0.5)',
        opacity: isVisible ? 1 : 0,
        transition: animState === 'exiting' ? 'opacity 100ms ease-in' : 'opacity 150ms ease-out',
      }}
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={t('commandPalette.ariaLabel')}
    >
      <div
        ref={dialogRef}
        className="w-full rounded-xl border flex flex-col overflow-hidden"
        style={{
          maxWidth: 560,
          maxHeight: '60vh',
          background: 'rgba(18, 18, 26, 0.82)',
          backdropFilter: 'blur(20px)',
          WebkitBackdropFilter: 'blur(20px)',
          borderColor: 'var(--border-dim)',
          boxShadow: '0 0 40px rgba(14, 165, 233, 0.08), 0 25px 50px -12px rgba(0, 0, 0, 0.5)',
          transform: isVisible ? 'scale(1)' : 'scale(0.98)',
          opacity: isVisible ? 1 : 0,
          transition:
            animState === 'exiting'
              ? 'transform 100ms ease-in, opacity 100ms ease-in'
              : 'transform 150ms ease-out, opacity 150ms ease-out',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search input */}
        <div className="flex-none px-4 py-3 border-b" style={{ borderColor: 'var(--border-dim)' }}>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('commandPalette.searchPlaceholder')}
            className="w-full px-3 py-2 rounded-lg text-sm outline-none transition-colors"
            style={{
              backgroundColor: 'var(--bg-input)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-dim)',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
            onFocus={(e) => (e.currentTarget.style.borderColor = 'var(--border-glow)')}
            onBlur={(e) => (e.currentTarget.style.borderColor = 'var(--border-dim)')}
            aria-label={t('commandPalette.searchAriaLabel')}
            aria-controls="command-palette-list"
            aria-activedescendant={selectedAction ? `palette-item-${selectedAction.id}` : undefined}
          />
        </div>

        {/* Results list */}
        <div
          ref={listRef}
          className="flex-1 overflow-y-auto chat-scroll py-2"
          role="listbox"
          id="command-palette-list"
          aria-label={t('commandPalette.resultsAriaLabel')}
        >
          {flatItems.length === 0 ? (
            <p
              className="text-sm text-center py-8"
              style={{
                color: 'var(--text-muted)',
                fontFamily: "'DM Sans', system-ui, sans-serif",
              }}
            >
              {t('commandPalette.noResults')}
            </p>
          ) : (
            grouped.map((group) => (
              <div key={group.category}>
                {/* Category label */}
                <div
                  className="px-4 py-1.5 text-[10px] font-semibold uppercase tracking-wider"
                  style={{
                    color: 'var(--text-muted)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                >
                  {group.category}
                </div>

                {/* Items */}
                {group.items.map((action) => {
                  const isSelected = selectedAction?.id === action.id;
                  const itemIndex = flatItems.indexOf(action);
                  return (
                    <div
                      key={action.id}
                      data-action-id={action.id}
                      id={`palette-item-${action.id}`}
                      role="option"
                      aria-selected={isSelected}
                      className="mx-2 px-3 py-2 rounded-lg flex items-center gap-3 cursor-pointer transition-colors duration-75"
                      style={{
                        backgroundColor: isSelected ? 'rgba(14, 165, 233, 0.1)' : 'transparent',
                      }}
                      onClick={() => executeAction(action)}
                      onMouseEnter={() => setSelectedIndex(itemIndex)}
                    >
                      {/* Icon */}
                      {action.icon && (
                        <span
                          className="flex-none w-5 h-5 flex items-center justify-center"
                          style={{ color: 'var(--text-secondary)' }}
                        >
                          {action.icon}
                        </span>
                      )}

                      {/* Label */}
                      <span
                        className="flex-1 text-sm"
                        style={{
                          color: 'var(--text-primary)',
                          fontFamily: "'DM Sans', system-ui, sans-serif",
                        }}
                      >
                        {action.label}
                      </span>

                      {/* Shortcut badge */}
                      {action.shortcut && (
                        <span
                          className="flex-none px-2 py-0.5 rounded text-[11px]"
                          style={{
                            backgroundColor: 'var(--bg-input)',
                            color: 'var(--text-muted)',
                            fontFamily: "'JetBrains Mono', monospace",
                          }}
                        >
                          {action.shortcut}
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            ))
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
