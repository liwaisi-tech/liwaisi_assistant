import { useState, useRef, useEffect, useCallback } from 'react';

type ActiveApp = 'chat' | 'flows' | 'monitor' | 'personality' | 'tools';

interface NavigationRailProps {
  activeApp: ActiveApp;
  isExpanded: boolean;
  onNavigate: (app: ActiveApp) => void;
  onSettingsClick: () => void;
  onToolsClick: () => void;
  sidebarContent?: React.ReactNode;
}

interface NavItem {
  id: ActiveApp;
  label: string;
  icon: React.ReactNode;
}

const navItems: NavItem[] = [
  {
    id: 'chat',
    label: 'Chat',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
      </svg>
    ),
  },
  {
    id: 'flows',
    label: 'Flows',
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
    label: 'Monitor',
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
  const [showTooltip, setShowTooltip] = useState(false);
  const tooltipTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (tooltipTimer.current) clearTimeout(tooltipTimer.current);
    };
  }, []);

  const handleMouseEnter = useCallback(() => {
    if (isExpanded) return;
    tooltipTimer.current = setTimeout(() => setShowTooltip(true), 200);
  }, [isExpanded]);

  const handleMouseLeave = useCallback(() => {
    if (tooltipTimer.current) clearTimeout(tooltipTimer.current);
    setShowTooltip(false);
  }, []);

  return (
    <div className="relative">
      <button
        onClick={onClick}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
        style={{
          padding: '10px 12px',
          color: isActive ? 'var(--accent)' : 'var(--text-muted)',
          backgroundColor: isActive ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
          borderLeft: isActive ? '2px solid var(--accent)' : '2px solid transparent',
        }}
        aria-label={item.label}
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
            {item.label}
          </span>
        )}
      </button>

      {/* Tooltip — only when collapsed */}
      {showTooltip && !isExpanded && (
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
          {item.label}
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

export function NavigationRail({
  activeApp,
  isExpanded,
  onNavigate,
  onSettingsClick,
  onToolsClick,
  sidebarContent,
}: NavigationRailProps) {
  const [settingsTooltip, setSettingsTooltip] = useState(false);
  const settingsTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [toolsTooltip, setToolsTooltip] = useState(false);
  const toolsTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (settingsTimer.current) clearTimeout(settingsTimer.current);
      if (toolsTimer.current) clearTimeout(toolsTimer.current);
    };
  }, []);

  const handleSettingsMouseEnter = useCallback(() => {
    if (isExpanded) return;
    settingsTimer.current = setTimeout(() => setSettingsTooltip(true), 200);
  }, [isExpanded]);

  const handleSettingsMouseLeave = useCallback(() => {
    if (settingsTimer.current) clearTimeout(settingsTimer.current);
    setSettingsTooltip(false);
  }, []);

  const handleToolsMouseEnter = useCallback(() => {
    if (isExpanded) return;
    toolsTimer.current = setTimeout(() => setToolsTooltip(true), 200);
  }, [isExpanded]);

  const handleToolsMouseLeave = useCallback(() => {
    if (toolsTimer.current) clearTimeout(toolsTimer.current);
    setToolsTooltip(false);
  }, []);

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
          onMouseEnter={handleToolsMouseEnter}
          onMouseLeave={handleToolsMouseLeave}
          className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
          style={{
            padding: '10px 12px',
            color: activeApp === 'tools' ? 'var(--accent)' : 'var(--text-muted)',
            backgroundColor: activeApp === 'tools' ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
            borderLeft: activeApp === 'tools' ? '2px solid var(--accent)' : '2px solid transparent',
          }}
          aria-label="Tools"
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
              Tools
            </span>
          )}
        </button>

        {/* Tools tooltip — only when collapsed */}
        {toolsTooltip && !isExpanded && (
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
            Tools
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

      {/* Settings button at bottom */}
      <div className="relative p-1 pb-2">
        <button
          onClick={onSettingsClick}
          onMouseEnter={handleSettingsMouseEnter}
          onMouseLeave={handleSettingsMouseLeave}
          className="w-full flex items-center gap-3 rounded-lg transition-colors duration-200"
          style={{
            padding: '10px 12px',
            color: activeApp === 'personality' ? 'var(--accent)' : 'var(--text-muted)',
            backgroundColor: activeApp === 'personality' ? 'rgba(14, 165, 233, 0.12)' : 'transparent',
            borderLeft: activeApp === 'personality' ? '2px solid var(--accent)' : '2px solid transparent',
          }}
          aria-label="Settings"
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
              Identity
            </span>
          )}
        </button>

        {/* Settings tooltip — only when collapsed */}
        {settingsTooltip && !isExpanded && (
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
            Identity
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
    </nav>
  );
}
