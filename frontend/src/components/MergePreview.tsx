interface FileMerge {
  path: string;
  status: string;
  conflict_count: number;
}

interface Stats {
  total_entities: number;
  unchanged: number;
  ours_modified: number;
  theirs_modified: number;
  both_modified: number;
  added: number;
  deleted: number;
  conflicts: number;
}

interface Props {
  preview: {
    has_conflicts: boolean;
    conflict_count: number;
    stats: Stats;
    files: FileMerge[];
  };
  onMerge?: () => void;
  merging?: boolean;
}

const statusColors: Record<string, string> = {
  clean: '#3fb950',
  conflict: '#f85149',
  added: '#58a6ff',
  deleted: '#f85149',
};

export function MergePreview({ preview, onMerge, merging }: Props) {
  const s = preview.stats;

  return (
    <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
      <div style={{ padding: '16px', borderBottom: '1px solid #30363d', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h3 style={{ fontSize: '16px', color: '#f0f6fc', marginBottom: '8px' }}>
            Structural Merge Preview
          </h3>
          <div style={{ fontSize: '13px', color: '#8b949e', display: 'flex', gap: '16px', flexWrap: 'wrap' }}>
            <span>{s.total_entities} entities</span>
            <span style={{ color: '#3fb950' }}>{s.unchanged} unchanged</span>
            {s.ours_modified > 0 && <span style={{ color: '#d29922' }}>{s.ours_modified} ours modified</span>}
            {s.theirs_modified > 0 && <span style={{ color: '#d29922' }}>{s.theirs_modified} theirs modified</span>}
            {s.added > 0 && <span style={{ color: '#58a6ff' }}>{s.added} added</span>}
            {s.deleted > 0 && <span style={{ color: '#f85149' }}>{s.deleted} deleted</span>}
            {s.conflicts > 0 && <span style={{ color: '#f85149', fontWeight: 'bold' }}>{s.conflicts} conflicts</span>}
          </div>
        </div>
        {onMerge && !preview.has_conflicts && (
          <button onClick={onMerge} disabled={merging}
            style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold' }}>
            {merging ? 'Merging...' : 'Merge pull request'}
          </button>
        )}
        {preview.has_conflicts && (
          <div style={{ color: '#f85149', fontWeight: 'bold' }}>
            {preview.conflict_count} conflict{preview.conflict_count > 1 ? 's' : ''} must be resolved
          </div>
        )}
      </div>
      <div>
        {preview.files.map(f => (
          <div key={f.path} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '8px 16px', borderBottom: '1px solid #21262d', fontSize: '13px', fontFamily: 'monospace' }}>
            <span style={{ color: statusColors[f.status] || '#c9d1d9', width: '70px' }}>
              {f.status}
            </span>
            <span>{f.path}</span>
            {f.conflict_count > 0 && (
              <span style={{ color: '#f85149', marginLeft: 'auto' }}>
                {f.conflict_count} conflict{f.conflict_count > 1 ? 's' : ''}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
