import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';

// The `User` interface in AuthContext is not currently exported, and the brief
// forbids editing that file. We mirror its public shape here so callers can
// pass the same object without type gymnastics.
export interface User {
  email: string;
  name: string;
  picture: string;
  is_admin: boolean;
}

export interface UserMenuProps {
  open: boolean;
  anchorRef: React.RefObject<HTMLElement | null>;
  user: User;
  isAdmin: boolean;
  currentWorkspaceName: string;
  onClose: () => void;
  onOpenSettings: () => void;
  onOpenAdmin: () => void;
  onLogout: () => void;
}

const MOBILE_QUERY = '(max-width: 479px)';
const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

type Position = { left: number; bottom: number };

function useMatchMedia(query: string): boolean {
  const [matches, setMatches] = useState<boolean>(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  );
  useEffect(() => {
    const mql = window.matchMedia(query);
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches);
    mql.addEventListener('change', handler);
    return () => mql.removeEventListener('change', handler);
  }, [query]);
  return matches;
}

// Compact inline icons. stroke-width 1.5 for crispness per brief.
const Icon = {
  Triangle: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 5 L20 19 L4 19 Z" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  ),
  ArrowRight: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 12h14M13 6l6 6-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  Chevron: () => (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M6 9l6 6 6-6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  Shield: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6l8-3z" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  ),
  Chat: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M4 5h16v11H8l-4 4z" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  ),
  Paint: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="3" y="4" width="18" height="6" rx="1" stroke="currentColor" strokeWidth="1.5" />
      <path d="M7 10v3h10v-1a2 2 0 0 1 2-2M11 13v3a2 2 0 0 0 2 2h0a2 2 0 0 1-2 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  Sparkle: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5 18 18M6 18l2.5-2.5M15.5 8.5 18 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  Logout: () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M15 3h4a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1h-4M10 17l-5-5 5-5M5 12h11" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
};

function monogram(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

function Avatar({ src, name }: { src: string; name: string }) {
  const [errored, setErrored] = useState(false);
  if (!src || errored) {
    return (
      <div
        aria-hidden="true"
        className="flex items-center justify-center shrink-0"
        style={{
          width: 40,
          height: 40,
          borderRadius: '50%',
          backgroundColor: 'var(--bg-input)',
          border: '1px solid var(--border-dim)',
          color: 'var(--text-secondary)',
          fontFamily: "'JetBrains Mono', monospace",
          fontSize: 13,
          letterSpacing: '0.05em',
        }}
      >
        {monogram(name)}
      </div>
    );
  }
  return (
    <img
      src={src}
      alt=""
      width={40}
      height={40}
      onError={() => setErrored(true)}
      style={{
        width: 40,
        height: 40,
        borderRadius: '50%',
        objectFit: 'cover',
        border: '1px solid var(--border-dim)',
        flexShrink: 0,
      }}
    />
  );
}

export function UserMenu({
  open,
  anchorRef,
  user,
  isAdmin,
  currentWorkspaceName,
  onClose,
  onOpenSettings,
  onOpenAdmin,
  onLogout,
}: UserMenuProps) {
  const { t, i18n } = useTranslation('desktop');
  const isMobile = useMatchMedia(MOBILE_QUERY);
  const reducedMotion = useMatchMedia(REDUCED_MOTION_QUERY);

  const panelRef = useRef<HTMLDivElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const [focusedIndex, setFocusedIndex] = useState(0);
  const [position, setPosition] = useState<Position>({ left: 0, bottom: 0 });
  const [isLoggingOut, setIsLoggingOut] = useState(false);
  const prevOpenRef = useRef(false);
  const prevMobileRef = useRef(isMobile);

  // Compute desktop position from anchor rect; clamp to viewport.
  const recompute = useCallback(() => {
    const anchor = anchorRef.current;
    if (!anchor) return;
    const rect = anchor.getBoundingClientRect();
    const left = Math.max(8, rect.right + 8);
    const bottom = Math.max(8, window.innerHeight - rect.bottom);
    setPosition({ left, bottom });
  }, [anchorRef]);

  useLayoutEffect(() => {
    if (!open || isMobile) return;
    recompute();
  }, [open, isMobile, recompute]);

  useEffect(() => {
    if (!open || isMobile) return;
    const onResize = () => recompute();
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [open, isMobile, recompute]);

  // Close when breakpoint crosses while open.
  useEffect(() => {
    if (!open) {
      prevMobileRef.current = isMobile;
      return;
    }
    if (prevMobileRef.current !== isMobile) {
      prevMobileRef.current = isMobile;
      onClose();
    }
  }, [isMobile, open, onClose]);

  // Outside mousedown + Esc → close; Esc returns focus to anchor.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node | null;
      if (
        panelRef.current &&
        target &&
        !panelRef.current.contains(target) &&
        !(anchorRef.current && anchorRef.current.contains(target))
      ) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        anchorRef.current?.focus();
      }
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, onClose, anchorRef]);

  // Focus first interactive row on open.
  useEffect(() => {
    if (open && !prevOpenRef.current) {
      setFocusedIndex(0);
      setIsLoggingOut(false);
      // Defer to allow portal mount + layout.
      requestAnimationFrame(() => {
        itemRefs.current[0]?.focus();
      });
    }
    prevOpenRef.current = open;
  }, [open]);

  if (!open) return null;

  // Build the ordered list of interactive rows. Each entry carries its click.
  type Row = {
    key: string;
    label: string;
    trailing?: React.ReactNode;
    leading?: React.ReactNode;
    onActivate: () => void;
    destructive?: boolean;
    describedBy?: string;
  };

  const rows: Row[] = [];

  // Workspace row — the triangle glyph + name + "Cambiar" ghost button.
  // Per brief: activating the row calls onClose() as a stubbed switch.
  rows.push({
    key: 'workspace',
    leading: <Icon.Triangle />,
    label: currentWorkspaceName,
    trailing: (
      <span
        style={{
          fontFamily: "'JetBrains Mono', monospace",
          fontSize: 11,
          color: 'var(--accent)',
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
        }}
      >
        {t('userMenu.workspace.switch')}
      </span>
    ),
    onActivate: () => {
      onClose();
    },
  });

  // Preferences: language (dispatches global event), theme (placeholder),
  // model (placeholder). Theme/model are focusable no-ops until wired.
  rows.push({
    key: 'pref-language',
    leading: <Icon.Chat />,
    label: t('userMenu.preferences.language'),
    trailing: (
      <span className="flex items-center gap-1" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", fontSize: 11 }}>
        {i18n.language.toUpperCase()}
        <Icon.Chevron />
      </span>
    ),
    onActivate: () => {
      window.dispatchEvent(new CustomEvent('liwaisi:openLanguageSwitcher'));
      onClose();
    },
  });
  rows.push({
    key: 'pref-theme',
    leading: <Icon.Paint />,
    label: t('userMenu.preferences.theme'),
    trailing: (
      <span className="flex items-center gap-1" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", fontSize: 11 }}>
        Oscuro
        <Icon.Chevron />
      </span>
    ),
    onActivate: () => {
      /* placeholder — real theme writeback wired by sibling agent */
    },
  });
  rows.push({
    key: 'pref-model',
    leading: <Icon.Sparkle />,
    label: t('userMenu.preferences.model'),
    trailing: (
      <span className="flex items-center gap-1" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", fontSize: 11 }}>
        Auto
        <Icon.Chevron />
      </span>
    ),
    onActivate: () => {
      /* placeholder */
    },
  });

  rows.push({
    key: 'more-settings',
    label: t('userMenu.moreSettings'),
    trailing: <Icon.ArrowRight />,
    onActivate: () => {
      onOpenSettings();
      onClose();
    },
  });

  if (isAdmin) {
    rows.push({
      key: 'admin',
      leading: <Icon.Shield />,
      label: t('userMenu.admin'),
      onActivate: () => {
        onOpenAdmin();
        onClose();
      },
    });
  }

  rows.push({
    key: 'logout',
    leading: <Icon.Logout />,
    label: t('userMenu.logout'),
    destructive: true,
    describedBy: 'user-menu-logout-desc',
    onActivate: () => {
      if (isLoggingOut) return;
      setIsLoggingOut(true);
      onLogout();
    },
  });

  const total = rows.length;

  const moveFocus = (next: number) => {
    const clamped = (next + total) % total;
    setFocusedIndex(clamped);
    itemRefs.current[clamped]?.focus();
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        moveFocus(focusedIndex + 1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        moveFocus(focusedIndex - 1);
        break;
      case 'Home':
        e.preventDefault();
        moveFocus(0);
        break;
      case 'End':
        e.preventDefault();
        moveFocus(total - 1);
        break;
      case 'Tab': {
        // Focus trap: wrap within the rows.
        e.preventDefault();
        moveFocus(focusedIndex + (e.shiftKey ? -1 : 1));
        break;
      }
    }
  };

  const headerStyle: React.CSSProperties = {
    fontFamily: "'JetBrains Mono', monospace",
    fontSize: 10,
    textTransform: 'uppercase',
    letterSpacing: '0.1em',
    color: 'var(--text-muted)',
    padding: '10px 12px 4px',
  };

  const dividerStyle: React.CSSProperties = {
    height: 1,
    backgroundColor: 'var(--border-dim)',
    margin: '4px 0',
  };

  const panelBaseStyle: React.CSSProperties = {
    position: 'fixed',
    border: '1px solid var(--border-dim)',
    boxShadow: '0 16px 48px rgba(0,0,0,0.4)',
    color: 'var(--text-primary)',
    zIndex: 1000,
    outline: 'none',
  };

  const panelStyle: React.CSSProperties = isMobile
    ? {
        ...panelBaseStyle,
        left: 0,
        right: 0,
        bottom: 0,
        borderRadius: '16px 16px 0 0',
        maxHeight: '60vh',
        overflowY: 'auto',
      }
    : {
        ...panelBaseStyle,
        left: position.left,
        bottom: position.bottom,
        width: 320,
        borderRadius: 12,
        maxHeight: 'calc(100vh - 16px)',
        overflowY: 'auto',
      };

  const animationStyle: React.CSSProperties = reducedMotion
    ? {}
    : { animation: 'modal-panel-in 0.18s cubic-bezier(0.16, 1, 0.3, 1)' };

  const renderRow = (row: Row, idx: number) => {
    const focused = idx === focusedIndex;
    const color = row.destructive ? '#ef4444' : 'var(--text-primary)';
    return (
      <button
        key={row.key}
        ref={(el) => {
          itemRefs.current[idx] = el;
        }}
        role="menuitem"
        tabIndex={focused ? 0 : -1}
        aria-describedby={row.describedBy}
        onClick={row.onActivate}
        onFocus={() => setFocusedIndex(idx)}
        onMouseEnter={(e) => {
          e.currentTarget.style.backgroundColor = 'var(--bg-input)';
        }}
        onMouseLeave={(e) => {
          if (idx !== focusedIndex) e.currentTarget.style.backgroundColor = 'transparent';
        }}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          width: '100%',
          padding: '10px 12px',
          border: 'none',
          background: 'transparent',
          backgroundColor: focused ? 'var(--bg-input)' : 'transparent',
          borderLeft: focused ? '2px solid var(--accent)' : '2px solid transparent',
          color,
          fontFamily: "'DM Sans', system-ui, sans-serif",
          fontSize: 13,
          textAlign: 'left',
          transition: reducedMotion ? 'none' : 'background-color 120ms ease',
          outline: 'none',
        }}
      >
        {row.leading ? (
          <span style={{ color: row.destructive ? '#ef4444' : 'var(--text-secondary)', display: 'inline-flex' }}>
            {row.leading}
          </span>
        ) : (
          <span style={{ width: 16, display: 'inline-block' }} />
        )}
        <span
          style={{
            flex: 1,
            minWidth: 0,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {row.label}
        </span>
        {row.trailing}
      </button>
    );
  };

  // Section indices: 0 = workspace, 1..3 = preferences, 4 = more-settings,
  // 5 = admin (optional), last = logout.
  const preferencesStart = 1;
  const preferencesEnd = 4; // exclusive
  const settingsRowIdx = 4;
  const adminRowIdx = isAdmin ? 5 : -1;
  const logoutRowIdx = total - 1;

  const content = (
    <div
      ref={panelRef}
      role="menu"
      aria-label={t('userMenu.openLabel')}
      className="glass-surface"
      style={{ ...panelStyle, ...animationStyle }}
      onKeyDown={handleKeyDown}
    >
      {/* User header (non-interactive) */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          padding: 12,
          minHeight: 56,
        }}
      >
        <Avatar src={user.picture} name={user.name} />
        <div style={{ minWidth: 0, flex: 1 }}>
          <div
            title={user.name}
            style={{
              fontFamily: "'DM Sans', system-ui, sans-serif",
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--text-primary)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {user.name}
          </div>
          <div
            style={{
              fontSize: 11,
              color: 'var(--text-muted)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {user.email}
          </div>
        </div>
      </div>

      <div style={dividerStyle} />

      <div style={headerStyle}>{t('userMenu.workspace.heading')}</div>
      {renderRow(rows[0], 0)}

      <div style={dividerStyle} />

      <div style={headerStyle}>{t('userMenu.preferences.heading')}</div>
      {rows.slice(preferencesStart, preferencesEnd).map((r, i) => renderRow(r, preferencesStart + i))}

      {renderRow(rows[settingsRowIdx], settingsRowIdx)}
      {adminRowIdx >= 0 && renderRow(rows[adminRowIdx], adminRowIdx)}

      <div style={dividerStyle} />

      {renderRow(rows[logoutRowIdx], logoutRowIdx)}

      <span id="user-menu-logout-desc" className="sr-only">
        {t('userMenu.logoutDescription')}
      </span>
    </div>
  );

  return createPortal(content, document.body);
}

export default UserMenu;
