import { useState } from 'preact/hooks';
import { findReferences } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
}

const COLORS = {
  bg: '#0d1117',
  text: '#c9d1d9',
  heading: '#f0f6fc',
  muted: '#8b949e',
  link: '#58a6ff',
  green: '#238636',
  red: '#f85149',
  border: '#30363d',
  surface: '#161b22',
  active: '#1f6feb',
};

const inputStyle: Record<string, string> = {
  background: '#0d1117',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  color: '#c9d1d9',
  fontSize: '14px',
};

const KIND_COLORS: Record<string, string> = {
  function: '#58a6ff',
  type: '#238636',
  method: '#a371f7',
  variable: '#d29922',
  constant: '#e3b341',
  reference: '#58a6ff',
  definition: '#238636',
  call: '#a371f7',
};

function kindColor(kind: string): string {
  const lower = kind.toLowerCase();
  return KIND_COLORS[lower] || COLORS.muted;
}

interface FileGroup {
  file: string;
  refs: any[];
}

function groupByFile(results: any[]): FileGroup[] {
  const map = new Map<string, any[]>();
  for (const ref of results) {
    const list = map.get(ref.file) || [];
    list.push(ref);
    map.set(ref.file, list);
  }
  return Array.from(map.entries()).map(([file, refs]) => ({ file, refs }));
}

export function ReferencesView({ owner, repo, ref: gitRef, path }: Props) {
  const [name, setName] = useState('');
  const [results, setResults] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [searched, setSearched] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const doSearch = (e?: Event) => {
    if (e) e.preventDefault();
    if (!owner || !repo || !gitRef || !name.trim()) return;

    setLoading(true);
    setError('');
    setSearched(true);
    setCollapsed({});
    findReferences(owner, repo, gitRef, name.trim())
      .then((data) => {
        setResults(data || []);
      })
      .catch((err) => {
        setError(err.message || 'Search failed');
        setResults([]);
      })
      .finally(() => setLoading(false));
  };

  const toggleFile = (file: string) => {
    setCollapsed((prev) => ({ ...prev, [file]: !prev[file] }));
  };

  const groups = groupByFile(results);

  return (
    <div style={{ color: COLORS.text }}>
      <h1 style={{ fontSize: '20px', color: COLORS.heading, margin: '0 0 16px 0' }}>
        Find References
      </h1>

      <form
        onSubmit={doSearch}
        style={{ display: 'flex', gap: '8px', marginBottom: '16px', flexWrap: 'wrap' }}
      >
        <input
          type="text"
          placeholder="Symbol name..."
          value={name}
          onInput={(e: any) => setName(e.target.value)}
          style={{ ...inputStyle, flex: '1', minWidth: '200px' }}
        />
        <button
          type="submit"
          disabled={loading || !name.trim()}
          style={{
            background: COLORS.active,
            color: '#fff',
            border: 'none',
            borderRadius: '6px',
            padding: '10px 16px',
            cursor: loading || !name.trim() ? 'not-allowed' : 'pointer',
            fontSize: '14px',
            fontWeight: 'bold',
            opacity: loading || !name.trim() ? 0.6 : 1,
          }}
        >
          {loading ? 'Searching...' : 'Find References'}
        </button>
      </form>

      {error && (
        <div style={{ color: COLORS.red, marginBottom: '12px', fontSize: '14px' }}>
          {error}
        </div>
      )}

      {loading && (
        <div style={{ color: COLORS.muted, padding: '20px 0', fontSize: '14px' }}>
          Searching for references...
        </div>
      )}

      {!loading && searched && results.length === 0 && !error && (
        <div
          style={{
            border: `1px solid ${COLORS.border}`,
            borderRadius: '6px',
            padding: '24px',
            color: COLORS.muted,
            textAlign: 'center',
            fontSize: '14px',
          }}
        >
          No references found for "{name}"
        </div>
      )}

      {!loading && results.length > 0 && (
        <div>
          <div style={{ color: COLORS.muted, fontSize: '13px', marginBottom: '12px' }}>
            {results.length} reference{results.length !== 1 ? 's' : ''} across{' '}
            {groups.length} file{groups.length !== 1 ? 's' : ''}
          </div>

          {groups.map((group) => {
            const isCollapsed = !!collapsed[group.file];
            return (
              <div
                key={group.file}
                style={{
                  border: `1px solid ${COLORS.border}`,
                  borderRadius: '6px',
                  marginBottom: '8px',
                }}
              >
                <div
                  onClick={() => toggleFile(group.file)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '10px 14px',
                    background: COLORS.surface,
                    cursor: 'pointer',
                    borderRadius: isCollapsed ? '6px' : '6px 6px 0 0',
                    userSelect: 'none',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span
                      style={{
                        color: COLORS.muted,
                        fontSize: '12px',
                        display: 'inline-block',
                        width: '12px',
                        textAlign: 'center',
                      }}
                    >
                      {isCollapsed ? '\u25B6' : '\u25BC'}
                    </span>
                    <a
                      href={`/${owner}/${repo}/blob/${gitRef}/${group.file}`}
                      onClick={(e: Event) => e.stopPropagation()}
                      style={{ color: COLORS.link, fontSize: '14px', textDecoration: 'none' }}
                    >
                      {group.file}
                    </a>
                  </div>
                  <span style={{ color: COLORS.muted, fontSize: '12px' }}>
                    {group.refs.length} reference{group.refs.length !== 1 ? 's' : ''}
                  </span>
                </div>

                {!isCollapsed && (
                  <div>
                    {group.refs.map((ref, idx) => (
                      <a
                        key={`${ref.start_line}-${ref.start_column}-${idx}`}
                        href={`/${owner}/${repo}/blob/${gitRef}/${group.file}`}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: '10px',
                          padding: '8px 14px 8px 34px',
                          borderTop: `1px solid ${COLORS.border}`,
                          textDecoration: 'none',
                          color: COLORS.text,
                          fontSize: '13px',
                        }}
                      >
                        <span
                          style={{
                            color: COLORS.muted,
                            fontFamily: 'monospace',
                            fontSize: '12px',
                            minWidth: '50px',
                          }}
                        >
                          L{ref.start_line}
                          {ref.end_line && ref.end_line !== ref.start_line
                            ? `-${ref.end_line}`
                            : ''}
                        </span>
                        {ref.start_column != null && (
                          <span
                            style={{
                              color: COLORS.muted,
                              fontFamily: 'monospace',
                              fontSize: '12px',
                            }}
                          >
                            col {ref.start_column}
                            {ref.end_column != null && ref.end_column !== ref.start_column
                              ? `-${ref.end_column}`
                              : ''}
                          </span>
                        )}
                        <span
                          style={{
                            display: 'inline-block',
                            padding: '1px 6px',
                            borderRadius: '12px',
                            fontSize: '11px',
                            fontWeight: 'bold',
                            color: kindColor(ref.kind),
                            border: `1px solid ${kindColor(ref.kind)}`,
                            textTransform: 'lowercase',
                          }}
                        >
                          {ref.kind}
                        </span>
                        <span style={{ color: COLORS.heading, fontSize: '13px' }}>
                          {ref.name}
                        </span>
                      </a>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {!loading && !searched && !error && (
        <div style={{ color: COLORS.muted, fontSize: '14px', padding: '20px 0' }}>
          Enter a symbol name and click "Find References" to search for all references in this
          repository.
        </div>
      )}
    </div>
  );
}
