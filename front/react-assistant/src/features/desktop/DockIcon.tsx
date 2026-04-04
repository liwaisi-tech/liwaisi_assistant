import { useState, useRef, useEffect } from 'react';

interface DockIconProps {
  label: string;
  icon: React.ReactNode;
  isActive?: boolean;
  onClick?: () => void;
}

export function DockIcon({ label, icon, isActive = false, onClick }: DockIconProps) {
  const [showTooltip, setShowTooltip] = useState(false);
  const tooltipTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (tooltipTimer.current) clearTimeout(tooltipTimer.current);
    };
  }, []);

  const handleClick = () => {
    if (onClick) {
      onClick();
      return;
    }
    // No onClick handler — show "Coming Soon" tooltip.
    setShowTooltip(true);
    if (tooltipTimer.current) clearTimeout(tooltipTimer.current);
    tooltipTimer.current = setTimeout(() => setShowTooltip(false), 2000);
  };

  return (
    <div className="relative flex flex-col items-center">
      {/* Tooltip */}
      {showTooltip && !isActive && (
        <div
          className="tooltip-enter absolute -top-12 left-1/2 -translate-x-1/2 whitespace-nowrap px-3 py-1.5 rounded-lg text-[10px] font-medium z-20"
          style={{
            backgroundColor: 'var(--bg-surface)',
            border: '1px solid var(--border-dim)',
            color: 'var(--text-secondary)',
            boxShadow: '0 4px 12px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
          }}
        >
          Coming Soon
          <div
            className="absolute left-1/2 -translate-x-1/2 -bottom-1 w-2 h-2 rotate-45"
            style={{ backgroundColor: 'var(--bg-surface)', borderRight: '1px solid var(--border-dim)', borderBottom: '1px solid var(--border-dim)' }}
          />
        </div>
      )}

      <button
        onClick={handleClick}
        className="dock-icon group flex flex-col items-center gap-0.5 px-1.5 py-1 rounded-lg transition-all duration-200 ease-out"
        style={{
          color: isActive ? 'var(--accent)' : 'var(--text-muted)',
        }}
        aria-label={label}
      >
        <div
          className="w-8 h-8 flex items-center justify-center rounded-lg transition-all duration-200"
          style={{
            backgroundColor: isActive ? 'rgba(14, 165, 233, 0.12)' : 'rgba(255,255,255,0.03)',
            border: isActive ? '1px solid rgba(14, 165, 233, 0.3)' : '1px solid transparent',
          }}
        >
          {icon}
        </div>
        <span
          className="text-[8px] font-medium tracking-wide transition-colors duration-200"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: isActive ? 'var(--accent)' : 'var(--text-muted)',
          }}
        >
          {label}
        </span>
      </button>

      {/* Active indicator dot */}
      {isActive && (
        <div
          className="absolute -bottom-0.5 w-1 h-1 rounded-full"
          style={{ backgroundColor: 'var(--accent)', boxShadow: '0 0 4px var(--accent)' }}
        />
      )}
    </div>
  );
}
