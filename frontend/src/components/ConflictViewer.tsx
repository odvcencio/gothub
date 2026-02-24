import { useState } from 'preact/hooks';

interface Entity {
  key: string;
  name: string;
  status: string;
  ours_content?: string;
  theirs_content?: string;
  base_content?: string;
}

interface FileEntry {
  path: string;
  status: string;
  conflict_count: number;
  entities?: Entity[];
}

interface ConflictViewerProps {
  files: any[];
}

const colors = {
  bg: '#0d1117',
  text: '#c9d1d9',
  heading: '#f0f6fc',
  muted: '#8b949e',
  link: '#58a6ff',
  green: '#238636',
  red: '#f85149',
  border: '#30363d',
  surface: '#161b22',
};

export function ConflictViewer({ files }: ConflictViewerProps) {
  const conflictFiles: FileEntry[] = (files || []).filter(
    (f: FileEntry) => f.conflict_count > 0 || f.status === 'conflict'
  );

  const totalConflicts = conflictFiles.reduce(
    (sum: number, f: FileEntry) => sum + (f.conflict_count || 0),
    0
  );

  if (conflictFiles.length === 0) {
    return (
      <div
        style={{
          border: `1px solid ${colors.border}`,
          borderRadius: '6px',
          padding: '16px',
          color: colors.muted,
          fontSize: '13px',
        }}
      >
        No conflicts
      </div>
    );
  }

  return (
    <div style={{ border: `1px solid ${colors.border}`, borderRadius: '6px' }}>
      <div
        style={{
          padding: '12px 16px',
          borderBottom: `1px solid ${colors.border}`,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}
      >
        <strong style={{ color: colors.heading, fontSize: '14px' }}>
          Conflict Details
        </strong>
        <span
          style={{
            color: colors.red,
            fontSize: '13px',
            fontWeight: 'bold',
          }}
        >
          {totalConflicts} conflict{totalConflicts !== 1 ? 's' : ''} in{' '}
          {conflictFiles.length} file{conflictFiles.length !== 1 ? 's' : ''}
        </span>
      </div>

      {conflictFiles.map((file) => (
        <ConflictFile key={file.path} file={file} />
      ))}
    </div>
  );
}

function ConflictFile({ file }: { file: FileEntry }) {
  const [expanded, setExpanded] = useState(true);

  const conflictEntities = (file.entities || []).filter(
    (e) => e.status === 'conflict' || e.status === 'both_modified'
  );

  return (
    <div style={{ borderBottom: `1px solid ${colors.border}` }}>
      <div
        onClick={() => setExpanded(!expanded)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          padding: '10px 16px',
          cursor: 'pointer',
          background: colors.surface,
          userSelect: 'none',
        }}
      >
        <span
          style={{
            color: colors.muted,
            fontSize: '12px',
            fontFamily: 'monospace',
            width: '16px',
            textAlign: 'center',
          }}
        >
          {expanded ? '\u25BC' : '\u25B6'}
        </span>
        <span
          style={{
            color: colors.heading,
            fontSize: '13px',
            fontFamily: 'monospace',
            flex: 1,
          }}
        >
          {file.path}
        </span>
        <span
          style={{
            color: colors.red,
            fontSize: '12px',
          }}
        >
          {file.conflict_count} conflict{file.conflict_count !== 1 ? 's' : ''}
        </span>
      </div>

      {expanded && (
        <div style={{ padding: '0 16px 12px 16px' }}>
          {conflictEntities.length === 0 ? (
            <div
              style={{
                color: colors.muted,
                fontSize: '12px',
                padding: '8px 0',
              }}
            >
              No entity-level conflict details available
            </div>
          ) : (
            conflictEntities.map((entity) => (
              <ConflictEntity key={entity.key} entity={entity} />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ConflictEntity({ entity }: { entity: Entity }) {
  const hasBase = !!entity.base_content;

  return (
    <div
      style={{
        border: `1px solid ${colors.border}`,
        borderRadius: '6px',
        marginTop: '10px',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          padding: '8px 12px',
          background: colors.surface,
          borderBottom: `1px solid ${colors.border}`,
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
        }}
      >
        <span
          style={{
            color: colors.red,
            fontSize: '12px',
            fontWeight: 'bold',
          }}
        >
          CONFLICT
        </span>
        <span
          style={{
            color: colors.heading,
            fontSize: '13px',
            fontWeight: 'bold',
          }}
        >
          {entity.name}
        </span>
        <span
          style={{
            color: colors.muted,
            fontSize: '12px',
            fontFamily: 'monospace',
          }}
        >
          {entity.key}
        </span>
      </div>

      {hasBase && (
        <div>
          <div
            style={{
              padding: '6px 12px',
              fontSize: '12px',
              fontWeight: 'bold',
              color: colors.muted,
              background: 'rgba(139, 148, 158, 0.08)',
              borderBottom: `1px solid ${colors.border}`,
            }}
          >
            BASE
          </div>
          <pre
            style={{
              margin: 0,
              padding: '10px 12px',
              fontSize: '12px',
              fontFamily: 'monospace',
              color: colors.text,
              background: 'rgba(139, 148, 158, 0.04)',
              borderBottom: `1px solid ${colors.border}`,
              overflowX: 'auto',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }}
          >
            {entity.base_content}
          </pre>
        </div>
      )}

      <div style={{ display: 'flex' }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              padding: '6px 12px',
              fontSize: '12px',
              fontWeight: 'bold',
              color: '#3fb950',
              background: 'rgba(35, 134, 54, 0.1)',
              borderBottom: `1px solid ${colors.border}`,
              borderRight: `1px solid ${colors.border}`,
            }}
          >
            OURS
          </div>
          <pre
            style={{
              margin: 0,
              padding: '10px 12px',
              fontSize: '12px',
              fontFamily: 'monospace',
              color: colors.text,
              background: 'rgba(35, 134, 54, 0.06)',
              borderRight: `1px solid ${colors.border}`,
              overflowX: 'auto',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              minHeight: '40px',
            }}
          >
            {entity.ours_content || '(empty)'}
          </pre>
        </div>

        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              padding: '6px 12px',
              fontSize: '12px',
              fontWeight: 'bold',
              color: colors.link,
              background: 'rgba(88, 166, 255, 0.1)',
              borderBottom: `1px solid ${colors.border}`,
            }}
          >
            THEIRS
          </div>
          <pre
            style={{
              margin: 0,
              padding: '10px 12px',
              fontSize: '12px',
              fontFamily: 'monospace',
              color: colors.text,
              background: 'rgba(88, 166, 255, 0.06)',
              overflowX: 'auto',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              minHeight: '40px',
            }}
          >
            {entity.theirs_content || '(empty)'}
          </pre>
        </div>
      </div>
    </div>
  );
}
