import { useState, useEffect } from 'preact/hooks';
import { listTree, getBlob } from '../api/client';
import { FileTree } from '../components/FileTree';
import { CodeViewer } from '../components/CodeViewer';

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
}

export function CodeView({ owner, repo, ref: gitRef, path }: Props) {
  const [entries, setEntries] = useState<any[] | null>(null);
  const [blob, setBlob] = useState<any>(null);
  const [isDir, setIsDir] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo || !gitRef) return;

    // Determine if path is a blob or tree based on the URL
    const isBlobUrl = location.pathname.includes('/blob/');

    if (isBlobUrl && path) {
      setIsDir(false);
      getBlob(owner, repo, gitRef, path)
        .then((b) => {
          setBlob({
            ...b,
            content: decodeBlobContent(b?.data),
          });
        })
        .catch(e => setError(e.message));
    } else {
      setIsDir(true);
      listTree(owner, repo, gitRef, path)
        .then(setEntries)
        .catch(e => setError(e.message));
    }
  }, [owner, repo, gitRef, path]);

  if (!owner || !repo || !gitRef) {
    return <div style={{ color: '#f85149', padding: '20px' }}>Missing repository context</div>;
  }

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;

  const breadcrumbs = buildBreadcrumbs(owner, repo, gitRef, path);

  return (
    <div>
      <div style={{ marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
        <a href={`/${owner}/${repo}`} style={{ color: '#58a6ff', fontWeight: 'bold' }}>{repo}</a>
        {breadcrumbs.map((bc, i) => (
          <span key={i}>
            <span style={{ color: '#8b949e' }}>/</span>
            {bc.href ? (
              <a href={bc.href} style={{ color: '#58a6ff' }}>{bc.name}</a>
            ) : (
              <span style={{ color: '#f0f6fc' }}>{bc.name}</span>
            )}
          </span>
        ))}
      </div>

      {isDir ? (
        entries ? (
          <FileTree entries={entries} owner={owner} repo={repo} ref={gitRef} currentPath={path || ''} />
        ) : (
          <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>
        )
      ) : (
        blob ? (
          <CodeViewer filename={path || ''} source={blob.content || ''} />
        ) : (
          <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>
        )
      )}
    </div>
  );
}

interface Breadcrumb {
  name: string;
  href?: string;
}

function buildBreadcrumbs(owner: string, repo: string, ref: string, path?: string): Breadcrumb[] {
  if (!path) return [];
  const parts = path.split('/');
  return parts.map((part, i) => {
    if (i === parts.length - 1) return { name: part };
    const subpath = parts.slice(0, i + 1).join('/');
    return { name: part, href: `/${owner}/${repo}/tree/${ref}/${subpath}` };
  });
}

function decodeBlobContent(data: unknown): string {
  if (typeof data !== 'string' || data === '') return '';

  try {
    const binary = atob(data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new TextDecoder().decode(bytes);
  } catch {
    return '';
  }
}
