import { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { SUPPORTED_LANGUAGES } from '../../i18n/config';

interface LanguageSwitcherProps {
  variant: 'landing' | 'desktop';
  className?: string;
}

export function LanguageSwitcher({ variant, className = '' }: LanguageSwitcherProps) {
  const { i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const [focusedIndex, setFocusedIndex] = useState(-1);
  const [announcement, setAnnouncement] = useState('');
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLUListElement>(null);

  const currentLang = i18n.language;
  const currentIndex = SUPPORTED_LANGUAGES.findIndex((l) => l.code === currentLang);

  const closeDropdown = useCallback(() => {
    setOpen(false);
    setFocusedIndex(-1);
    triggerRef.current?.focus();
  }, []);

  const selectLanguage = useCallback(
    (code: string) => {
      i18n.changeLanguage(code);
      const meta = SUPPORTED_LANGUAGES.find((l) => l.code === code);
      setAnnouncement(`Language changed to ${meta?.name ?? code}`);
      closeDropdown();
    },
    [i18n, closeDropdown],
  );

  // Auto-open when the UserMenu or CommandPalette fires the global event.
  useEffect(() => {
    const handler = () => setOpen(true);
    window.addEventListener('liwaisi:openLanguageSwitcher', handler);
    return () => window.removeEventListener('liwaisi:openLanguageSwitcher', handler);
  }, []);

  // Close on outside click or Escape
  useEffect(() => {
    if (!open) return;

    const handleClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        closeDropdown();
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeDropdown();
    };

    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKey);
    };
  }, [open, closeDropdown]);

  // Focus the active item when dropdown opens
  useEffect(() => {
    if (open && listRef.current) {
      const idx = currentIndex >= 0 ? currentIndex : 0;
      setFocusedIndex(idx);
      const items = listRef.current.querySelectorAll<HTMLLIElement>('[role="option"]');
      items[idx]?.focus();
    }
  }, [open, currentIndex]);

  const handleTriggerKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
      e.preventDefault();
      setOpen(true);
    }
  };

  const handleListKeyDown = (e: React.KeyboardEvent) => {
    const len = SUPPORTED_LANGUAGES.length;

    switch (e.key) {
      case 'ArrowDown': {
        e.preventDefault();
        const next = (focusedIndex + 1) % len;
        setFocusedIndex(next);
        const items = listRef.current?.querySelectorAll<HTMLLIElement>('[role="option"]');
        items?.[next]?.focus();
        break;
      }
      case 'ArrowUp': {
        e.preventDefault();
        const prev = (focusedIndex - 1 + len) % len;
        setFocusedIndex(prev);
        const items = listRef.current?.querySelectorAll<HTMLLIElement>('[role="option"]');
        items?.[prev]?.focus();
        break;
      }
      case 'Enter':
      case ' ': {
        e.preventDefault();
        if (focusedIndex >= 0 && focusedIndex < len) {
          selectLanguage(SUPPORTED_LANGUAGES[focusedIndex].code);
        }
        break;
      }
      case 'Escape': {
        e.preventDefault();
        closeDropdown();
        break;
      }
    }
  };

  const isLanding = variant === 'landing';

  const triggerBaseStyle: React.CSSProperties = {
    fontFamily: "'JetBrains Mono', monospace",
    color: open ? 'var(--accent)' : 'var(--text-secondary)',
    backgroundColor: open ? 'var(--bg-input)' : 'transparent',
    border: isLanding ? '1px solid' : '1px solid transparent',
    borderColor: open
      ? 'var(--accent)'
      : isLanding
        ? 'var(--border-dim)'
        : 'transparent',
    boxShadow: open ? '0 0 12px -2px var(--accent-glow)' : 'none',
    transition: 'all 200ms',
    cursor: 'pointer',
  };

  return (
    <div ref={containerRef} className={`relative ${className}`}>
      {/* Trigger button */}
      <button
        ref={triggerRef}
        onClick={() => setOpen((p) => !p)}
        onKeyDown={handleTriggerKeyDown}
        className={`flex items-center gap-1.5 text-xs font-medium ${
          isLanding ? 'px-3 py-1.5 rounded-full' : 'px-2 py-1 rounded-md'
        }`}
        style={triggerBaseStyle}
        onMouseEnter={(e) => {
          if (!open) {
            e.currentTarget.style.borderColor = 'var(--accent)';
            e.currentTarget.style.color = 'var(--accent)';
            e.currentTarget.style.boxShadow = '0 0 12px -2px var(--accent-glow)';
          }
        }}
        onMouseLeave={(e) => {
          if (!open) {
            e.currentTarget.style.borderColor = isLanding ? 'var(--border-dim)' : 'transparent';
            e.currentTarget.style.color = 'var(--text-secondary)';
            e.currentTarget.style.boxShadow = 'none';
          }
        }}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-label="Select language"
      >
        {/* Globe icon */}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          className="shrink-0"
          aria-hidden="true"
          style={{
            transform: open ? 'rotate(15deg)' : 'rotate(0deg)',
            transition: 'transform 200ms ease',
          }}
        >
          <circle cx="12" cy="12" r="9.5" stroke="currentColor" strokeWidth="1.5" />
          <ellipse cx="12" cy="12" rx="4" ry="9.5" stroke="currentColor" strokeWidth="1.5" />
          <path d="M3 12h18" stroke="currentColor" strokeWidth="1.5" />
          <path d="M4.5 7.5h15" stroke="currentColor" strokeWidth="1" opacity="0.5" />
          <path d="M4.5 16.5h15" stroke="currentColor" strokeWidth="1" opacity="0.5" />
        </svg>
        {currentLang.toUpperCase()}
      </button>

      {/* Dropdown */}
      {open && (
        <div
          className="signin-panel absolute right-0 top-full mt-2 w-52 rounded-xl border overflow-hidden"
          style={{
            backgroundColor: 'rgba(18, 18, 26, 0.95)',
            backdropFilter: 'blur(20px)',
            WebkitBackdropFilter: 'blur(20px)',
            borderColor: 'var(--border-dim)',
            boxShadow:
              '0 0 0 1px var(--border-dim), 0 0 32px -8px var(--accent-glow), 0 16px 48px -16px rgba(0,0,0,0.7)',
          }}
        >
          {/* Top accent line */}
          <div
            className="h-px w-full"
            style={{ background: 'linear-gradient(90deg, transparent, var(--accent), transparent)' }}
          />

          <ul
            ref={listRef}
            role="listbox"
            aria-label="Available languages"
            onKeyDown={handleListKeyDown}
            className="py-1.5"
          >
            {SUPPORTED_LANGUAGES.map((lang, idx) => {
              const isActive = lang.code === currentLang;
              const isFocused = idx === focusedIndex;

              return (
                <li
                  key={lang.code}
                  role="option"
                  tabIndex={-1}
                  aria-selected={isActive}
                  onClick={() => selectLanguage(lang.code)}
                  className="flex items-center justify-between px-4 py-2 cursor-pointer outline-none transition-colors"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    backgroundColor: isFocused ? 'var(--bg-input)' : 'transparent',
                    color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                  }}
                  onMouseEnter={(e) => {
                    setFocusedIndex(idx);
                    e.currentTarget.style.backgroundColor = 'var(--bg-input)';
                    if (!isActive) e.currentTarget.style.color = 'var(--accent)';
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.backgroundColor = 'transparent';
                    if (!isActive) e.currentTarget.style.color = 'var(--text-secondary)';
                  }}
                  onFocus={(e) => {
                    e.currentTarget.style.backgroundColor = 'var(--bg-input)';
                  }}
                  onBlur={(e) => {
                    e.currentTarget.style.backgroundColor = 'transparent';
                  }}
                >
                  <span className="flex items-center gap-2.5">
                    <span className="text-[10px] uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
                      {lang.code}
                    </span>
                    <span className="text-xs">{lang.name}</span>
                  </span>

                  {isActive && (
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                      <path
                        d="M5 13l4 4L19 7"
                        stroke="var(--accent)"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      />
                    </svg>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {/* Screen reader announcements */}
      <div aria-live="polite" className="sr-only">
        {announcement}
      </div>
    </div>
  );
}
