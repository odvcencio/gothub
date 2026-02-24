import type { RepoIndexStatus } from '../api/client';

interface Props {
  status: RepoIndexStatus | null;
  loading?: boolean;
  error?: string;
  refName?: string;
}

export function IndexStatusCard({ status, loading, error, refName }: Props) {
  const resolvedRef = status?.ref || refName || 'this ref';

  if (loading) {
    return (
      <div style={cardStyle}>
        <div style={{ color: '#8b949e', fontSize: '12px' }}>Indexing status: loading...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div style={cardStyle}>
        <div style={{ color: '#d29922', fontSize: '12px' }}>{error}</div>
      </div>
    );
  }

  if (!status) return null;

  const ui = statusPresentation(status.queue_status, resolvedRef);
  const updatedAt = formatUpdatedAt(status.updated_at);

  return (
    <div style={cardStyle}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', flexWrap: 'wrap' }}>
        <strong style={{ color: '#f0f6fc', fontSize: '12px' }}>Indexing</strong>
        <span style={{ color: ui.color, background: ui.background, border: `1px solid ${ui.color}`, borderRadius: '999px', padding: '2px 8px', fontSize: '11px', fontWeight: 'bold', textTransform: 'uppercase' }}>
          {ui.label}
        </span>
      </div>
      <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '6px' }}>
        {ui.message}
      </div>
      <div style={{ color: '#8b949e', fontSize: '11px', marginTop: '6px' }}>
        <span style={{ fontFamily: 'monospace' }}>commit {shortHash(status.commit_hash)}</span>
        {updatedAt && <span> {'\u00b7'} updated {updatedAt}</span>}
      </div>
      {status.queue_status === 'failed' && status.last_error && (
        <div style={{ color: '#f85149', fontSize: '11px', marginTop: '6px', whiteSpace: 'pre-wrap' }}>{status.last_error}</div>
      )}
    </div>
  );
}

const cardStyle = {
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  background: '#0d1117',
} as const;

function statusPresentation(queueStatus: string, ref: string): { label: string; color: string; background: string; message: string } {
  switch (queueStatus) {
    case 'queued':
      return {
        label: 'queued',
        color: '#d29922',
        background: '#2b230f',
        message: `Queued for ${ref}.`,
      };
    case 'in_progress':
      return {
        label: 'in progress',
        color: '#58a6ff',
        background: '#0d2238',
        message: `Indexing in progress for ${ref}.`,
      };
    case 'completed':
      return {
        label: 'completed',
        color: '#3fb950',
        background: '#132a1a',
        message: `Index is ready for ${ref}.`,
      };
    case 'failed':
      return {
        label: 'failed',
        color: '#f85149',
        background: '#2d1216',
        message: `Indexing failed for ${ref}.`,
      };
    case 'not_found':
      return {
        label: 'not found',
        color: '#8b949e',
        background: '#161b22',
        message: `No indexing job found for ${ref}.`,
      };
    default:
      return {
        label: queueStatus || 'unknown',
        color: '#8b949e',
        background: '#161b22',
        message: `Index status for ${ref}: ${queueStatus || 'unknown'}.`,
      };
  }
}

function shortHash(hash: string): string {
  if (!hash) return '';
  return hash.length > 12 ? hash.slice(0, 12) : hash;
}

function formatUpdatedAt(value?: string): string {
  if (!value) return '';
  const ts = Date.parse(value);
  if (!Number.isFinite(ts)) return '';
  return new Date(ts).toLocaleString();
}
