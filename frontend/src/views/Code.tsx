import { useState, useEffect } from 'preact/hooks';
import {
  getBlob,
  getEntityBlame,
  getRepoIndexStatus,
  listEntities,
  listTree,
  type EntityBlameInfo,
  type FileEntity,
  type RepoIndexStatus,
  type TreeEntry,
} from '../api/client';
import { FileTree } from '../components/FileTree';
import { CodeViewer } from '../components/CodeViewer';
import { MarkdownViewer } from '../components/MarkdownViewer';
import { IndexStatusCard } from '../components/IndexStatusCard';

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
}

export function CodeView({ owner, repo, ref: gitRef, path }: Props) {
  const [entries, setEntries] = useState<TreeEntry[] | null>(null);
  const [blob, setBlob] = useState<{ content: string } | null>(null);
  const [isDir, setIsDir] = useState(true);
  const [entities, setEntities] = useState<FileEntity[] | null>(null);
  const [selectedEntity, setSelectedEntity] = useState<FileEntity | null>(null);
  const [blame, setBlame] = useState<EntityBlameInfo | null>(null);
  const [blameLoading, setBlameLoading] = useState(false);
  const [indexStatus, setIndexStatus] = useState<RepoIndexStatus | null>(null);
  const [indexStatusLoading, setIndexStatusLoading] = useState(false);
  const [indexStatusError, setIndexStatusError] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const appendNotice = (message: string) => {
    const next = message.trim();
    if (!next) return;
    setNotice((current) => {
      if (!current) return next;
      if (current.includes(next)) return current;
      return `${current} • ${next}`;
    });
  };

  useEffect(() => {
    if (!owner || !repo || !gitRef) return;
    setError('');
    setNotice('');
    setEntries(null);
    setBlob(null);
    setEntities(null);
    setSelectedEntity(null);
    setBlame(null);
    setBlameLoading(false);

    // Determine if path is a blob or tree based on the URL
    const isBlobUrl = location.pathname.includes('/blob/');

    if (isBlobUrl && path) {
      setIsDir(false);
      getBlob(owner, repo, gitRef, path)
        .then((b) => {
          const decoded = decodeBlobContent(b?.data);
          setBlob({
            content: decoded.content,
          });
          if (decoded.error) {
            appendNotice(decoded.error);
          }
        })
        .catch(e => setError(e.message));
      listEntities(owner, repo, gitRef, path)
        .then((resp) => {
          const list = resp.entities || [];
          setEntities(list);
          setSelectedEntity(list.length > 0 ? list[0] : null);
        })
        .catch((e: any) => {
          setEntities([]);
          setSelectedEntity(null);
          appendNotice(e?.message || 'failed to load structural entities');
        });
    } else {
      setIsDir(true);
      listTree(owner, repo, gitRef, path)
        .then(setEntries)
        .catch(e => setError(e.message));
    }
  }, [owner, repo, gitRef, path]);

  useEffect(() => {
    if (!owner || !repo || !gitRef || !path || !selectedEntity) return;

    setBlameLoading(true);
    setBlame(null);
    getEntityBlame(owner, repo, gitRef, selectedEntity.key, path, 500)
      .then(setBlame)
      .catch((e: any) => {
        setBlame(null);
        appendNotice(e?.message || 'failed to load structural blame');
      })
      .finally(() => setBlameLoading(false));
  }, [owner, repo, gitRef, path, selectedEntity?.key]);

  useEffect(() => {
    if (!owner || !repo || !gitRef) return;

    let cancelled = false;
    setIndexStatus(null);
    setIndexStatusError('');
    setIndexStatusLoading(true);

    getRepoIndexStatus(owner, repo, gitRef)
      .then((status) => {
        if (!cancelled) setIndexStatus(status);
      })
      .catch((e: any) => {
        if (!cancelled) setIndexStatusError(e?.message || 'failed to load indexing status');
      })
      .finally(() => {
        if (!cancelled) setIndexStatusLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [owner, repo, gitRef]);

  if (!owner || !repo || !gitRef) {
    return <div style={{ color: '#f85149', padding: '20px' }}>Missing repository context</div>;
  }

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;

  const breadcrumbs = buildBreadcrumbs(owner, repo, gitRef, path);
  const showIndexStatus = indexStatusLoading || !!indexStatusError || !!indexStatus;
  const isMarkdown = isMarkdownPath(path || '');

  return (
    <div>
      {notice && (
        <div style={{ color: '#d29922', marginBottom: '16px', padding: '10px 12px', background: '#2b230f', border: '1px solid #d29922', borderRadius: '6px', fontSize: '13px' }}>
          {notice}
        </div>
      )}
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
      {showIndexStatus && (
        <div style={{ marginBottom: '16px', maxWidth: '420px' }}>
          <IndexStatusCard
            status={indexStatus}
            loading={indexStatusLoading}
            error={indexStatusError}
            refName={gitRef}
          />
        </div>
      )}

      {isDir ? (
        entries ? (
          <FileTree entries={entries} owner={owner} repo={repo} ref={gitRef} currentPath={path || ''} />
        ) : (
          <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>
        )
      ) : (
        blob ? (
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', gap: '16px' }}>
            <div style={{ flex: '1 1 640px', minWidth: 0 }}>
              {isMarkdown ? (
                <MarkdownViewer
                  filename={path || ''}
                  source={blob.content || ''}
                  owner={owner}
                  repo={repo}
                  gitRef={gitRef}
                  path={path || ''}
                />
              ) : (
                <CodeViewer
                  filename={path || ''}
                  source={blob.content || ''}
                  owner={owner}
                  repo={repo}
                  gitRef={gitRef}
                  path={path || ''}
                />
              )}
            </div>
            <EntityBlamePanel
              entities={entities}
              selectedKey={selectedEntity?.key || ''}
              blame={blame}
              blameLoading={blameLoading}
              onSelect={(entity) => setSelectedEntity(entity)}
            />
          </div>
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

function decodeBlobContent(data: unknown): { content: string; error?: string } {
  if (typeof data !== 'string' || data === '') return { content: '' };

  try {
    const binary = atob(data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return { content: new TextDecoder().decode(bytes) };
  } catch {
    return {
      content: '',
      error: 'Failed to decode file contents returned by the server.',
    };
  }
}

function isMarkdownPath(filePath: string): boolean {
  const lower = filePath.toLowerCase();
  return lower.endsWith('.md') || lower.endsWith('.markdown');
}

function EntityBlamePanel({
  entities,
  selectedKey,
  blame,
  blameLoading,
  onSelect,
}: {
  entities: FileEntity[] | null;
  selectedKey: string;
  blame: EntityBlameInfo | null;
  blameLoading: boolean;
  onSelect: (entity: FileEntity) => void;
}) {
  return (
    <aside style={{ flex: '1 1 320px', width: '320px', maxWidth: '100%', border: '1px solid #30363d', borderRadius: '6px', overflow: 'hidden' }}>
      <div style={{ padding: '10px 12px', borderBottom: '1px solid #30363d', background: '#161b22' }}>
        <strong style={{ color: '#f0f6fc', fontSize: '13px' }}>Structural Blame</strong>
      </div>

      {entities === null && (
        <div style={{ color: '#8b949e', fontSize: '13px', padding: '12px' }}>Loading entities...</div>
      )}

      {entities !== null && entities.length === 0 && (
        <div style={{ color: '#8b949e', fontSize: '13px', padding: '12px' }}>No entities detected for this file.</div>
      )}

      {entities !== null && entities.length > 0 && (
        <div style={{ maxHeight: '240px', overflowY: 'auto', borderBottom: '1px solid #30363d' }}>
          {entities.map((entity) => (
            <button
              key={entity.key}
              onClick={() => onSelect(entity)}
              style={{
                width: '100%',
                textAlign: 'left',
                border: 'none',
                borderTop: '1px solid #21262d',
                background: selectedKey === entity.key ? '#1f2937' : '#0d1117',
                color: '#c9d1d9',
                padding: '8px 10px',
                cursor: 'pointer',
              }}
            >
              <div style={{ fontFamily: 'monospace', fontSize: '12px' }}>{entity.name || entity.key}</div>
              <div style={{ color: '#8b949e', fontSize: '11px' }}>
                {entity.decl_kind} · L{entity.start_line}-{entity.end_line}
              </div>
            </button>
          ))}
        </div>
      )}

      <div style={{ padding: '12px' }}>
        {blameLoading ? (
          <div style={{ color: '#8b949e', fontSize: '13px' }}>Resolving blame...</div>
        ) : blame ? (
          <div style={{ display: 'grid', gap: '6px' }}>
            <div style={{ color: '#58a6ff', fontFamily: 'monospace', fontSize: '12px' }}>{shortHash(blame.commit_hash)}</div>
            <div style={{ color: '#c9d1d9', fontSize: '13px' }}>{blame.author || 'unknown'}</div>
            <div style={{ color: '#8b949e', fontSize: '12px' }}>{formatTimestamp(blame.timestamp)}</div>
            <div style={{ color: '#c9d1d9', fontSize: '12px' }}>{blame.message || '(no message)'}</div>
          </div>
        ) : (
          <div style={{ color: '#8b949e', fontSize: '13px' }}>Select an entity to view attribution.</div>
        )}
      </div>
    </aside>
  );
}

function shortHash(hash: string): string {
  if (!hash) return '';
  return hash.length > 12 ? hash.slice(0, 12) : hash;
}

function formatTimestamp(ts: number): string {
  if (!ts) return 'unknown time';
  return new Date(ts * 1000).toLocaleString();
}
