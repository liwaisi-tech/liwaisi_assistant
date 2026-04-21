import { useState, useCallback } from 'react';
import type { ToolExecution } from '../../types/chat';

// ── Glyph map ──────────────────────────────────────────────────────────────

const TOOL_GLYPHS: Record<string, string> = {
  bash_exec:  '❯',
  file_read:  '◎',
  file_write: '◈',
};

const TOOL_LABELS: Record<string, string> = {
  bash_exec:  'shell',
  file_read:  'read',
  file_write: 'write',
};

// ── Helpers ────────────────────────────────────────────────────────────────

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function extractBashCommand(args?: Record<string, unknown>): string | null {
  if (!args) return null;
  const cmd = args.command;
  if (typeof cmd !== 'string' || !cmd.trim()) return null;
  const argList = Array.isArray(args.args) ? (args.args as string[]) : [];
  return argList.length > 0 ? `${cmd} ${argList.join(' ')}` : cmd;
}

function extractFilePath(args?: Record<string, unknown>): string | null {
  if (!args) return null;
  const path = args.path;
  return typeof path === 'string' && path.trim() ? path : null;
}

// ── Single badge ──────────────────────────────────────────────────────────

interface ToolBadgeProps {
  execution: ToolExecution;
}

function ToolBadge({ execution }: ToolBadgeProps) {
  // Default: collapsed for success, expanded for failure (REQ-022).
  const [expanded, setExpanded] = useState(!execution.success);

  const toggle = useCallback(() => setExpanded((v) => !v), []);

  const glyph = TOOL_GLYPHS[execution.toolName] ?? '◦';
  const label = TOOL_LABELS[execution.toolName] ?? execution.toolName;
  const dur   = formatDuration(execution.durationMs);

  // Success = dim green, failure = amber
  const successColor = execution.success ? '#4ade80' : '#fbbf24';
  const statusGlyph  = execution.success ? '✓' : '✗';

  // Detail line: command for bash, path for file ops
  const bashCmd  = execution.toolName === 'bash_exec'  ? extractBashCommand(execution.arguments)  : null;
  const filePath = (execution.toolName === 'file_read' || execution.toolName === 'file_write')
    ? extractFilePath(execution.arguments)
    : null;

  const hasDetail = Boolean(bashCmd || filePath || execution.error);

  return (
    <div
      style={{
        borderRadius: '6px',
        border: `1px solid ${execution.success ? 'rgba(74, 222, 128, 0.12)' : 'rgba(251, 191, 36, 0.2)'}`,
        backgroundColor: execution.success
          ? 'rgba(74, 222, 128, 0.04)'
          : 'rgba(251, 191, 36, 0.06)',
        overflow: 'hidden',
        transition: 'border-color 120ms ease',
      }}
    >
      {/* Header pill */}
      <button
        type="button"
        onClick={hasDetail ? toggle : undefined}
        aria-expanded={hasDetail ? expanded : undefined}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          width: '100%',
          padding: '3px 8px',
          background: 'none',
          border: 'none',
          cursor: hasDetail ? 'pointer' : 'default',
          textAlign: 'left',
        }}
      >
        {/* Glyph */}
        <span
          aria-hidden="true"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: '11px',
            color: 'var(--accent)',
            opacity: 0.8,
            minWidth: '10px',
          }}
        >
          {glyph}
        </span>

        {/* Tool label */}
        <span
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: '10px',
            fontWeight: 600,
            textTransform: 'uppercase',
            letterSpacing: '0.08em',
            color: 'var(--text-secondary)',
          }}
        >
          {label}
        </span>

        {/* Duration */}
        <span
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: '10px',
            color: 'var(--text-muted)',
          }}
        >
          {dur}
        </span>

        {/* Spacer */}
        <span style={{ flex: 1 }} />

        {/* Status */}
        <span
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: '10px',
            fontWeight: 700,
            color: successColor,
          }}
        >
          {statusGlyph}
        </span>

        {/* Expand chevron */}
        {hasDetail && (
          <span
            aria-hidden="true"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              fontSize: '9px',
              color: 'var(--text-muted)',
              transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 150ms ease',
              display: 'inline-block',
              marginLeft: '2px',
            }}
          >
            ›
          </span>
        )}
      </button>

      {/* Expandable body */}
      {hasDetail && expanded && (
        <div
          style={{
            borderTop: `1px solid ${execution.success ? 'rgba(74, 222, 128, 0.08)' : 'rgba(251, 191, 36, 0.1)'}`,
            padding: '6px 8px 8px',
            display: 'flex',
            flexDirection: 'column',
            gap: '4px',
          }}
        >
          {/* bash_exec command block (REQ-021) */}
          {bashCmd && (
            <pre
              style={{
                margin: 0,
                padding: '6px 10px',
                borderRadius: '4px',
                backgroundColor: 'var(--bg-deep)',
                border: '1px solid var(--border-dim)',
                fontFamily: "'JetBrains Mono', monospace",
                fontSize: '11px',
                lineHeight: 1.55,
                color: 'var(--text-primary)',
                overflowX: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}
            >
              <span style={{ color: 'var(--accent)', opacity: 0.7, userSelect: 'none' }}>$ </span>
              {bashCmd}
            </pre>
          )}

          {/* file path */}
          {filePath && (
            <span
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                fontSize: '11px',
                color: 'var(--text-secondary)',
                paddingLeft: '2px',
              }}
            >
              {filePath}
            </span>
          )}

          {/* error message */}
          {execution.error && (
            <span
              style={{
                fontFamily: "'DM Sans', system-ui, sans-serif",
                fontSize: '11px',
                color: '#fbbf24',
                paddingLeft: '2px',
              }}
            >
              {execution.error}
            </span>
          )}
        </div>
      )}
    </div>
  );
}

// ── List ───────────────────────────────────────────────────────────────────

interface ToolExecutionListProps {
  executions: ToolExecution[];
}

export function ToolExecutionList({ executions }: ToolExecutionListProps) {
  if (executions.length === 0) return null;

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '3px',
        marginBottom: '8px',
      }}
    >
      {executions.map((exec) => (
        <ToolBadge key={exec.id} execution={exec} />
      ))}
    </div>
  );
}
