import { useTranslation } from 'react-i18next';

type ActiveApp = 'chat' | 'flows' | 'monitor' | 'personality' | 'tools';

interface MobileTabBarProps {
  activeApp: ActiveApp;
  onNavigate: (app: ActiveApp) => void;
}

interface TabConfig {
  id: ActiveApp;
  labelKey: string;
  icon: React.ReactNode;
}

const tabs: TabConfig[] = [
  {
    id: 'chat',
    labelKey: 'mobileTabBar.chat',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
        <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
      </svg>
    ),
  },
  {
    id: 'flows',
    labelKey: 'mobileTabBar.flows',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="5" r="2" />
        <circle cx="6" cy="19" r="2" />
        <circle cx="18" cy="19" r="2" />
        <path d="M12 7v4M12 11l-6 6M12 11l6 6" />
      </svg>
    ),
  },
  {
    id: 'monitor',
    labelKey: 'mobileTabBar.monitor',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
        <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
      </svg>
    ),
  },
];

export function MobileTabBar({ activeApp, onNavigate }: MobileTabBarProps) {
  const { t } = useTranslation('desktop');

  return (
    <nav
      className="fixed bottom-0 left-0 right-0 glass-surface flex items-center justify-around z-10"
      style={{
        height: 56,
        borderTop: '1px solid var(--border-dim)',
      }}
      role="tablist"
      aria-label="Mobile navigation"
    >
      {tabs.map((tab) => {
        const isActive = activeApp === tab.id;
        return (
          <button
            key={tab.id}
            role="tab"
            aria-selected={isActive}
            aria-label={t(tab.labelKey)}
            onClick={() => onNavigate(tab.id)}
            className="flex flex-col items-center justify-center flex-1 h-full relative"
            style={{
              color: isActive ? 'var(--accent)' : 'var(--text-muted)',
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
            }}
          >
            {tab.icon}
            <span
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                fontSize: 10,
                marginTop: 2,
              }}
            >
              {t(tab.labelKey)}
            </span>
            {isActive && (
              <span
                className="absolute rounded-full"
                style={{
                  bottom: 6,
                  width: 4,
                  height: 4,
                  backgroundColor: 'var(--accent)',
                }}
              />
            )}
          </button>
        );
      })}
    </nav>
  );
}
