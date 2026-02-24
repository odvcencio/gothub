interface Change {
  type: string;
  key: string;
  before?: { name?: string; decl_kind?: string };
  after?: { name?: string; decl_kind?: string };
}

interface FileDiff {
  path: string;
  changes: Change[];
}

interface Props {
  files: FileDiff[];
}

const changeColors: Record<string, string> = {
  added: '#3fb950',
  removed: '#f85149',
  modified: '#d29922',
};

export function EntityDiff({ files }: Props) {
  if (!files || files.length === 0) {
    return <div style={{ color: '#8b949e', padding: '20px' }}>No changes</div>;
  }

  return (
    <div>
      {files.map(file => (
        <div key={file.path} style={{ border: '1px solid #30363d', borderRadius: '6px', marginBottom: '16px' }}>
          <div style={{ background: '#161b22', padding: '10px 16px', borderBottom: '1px solid #30363d', fontFamily: 'monospace', fontSize: '13px' }}>
            {file.path}
          </div>
          <div style={{ padding: '8px 16px' }}>
            {file.changes.map(change => (
              <div key={change.key} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '4px 0', fontSize: '13px', fontFamily: 'monospace' }}>
                <span style={{ color: changeColors[change.type] || '#c9d1d9', fontWeight: 'bold', width: '70px' }}>
                  {change.type}
                </span>
                <span style={{ color: '#c9d1d9' }}>{change.key}</span>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
