import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useTooltip } from '../../hooks/useTooltip';
import { UserMenu, type User } from './UserMenu';

type ActiveApp = 'chat' | 'flows' | 'monitor' | 'personality' | 'tools' | 'admin' | 'settings';

interface NavigationRailProps {
  activeApp: ActiveApp;
  isExpanded: boolean;
  isAdmin?: boolean;
  onNavigate: (app: ActiveApp) => void;
  onSettingsClick: () => void;
  onToolsClick: () => void;
  onAdminClick?: () => void;
  sidebarContent?: React.ReactNode;
  user?: User | null;
  currentWorkspaceName?: string;
  onOpenUserSettings?: () => void;
  onLogout?: () => void;
}

interface NavItem {
  id: ActiveApp;
  labelKey: string;
  icon: React.ReactNode;
}

const navItems: NavItem[] = [
  {
    id: 'chat',
    labelKey: 'navigationRail.chat',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
      </svg>
    ),
  },
  {
    id: 'flows',
    labelKey: 'navigationRail.flows',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="5" r="2" />
        <circle cx="6" cy="19" r="2" />
        <circle cx="18" cy="19" r="2" />
        <path d="M12 7v4M12 11l-6 6M12 11l6 6" />
      </svg>
    ),
  },
  {
    id: 'monitor',
    labelKey: 'navigationRail.monitor',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
      </svg>
    ),
  },
];

const toolsIcon = (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
  </svg>
);

const adminIcon = (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
  </svg>
);

const settingsIcon = (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z" />
  </svg>
);

function NavButton({
  item,
  isActive,
  isExpanded,
  onClick,
}: {
  item: NavItem;
  isActive: boolean;
  isExpanded: boolean;
  onClick: () => void;
}) {
  const { t } = useTranslation('desktop');
  const tooltip = useTooltip(200);
  const label = t(item.labelKey);

  return (
    <div className="relative">
      <button
        onClick={onClick}
        onMouseEnter={isExpanded ? undefined : tooltip.onMouseEnter}
        onMouseLeave={tooltip.onMouseLeave}
        className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
        style={{
          padding: '10px 12px',
          color: isActive ? 'var(--accent)' : 'var(--text-muted)',
          backgroundColor: isActive ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
          borderLeft: isActive ? '2px solid var(--accent)' : '2px solid transparent',
        }}
        aria-label={label}
        aria-current={isActive ? 'page' : undefined}
      >
        <div className="shrink-0 w-5 h-5 flex items-center justify-center">
          {item.icon}
        </div>
        {isExpanded && (
          <span
            className="text-xs font-medium tracking-wide whitespace-nowrap overflow-hidden"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
            }}
          >
            {label}
          </span>
        )}
      </button>

      {/* Tooltip — only when collapsed */}
      {tooltip.visible && !isExpanded && (
        <div
          className="absolute left-full top-1/2 -translate-y-1/2 ml-2 whitespace-nowrap px-3 py-1.5 rounded-lg text-[10px] font-medium z-50 pointer-events-none"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-dim)',
            color: 'var(--text-secondary)',
            boxShadow: '0 4px 12px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
          }}
        >
          {label}
          <div
            className="absolute right-full top-1/2 -translate-y-1/2 w-2 h-2 rotate-45"
            style={{
              marginRight: '-4px',
              backgroundColor: 'var(--bg-surface)',
              borderLeft: '1px solid var(--border-dim)',
              borderBottom: '1px solid var(--border-dim)',
            }}
          />
        </div>
      )}
    </div>
  );
}

function monogram(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export function NavigationRail({
  activeApp,
  isExpanded,
  isAdmin,
  onNavigate,
  onSettingsClick,
  onToolsClick,
  onAdminClick,
  sidebarContent,
  user,
  currentWorkspaceName = 'Liwaisi Tech',
  onOpenUserSettings,
  onLogout,
}: NavigationRailProps) {
  const { t } = useTranslation('desktop');
  const settingsTooltip = useTooltip(200);
  const toolsTooltip = useTooltip(200);
  const adminTooltip = useTooltip(200);
  const avatarTooltip = useTooltip(200);
  const avatarRef = useRef<HTMLButtonElement>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [avatarErrored, setAvatarErrored] = useState(false);

  return (
    <nav
      role="navigation"
      aria-label="Main navigation"
      className="glass-surface flex flex-col h-full shrink-0"
      style={{
        width: isExpanded ? 280 : 48,
        transition: 'width 200ms cubic-bezier(0.4, 0, 0.2, 1)',
        borderRight: '1px solid var(--border-dim)',
      }}
    >
      {/* Nav icons */}
      <div className="flex flex-col gap-1 p-1 pt-2">
        {navItems.map((item) => (
          <NavButton
            key={item.id}
            item={item}
            isActive={activeApp === item.id}
            isExpanded={isExpanded}
            onClick={() => onNavigate(item.id)}
          />
        ))}
      </div>

      {/* Sidebar content when expanded */}
      {isExpanded && sidebarContent && (
        <div className="flex-1 min-h-0 overflow-y-auto border-t" style={{ borderColor: 'var(--border-dim)' }}>
          {sidebarContent}
        </div>
      )}

      {/* Spacer */}
      {!isExpanded && <div className="flex-1" />}

      {/* Tools button */}
      <div className="relative p-1 pb-0">
        <button
          onClick={onToolsClick}
          onMouseEnter={isExpanded ? undefined : toolsTooltip.onMouseEnter}
          onMouseLeave={toolsTooltip.onMouseLeave}
          className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
          style={{
            padding: '10px 12px',
            color: activeApp === 'tools' ? 'var(--accent)' : 'var(--text-muted)',
            backgroundColor: activeApp === 'tools' ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
            borderLeft: activeApp === 'tools' ? '2px solid var(--accent)' : '2px solid transparent',
          }}
          aria-label={t('navigationRail.tools')}
        >
          <div className="shrink-0 w-5 h-5 flex items-center justify-center">
            {toolsIcon}
          </div>
          {isExpanded && (
            <span
              className="text-xs font-medium tracking-wide whitespace-nowrap"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                color: activeApp === 'tools' ? 'var(--accent)' : 'var(--text-secondary)',
              }}
            >
              {t('navigationRail.tools')}
            </span>
          )}
        </button>

        {/* Tools tooltip — only when collapsed */}
        {toolsTooltip.visible && !isExpanded && (
          <div
            className="absolute left-full top-1/2 -translate-y-1/2 ml-2 whitespace-nowrap px-3 py-1.5 rounded-lg text-[10px] font-medium z-50 pointer-events-none"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'var(--bg-surface)',
              border: '1px solid var(--border-dim)',
              color: 'var(--text-secondary)',
              boxShadow: '0 4px 12px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
            }}
          >
            {t('navigationRail.tools')}
            <div
              className="absolute right-full top-1/2 -translate-y-1/2 w-2 h-2 rotate-45"
              style={{
                marginRight: '-4px',
                backgroundColor: 'var(--bg-surface)',
                borderLeft: '1px solid var(--border-dim)',
                borderBottom: '1px solid var(--border-dim)',
              }}
            />
          </div>
        )}
      </div>

      {/* Admin button — only for admins */}
      {isAdmin && (
        <div className="relative p-1 pb-0">
          <button
            onClick={onAdminClick}
            onMouseEnter={isExpanded ? undefined : adminTooltip.onMouseEnter}
            onMouseLeave={adminTooltip.onMouseLeave}
            className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
            style={{
              padding: '10px 12px',
              color: activeApp === 'admin' ? 'var(--accent)' : 'var(--text-muted)',
              backgroundColor: activeApp === 'admin' ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
              borderLeft: activeApp === 'admin' ? '2px solid var(--accent)' : '2px solid transparent',
            }}
            aria-label={t('navigationRail.admin')}
          >
            <div className="shrink-0 w-5 h-5 flex items-center justify-center">
              {adminIcon}
            </div>
            {isExpanded && (
              <span
                className="text-xs font-medium tracking-wide whitespace-nowrap"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: activeApp === 'admin' ? 'var(--accent)' : 'var(--text-secondary)',
                }}
              >
                {t('navigationRail.admin')}
              </span>
            )}
          </button>

          {/* Admin tooltip — only when collapsed */}
          {adminTooltip.visible && !isExpanded && (
            <div
              className="absolute left-full top-1/2 -translate-y-1/2 ml-2 whitespace-nowrap px-3 py-1.5 rounded-lg text-[10px] font-medium z-50 pointer-events-none"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                backgroundColor: 'var(--bg-surface)',
                border: '1px solid var(--border-dim)',
                color: 'var(--text-secondary)',
                boxShadow: '0 4px 12px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
              }}
            >
              {t('navigationRail.admin')}
              <div
                className="absolute right-full top-1/2 -translate-y-1/2 w-2 h-2 rotate-45"
                style={{
                  marginRight: '-4px',
                  backgroundColor: 'var(--bg-surface)',
                  borderLeft: '1px solid var(--border-dim)',
                  borderBottom: '1px solid var(--border-dim)',
                }}
              />
            </div>
          )}
        </div>
      )}

      {/* Settings button at bottom */}
      <div className="relative p-1 pb-2">
        <button
          onClick={onSettingsClick}
          onMouseEnter={isExpanded ? undefined : settingsTooltip.onMouseEnter}
          onMouseLeave={settingsTooltip.onMouseLeave}
          className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
          style={{
            padding: '10px 12px',
            color: activeApp === 'personality' ? 'var(--accent)' : 'var(--text-muted)',
            backgroundColor: activeApp === 'personality' ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
            borderLeft: activeApp === 'personality' ? '2px solid var(--accent)' : '2px solid transparent',
          }}
          aria-label={t('navigationRail.settings')}
        >
          <div className="shrink-0 w-5 h-5 flex items-center justify-center">
            {settingsIcon}
          </div>
          {isExpanded && (
            <span
              className="text-xs font-medium tracking-wide whitespace-nowrap"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                color: activeApp === 'personality' ? 'var(--accent)' : 'var(--text-secondary)',
              }}
            >
              {t('navigationRail.identity')}
            </span>
          )}
        </button>

        {/* Settings tooltip — only when collapsed */}
        {settingsTooltip.visible && !isExpanded && (
          <div
            className="absolute left-full top-1/2 -translate-y-1/2 ml-2 whitespace-nowrap px-3 py-1.5 rounded-lg text-[10px] font-medium z-50 pointer-events-none"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'var(--bg-surface)',
              border: '1px solid var(--border-dim)',
              color: 'var(--text-secondary)',
              boxShadow: '0 4px 12px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
            }}
          >
            {t('navigationRail.identity')}
            <div
              className="absolute right-full top-1/2 -translate-y-1/2 w-2 h-2 rotate-45"
              style={{
                marginRight: '-4px',
                backgroundColor: 'var(--bg-surface)',
                borderLeft: '1px solid var(--border-dim)',
                borderBottom: '1px solid var(--border-dim)',
              }}
            />
          </div>
        )}
      </div>

      {/* Avatar anchor — opens unified UserMenu */}
      {user && (
        <div
          className="relative p-1 pb-2 pt-2"
          style={{ borderTop: '1px solid var(--border-dim)', marginTop: 2 }}
        >
          <button
            ref={avatarRef}
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            onMouseEnter={isExpanded ? undefined : avatarTooltip.onMouseEnter}
            onMouseLeave={avatarTooltip.onMouseLeave}
            className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
            style={{
              padding: '6px 8px',
              backgroundColor: menuOpen ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
              borderLeft: menuOpen ? '2px solid var(--accent)' : '2px solid transparent',
            }}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-controls="user-menu-popover"
            aria-label={t('userMenu.openLabel', { name: user.name, defaultValue: `${user.name} — abrir menú de cuenta` })}
          >
            <span
              className="shrink-0 flex items-center justify-center overflow-hidden"
              style={{
                width: 32,
                height: 32,
                borderRadius: '50%',
                border: '1px solid var(--border-dim)',
                backgroundColor: 'var(--bg-input)',
              }}
            >
              {user.picture && !avatarErrored ? (
                <img
                  src={user.picture}
                  alt=""
                  width={32}
                  height={32}
                  referrerPolicy="no-referrer"
                  onError={() => setAvatarErrored(true)}
                  style={{ width: 32, height: 32, objectFit: 'cover' }}
                />
              ) : (
                <span
                  aria-hidden="true"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    fontSize: 11,
                    color: 'var(--accent)',
                    letterSpacing: '0.05em',
                  }}
                >
                  {monogram(user.name)}
                </span>
              )}
            </span>
            {isExpanded && (
              <span className="flex flex-col min-w-0 text-left">
                <span
                  className="text-xs font-medium whitespace-nowrap overflow-hidden text-ellipsis"
                  style={{
                    fontFamily: "'DM Sans', system-ui, sans-serif",
                    color: 'var(--text-primary)',
                  }}
                  title={user.name}
                >
                  {user.name}
                </span>
                <span
                  className="text-[10px] whitespace-nowrap overflow-hidden text-ellipsis"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: 'var(--text-muted)',
                  }}
                >
                  {user.email}
                </span>
              </span>
            )}
          </button>

          {/* Avatar tooltip — only when collapsed */}
          {avatarTooltip.visible && !isExpanded && !menuOpen && (
            <div
              className="absolute left-full top-1/2 -translate-y-1/2 ml-2 whitespace-nowrap px-3 py-1.5 rounded-lg text-[10px] font-medium z-50 pointer-events-none"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                backgroundColor: 'var(--bg-surface)',
                border: '1px solid var(--border-dim)',
                color: 'var(--text-secondary)',
                boxShadow: '0 4px 12px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
              }}
            >
              {user.name}
              <div
                className="absolute right-full top-1/2 -translate-y-1/2 w-2 h-2 rotate-45"
                style={{
                  marginRight: '-4px',
                  backgroundColor: 'var(--bg-surface)',
                  borderLeft: '1px solid var(--border-dim)',
                  borderBottom: '1px solid var(--border-dim)',
                }}
              />
            </div>
          )}

          <UserMenu
            open={menuOpen}
            anchorRef={avatarRef}
            user={user}
            isAdmin={!!isAdmin}
            currentWorkspaceName={currentWorkspaceName}
            onClose={() => setMenuOpen(false)}
            onOpenSettings={() => {
              setMenuOpen(false);
              onOpenUserSettings?.();
            }}
            onOpenAdmin={() => {
              setMenuOpen(false);
              onAdminClick?.();
            }}
            onLogout={() => {
              setMenuOpen(false);
              onLogout?.();
            }}
          />
        </div>
      )}
    </nav>
  );
}
