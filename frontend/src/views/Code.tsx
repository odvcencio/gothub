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
import { CodeViewer, type BlameEntry } from '../components/CodeViewer';
import { MarkdownViewer } from '../components/MarkdownViewer';
import { IndexStatusCard } from '../components/IndexStatusCard';

/* Inject shimmer keyframe animation once */
if (typeof document !== 'undefined' && !document.getElementById('gothub-shimmer')) {
  const s = document.createElement('style');
  s.id = 'gothub-shimmer';
  s.textContent = '@keyframes shimmer { 0% { background-position: 200% 0; } 100% { background-position: -200% 0; } }';
  document.head.appendChild(s);
}

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
}

function LoadingSkeleton({ lines = 8 }: { lines?: number }) {
  return (
    <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px' }}>
      {Array.from({ length: lines }).map((_, i) => (
        <div key={i} style={{
          height: '14px',
          background: 'linear-gradient(90deg, #21262d 25%, #30363d 50%, #21262d 75%)',
          backgroundSize: '200% 100%',
          animation: 'shimmer 1.5s infinite',
          borderRadius: '4px',
          marginBottom: i < lines - 1 ? '8px' : 0,
          width: `${60 + (i * 7) % 40}%`,
        }} />
      ))}
    </div>
  );
}

function ActionButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} style={{
      background: '#21262d', color: '#c9d1d9', border: '1px solid #30363d',
      borderRadius: '6px', padding: '4px 10px', cursor: 'pointer', fontSize: '12px',
    }}>
      {label}
    </button>
  );
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(() => {});
}

function downloadFile(filename: string, content: string) {
  const b = new Blob([content], { type: 'text/plain' });
  const url = URL.createObjectURL(b);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename.split('/').pop() || 'file';
  a.click();
  URL.revokeObjectURL(url);
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
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
  const [showRaw, setShowRaw] = useState(false);
  const [showBlame, setShowBlame] = useState(false);

  /* Task 8: responsive layout */
  const [narrow, setNarrow] = useState(false);
  useEffect(() => {
    const check = () => setNarrow(window.innerWidth < 768);
    check();
    window.addEventListener('resize', check);
    return () => window.removeEventListener('resize', check);
  }, []);

  const appendNotice = (message: string) => {
    const next = message.trim();
    if (!next) return;
    setNotice((current) => {
      if (!current) return next;
      if (current.includes(next)) return current;
      return `${current} \u2022 ${next}`;
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
    setShowRaw(false);
    setShowBlame(false);

    // Determine if path is a blob or tree based on the URL
    const isBlobUrl = isBlobRoute();

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
          const list = Array.isArray(resp?.entities) ? resp.entities : [];
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
        .then((items) => setEntries(items || []))
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

  if (isBlobRoute() && !path) {
    return <div style={{ color: '#f85149', padding: '20px' }}>Missing file path for blob view</div>;
  }

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;

  const breadcrumbs = buildBreadcrumbs(owner, repo, gitRef, path);
  const showIndexStatus = indexStatusLoading || !!indexStatusError || !!indexStatus;
  const isMarkdown = isMarkdownPath(path || '');

  /* Task 6: build blame data for CodeViewer from entities + per-entity blame */
  const blameData: BlameEntry[] = [];
  if (showBlame && entities && blame && selectedEntity) {
    /* For V1, show blame annotation for the currently selected entity only */
    const entry = entities.find((e) => e.key === selectedEntity.key);
    if (entry && blame.commit_hash) {
      blameData.push({
        start_line: entry.start_line,
        end_line: entry.end_line,
        author: blame.author || 'unknown',
        commit_hash: blame.commit_hash,
      });
    }
  }

  /* Task 5: permalink */
  const handlePermalink = () => {
    if (typeof window !== 'undefined') {
      copyToClipboard(window.location.href);
    }
  };

  return (
    <div>
      {notice && (
        <div style={{ color: '#d29922', marginBottom: '16px', padding: '10px 12px', background: '#2b230f', border: '1px solid #d29922', borderRadius: '6px', fontSize: '13px' }}>
          {notice}
        </div>
      )}

      {/* Task 9: sticky file header */}
      <div style={{
        position: 'sticky', top: 0, zIndex: 10,
        background: '#0d1117', paddingBottom: '8px',
      }}>
        <div style={{ marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
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

        {/* Task 5: file info bar + action buttons */}
        {!isDir && blob && (
          <div style={{
            display: 'flex', justifyContent: 'space-between', alignItems: 'center',
            padding: '8px 12px', background: '#161b22',
            border: '1px solid #30363d', borderRadius: '6px 6px 0 0',
            borderBottom: 'none', flexWrap: 'wrap', gap: '8px',
          }}>
            <div style={{ color: '#8b949e', fontSize: '12px', display: 'flex', gap: '12px' }}>
              <span>{blob.content.split('\n').length} lines</span>
              <span>{formatFileSize(new Blob([blob.content]).size)}</span>
            </div>
            <div style={{ display: 'flex', gap: '4px' }}>
              <ActionButton label="Copy" onClick={() => copyToClipboard(blob.content)} />
              <ActionButton label={showRaw ? 'Highlighted' : 'Raw'} onClick={() => setShowRaw(!showRaw)} />
              <ActionButton label="Download" onClick={() => downloadFile(path || 'file', blob.content)} />
              <ActionButton label="Permalink" onClick={handlePermalink} />
              {!isMarkdown && (
                <ActionButton label={showBlame ? 'Hide Blame' : 'Blame'} onClick={() => setShowBlame(!showBlame)} />
              )}
            </div>
          </div>
        )}
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
          <LoadingSkeleton lines={10} />
        )
      ) : (
        blob ? (
          <div style={{ display: 'flex', flexDirection: narrow ? 'column' : 'row', flexWrap: 'wrap', alignItems: 'flex-start', gap: '16px' }}>
            <div style={{ flex: '1 1 640px', minWidth: 0 }}>
              {showRaw ? (
                <pre style={{
                  background: '#0d1117',
                  border: '1px solid #30363d',
                  borderRadius: !isDir && blob ? '0 0 6px 6px' : '6px',
                  padding: '16px',
                  fontSize: '13px',
                  lineHeight: '1.5',
                  overflowX: 'auto',
                  color: '#c9d1d9',
                  fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
                }}>
                  {blob.content}
                </pre>
              ) : isMarkdown ? (
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
                  showBlame={showBlame}
                  blameData={blameData.length > 0 ? blameData : undefined}
                />
              )}
            </div>
            <EntityBlamePanel
              entities={entities}
              selectedKey={selectedEntity?.key || ''}
              blame={blame}
              blameLoading={blameLoading}
              onSelect={(entity) => setSelectedEntity(entity)}
              narrow={narrow}
            />
          </div>
        ) : (
          <LoadingSkeleton lines={16} />
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

function isBlobRoute(): boolean {
  if (typeof window === 'undefined') return false;
  return window.location.pathname.includes('/blob/');
}

function EntityBlamePanel({
  entities,
  selectedKey,
  blame,
  blameLoading,
  onSelect,
  narrow,
}: {
  entities: FileEntity[] | null;
  selectedKey: string;
  blame: EntityBlameInfo | null;
  blameLoading: boolean;
  onSelect: (entity: FileEntity) => void;
  narrow: boolean;
}) {
  return (
    <aside style={{
      flex: '1 1 320px',
      width: narrow ? '100%' : '320px',
      maxWidth: '100%',
      border: '1px solid #30363d',
      borderRadius: '6px',
      overflow: 'hidden',
    }}>
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
