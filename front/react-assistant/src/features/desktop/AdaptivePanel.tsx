import { useState, useRef, useCallback, useEffect } from 'react';

interface AdaptivePanelProps {
  isOpen: boolean;
  title: string;
  onClose: () => void;
  onPopOut: () => void;
  children: React.ReactNode;
}

const MIN_WIDTH = 400;
const MAX_WIDTH_PERCENT = 0.7;
const DEFAULT_WIDTH_PERCENT = 0.5;
const COLLAPSE_THRESHOLD = MIN_WIDTH;

export function AdaptivePanel({ isOpen, title, onClose, onPopOut, children }: AdaptivePanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState<number | null>(null);
  const isDragging = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(0);

  // Compute actual width from state or default
  const getEffectiveWidth = useCallback(() => {
    if (width !== null) return width;
    const parent = panelRef.current?.parentElement;
    if (!parent) return MIN_WIDTH;
    const parentWidth = parent.clientWidth;
    return Math.max(MIN_WIDTH, Math.min(parentWidth * DEFAULT_WIDTH_PERCENT, parentWidth * MAX_WIDTH_PERCENT));
  }, [width]);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDragging.current = true;
    startX.current = e.clientX;
    startWidth.current = getEffectiveWidth();
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }, [getEffectiveWidth]);

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isDragging.current) return;
      const parent = panelRef.current?.parentElement;
      if (!parent) return;

      const maxWidth = parent.clientWidth * MAX_WIDTH_PERCENT;
      const delta = startX.current - e.clientX; // moving left = wider
      const newWidth = Math.min(startWidth.current + delta, maxWidth);

      if (newWidth < COLLAPSE_THRESHOLD) {
        isDragging.current = false;
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        setWidth(null);
        onClose();
        return;
      }

      setWidth(Math.max(MIN_WIDTH, newWidth));
    };

    const handleMouseUp = () => {
      if (!isDragging.current) return;
      isDragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };
  }, [onClose]);

  // Reset width when panel reopens
  useEffect(() => {
    if (isOpen) setWidth(null);
  }, [isOpen]);

  const effectiveWidth = getEffectiveWidth();

  return (
    <div
      ref={panelRef}
      role="complementary"
      aria-label={`${title} panel`}
      className="absolute top-0 right-0 h-full flex flex-col"
      style={{
        width: effectiveWidth,
        minWidth: MIN_WIDTH,
        maxWidth: '70%',
        background: 'var(--bg-surface)',
        borderLeft: '1px solid var(--border-dim)',
        transform: isOpen ? 'translateX(0)' : 'translateX(100%)',
        transition: isDragging.current ? 'none' : 'transform 250ms cubic-bezier(0.4, 0, 0.2, 1)',
        visibility: isOpen ? 'visible' : 'hidden',
        zIndex: 20,
      }}
      onTransitionEnd={(e) => {
        // Keep visibility in sync after transition completes
        if (e.propertyName === 'transform' && !isOpen && panelRef.current) {
          panelRef.current.style.visibility = 'hidden';
        }
      }}
    >
      {/* Resize handle */}
      <div
        className="absolute top-0 left-0 h-full group"
        style={{ width: 6, cursor: 'col-resize', zIndex: 30 }}
        onMouseDown={handleMouseDown}
      >
        <div
          className="h-full transition-colors duration-150"
          style={{ width: 2, marginLeft: 2, backgroundColor: 'var(--border-dim)' }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--accent)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--border-dim)'; }}
        />
      </div>

      {/* Header */}
      <div
        className="flex items-center justify-between shrink-0 px-3"
        style={{
          height: 40,
          borderBottom: '1px solid var(--border-dim)',
        }}
      >
        <span
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: 12,
            textTransform: 'uppercase',
            letterSpacing: '0.05em',
            color: 'var(--text-secondary)',
          }}
        >
          {title}
        </span>

        <div className="flex items-center gap-1">
          {/* Pop-out button */}
          <button
            onClick={onPopOut}
            className="p-1.5 rounded transition-colors duration-150"
            aria-label={`Pop out ${title}`}
            style={{ color: 'var(--text-muted)' }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--text-secondary)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--text-muted)'; }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="15 3 21 3 21 9" />
              <polyline points="9 21 3 21 3 15" />
              <line x1="21" y1="3" x2="14" y2="10" />
              <line x1="3" y1="21" x2="10" y2="14" />
            </svg>
          </button>

          {/* Close button */}
          <button
            onClick={onClose}
            className="p-1.5 rounded transition-colors duration-150"
            aria-label={`Close ${title}`}
            style={{ color: 'var(--text-muted)' }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--text-secondary)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--text-muted)'; }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto">
        {children}
      </div>
    </div>
  );
}
