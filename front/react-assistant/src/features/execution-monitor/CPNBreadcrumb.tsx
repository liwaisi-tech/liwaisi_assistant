import type { CPNHierarchyNode } from '../../hooks/useExecutionMonitor';

interface CPNBreadcrumbProps {
  hierarchy: CPNHierarchyNode[];
  activeCPNId: string | null;
  rootLabel?: string;
  onNavigate: (cpnId: string) => void;
}

export function CPNBreadcrumb({ hierarchy, activeCPNId, rootLabel = 'Root CPN', onNavigate }: CPNBreadcrumbProps) {
  // Build breadcrumb path from root to active CPN
  const path: { id: string; label: string }[] = [];

  if (hierarchy.length === 0 || !activeCPNId) {
    return (
      <div className="flex items-center gap-1" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
        <span className="text-[10px] font-semibold" style={{ color: 'var(--accent)' }}>
          {rootLabel}
        </span>
      </div>
    );
  }

  // Add root
  path.push({ id: 'root', label: rootLabel });

  // Add hierarchy nodes leading to activeCPNId
  for (const node of hierarchy) {
    if (node.cpnId === activeCPNId || path.length > 1) {
      path.push({ id: node.cpnId, label: node.cpnRole || node.cpnId.slice(0, 8) });
    }
  }

  return (
    <div className="flex items-center gap-1" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
      {path.map((crumb, i) => (
        <span key={crumb.id} className="flex items-center gap-1">
          {i > 0 && (
            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
              {'\u203A'}
            </span>
          )}
          <button
            onClick={() => onNavigate(crumb.id)}
            className="text-[10px] font-semibold transition-colors hover:underline"
            style={{
              color: crumb.id === activeCPNId || (i === path.length - 1) ? 'var(--accent)' : 'var(--text-secondary)',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              padding: 0,
            }}
          >
            {crumb.label}
          </button>
        </span>
      ))}
    </div>
  );
}
