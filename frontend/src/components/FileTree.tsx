interface TreeEntry {
  name: string;
  is_dir: boolean;
  blob_hash?: string;
}

interface Props {
  entries: TreeEntry[];
  owner: string;
  repo: string;
  ref: string;
  currentPath: string;
}

export function FileTree({ entries, owner, repo, ref, currentPath }: Props) {
  const basePath = currentPath ? `/${owner}/${repo}/tree/${ref}/${currentPath}` : `/${owner}/${repo}/tree/${ref}`;

  return (
    <div style={{ border: '1px solid #30363d', borderRadius: '6px', overflow: 'hidden' }}>
      {currentPath && (
        <a href={parentPath(basePath)} style={{ display: 'block', padding: '8px 16px', borderBottom: '1px solid #30363d', background: '#161b22' }}>
          ..
        </a>
      )}
      {entries.sort((a, b) => {
        if (a.is_dir && !b.is_dir) return -1;
        if (!a.is_dir && b.is_dir) return 1;
        return a.name.localeCompare(b.name);
      }).map(entry => {
        const href = entry.is_dir
          ? `/${owner}/${repo}/tree/${ref}/${currentPath ? currentPath + '/' : ''}${entry.name}`
          : `/${owner}/${repo}/blob/${ref}/${currentPath ? currentPath + '/' : ''}${entry.name}`;

        return (
          <a key={entry.name} href={href}
            style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '8px 16px', borderBottom: '1px solid #21262d', color: '#c9d1d9' }}>
            <span style={{ color: entry.is_dir ? '#58a6ff' : '#8b949e', fontFamily: 'monospace', fontSize: '14px' }}>
              {entry.is_dir ? '\u{1F4C1}' : '\u{1F4C4}'}
            </span>
            <span>{entry.name}</span>
          </a>
        );
      })}
    </div>
  );
}

function parentPath(path: string): string {
  const parts = path.split('/');
  parts.pop();
  return parts.join('/') || '/';
}
