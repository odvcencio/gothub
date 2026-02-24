import { useState } from 'preact/hooks';
import { getCallGraph } from '../api/client';

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
};

function kindColor(kind: string): string {
  const lower = kind.toLowerCase();
  return KIND_COLORS[lower] || COLORS.muted;
}

interface CallerGroup {
  callerName: string;
  callerFile: string;
  edges: any[];
}

function groupEdgesByCaller(edges: any[]): CallerGroup[] {
  const map = new Map<string, CallerGroup>();
  for (const edge of edges) {
    const key = `${edge.caller_name}::${edge.caller_file}`;
    if (!map.has(key)) {
      map.set(key, {
        callerName: edge.caller_name,
        callerFile: edge.caller_file,
        edges: [],
      });
    }
    map.get(key)!.edges.push(edge);
  }
  return Array.from(map.values());
}

export function CallGraphView({ owner, repo, ref: gitRef, path }: Props) {
  const [symbol, setSymbol] = useState('');
  const [depth, setDepth] = useState(3);
  const [reverse, setReverse] = useState(false);
  const [definitions, setDefinitions] = useState<any[]>([]);
  const [edges, setEdges] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [searched, setSearched] = useState(false);

  const doSearch = (e?: Event) => {
    if (e) e.preventDefault();
    if (!owner || !repo || !gitRef || !symbol.trim()) return;

    setLoading(true);
    setError('');
    setSearched(true);
    getCallGraph(owner, repo, gitRef, symbol.trim(), depth, reverse)
      .then((data) => {
        setDefinitions(data?.definitions || []);
        setEdges(data?.edges || []);
      })
      .catch((err) => {
        setError(err.message || 'Failed to load call graph');
        setDefinitions([]);
        setEdges([]);
      })
      .finally(() => setLoading(false));
  };

  const setMode = (isReverse: boolean) => {
    setReverse(isReverse);
  };

  const callerGroups = groupEdgesByCaller(edges);
  const hasResults = definitions.length > 0 || edges.length > 0;

  return (
    <div style={{ color: COLORS.text }}>
      <h1 style={{ fontSize: '20px', color: COLORS.heading, margin: '0 0 16px 0' }}>
        Call Graph
      </h1>

      <form onSubmit={doSearch} style={{ marginBottom: '16px' }}>
        <div style={{ display: 'flex', gap: '8px', marginBottom: '10px', flexWrap: 'wrap' }}>
          <input
            type="text"
            placeholder="Symbol name..."
            value={symbol}
            onInput={(e: any) => setSymbol(e.target.value)}
            style={{ ...inputStyle, flex: '1', minWidth: '200px' }}
          />
          <input
            type="number"
            min={1}
            max={10}
            value={depth}
            onInput={(e: any) => setDepth(parseInt(e.target.value, 10) || 3)}
            style={{ ...inputStyle, width: '80px' }}
            title="Depth"
          />
          <button
            type="submit"
            disabled={loading || !symbol.trim()}
            style={{
              background: COLORS.active,
              color: '#fff',
              border: 'none',
              borderRadius: '6px',
              padding: '10px 16px',
              cursor: loading || !symbol.trim() ? 'not-allowed' : 'pointer',
              fontSize: '14px',
              fontWeight: 'bold',
              opacity: loading || !symbol.trim() ? 0.6 : 1,
            }}
          >
            {loading ? 'Loading...' : 'Search'}
          </button>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '16px', flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: '4px' }}>
            <button
              type="button"
              onClick={() => setMode(false)}
              style={{
                background: !reverse ? COLORS.active : COLORS.surface,
                color: COLORS.text,
                border: `1px solid ${COLORS.border}`,
                borderRadius: '6px 0 0 6px',
                padding: '6px 12px',
                cursor: 'pointer',
                fontSize: '13px',
              }}
            >
              Callees
            </button>
            <button
              type="button"
              onClick={() => setMode(true)}
              style={{
                background: reverse ? COLORS.active : COLORS.surface,
                color: COLORS.text,
                border: `1px solid ${COLORS.border}`,
                borderRadius: '0 6px 6px 0',
                padding: '6px 12px',
                cursor: 'pointer',
                fontSize: '13px',
              }}
            >
              Callers
            </button>
          </div>

          <label
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              color: COLORS.muted,
              fontSize: '13px',
            }}
          >
            <span>Depth: {depth}</span>
          </label>
        </div>
      </form>

      {error && (
        <div style={{ color: COLORS.red, marginBottom: '12px', fontSize: '14px' }}>
          {error}
        </div>
      )}

      {loading && (
        <div style={{ color: COLORS.muted, padding: '20px 0', fontSize: '14px' }}>
          Loading call graph...
        </div>
      )}

      {!loading && searched && !hasResults && !error && (
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
          No call graph data found for "{symbol}"
        </div>
      )}

      {!loading && hasResults && (
        <div>
          {/* Definitions section */}
          {definitions.length > 0 && (
            <div style={{ marginBottom: '20px' }}>
              <h2 style={{ fontSize: '16px', color: COLORS.heading, margin: '0 0 10px 0' }}>
                Definitions
                <span style={{ color: COLORS.muted, fontWeight: 'normal', fontSize: '13px', marginLeft: '8px' }}>
                  ({definitions.length})
                </span>
              </h2>
              <div style={{ border: `1px solid ${COLORS.border}`, borderRadius: '6px' }}>
                {definitions.map((def, idx) => (
                  <a
                    key={def.id || `${def.name}-${def.file}-${idx}`}
                    href={`/${owner}/${repo}/blob/${gitRef}/${def.file}`}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '10px',
                      padding: '10px 14px',
                      borderTop: idx === 0 ? 'none' : `1px solid ${COLORS.border}`,
                      textDecoration: 'none',
                      color: COLORS.text,
                    }}
                  >
                    <span
                      style={{
                        display: 'inline-block',
                        padding: '2px 8px',
                        borderRadius: '12px',
                        fontSize: '11px',
                        fontWeight: 'bold',
                        color: kindColor(def.kind),
                        border: `1px solid ${kindColor(def.kind)}`,
                        textTransform: 'lowercase',
                        flexShrink: 0,
                      }}
                    >
                      {def.kind}
                    </span>
                    <span style={{ color: COLORS.heading, fontWeight: 'bold', fontSize: '14px' }}>
                      {def.name}
                    </span>
                    <span style={{ color: COLORS.link, fontSize: '12px', marginLeft: 'auto' }}>
                      {def.file}
                    </span>
                  </a>
                ))}
              </div>
            </div>
          )}

          {/* Edges section */}
          {edges.length > 0 && (
            <div>
              <h2 style={{ fontSize: '16px', color: COLORS.heading, margin: '0 0 10px 0' }}>
                {reverse ? 'Caller' : 'Callee'} Edges
                <span style={{ color: COLORS.muted, fontWeight: 'normal', fontSize: '13px', marginLeft: '8px' }}>
                  ({edges.length})
                </span>
              </h2>

              {callerGroups.map((group) => (
                <div
                  key={`${group.callerName}-${group.callerFile}`}
                  style={{
                    border: `1px solid ${COLORS.border}`,
                    borderRadius: '6px',
                    marginBottom: '8px',
                  }}
                >
                  <div
                    style={{
                      padding: '10px 14px',
                      background: COLORS.surface,
                      borderRadius: '6px 6px 0 0',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '8px',
                      flexWrap: 'wrap',
                    }}
                  >
                    <span style={{ color: COLORS.muted, fontSize: '12px' }}>
                      {reverse ? 'caller' : 'from'}
                    </span>
                    <a
                      href={`/${owner}/${repo}/blob/${gitRef}/${group.callerFile}`}
                      style={{ color: COLORS.heading, fontWeight: 'bold', fontSize: '14px', textDecoration: 'none' }}
                    >
                      {group.callerName}
                    </a>
                    <span style={{ color: COLORS.link, fontSize: '12px' }}>
                      {group.callerFile}
                    </span>
                    <span style={{ color: COLORS.muted, fontSize: '12px', marginLeft: 'auto' }}>
                      {group.edges.length} call{group.edges.length !== 1 ? 's' : ''}
                    </span>
                  </div>

                  {group.edges.map((edge, idx) => (
                    <a
                      key={`${edge.callee_name}-${edge.callee_file}-${idx}`}
                      href={`/${owner}/${repo}/blob/${gitRef}/${edge.callee_file}`}
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
                      <span style={{ color: COLORS.muted, fontSize: '14px' }}>
                        {'\u2192'}
                      </span>
                      <span style={{ color: COLORS.heading, fontWeight: 'bold' }}>
                        {edge.callee_name}
                      </span>
                      <span style={{ color: COLORS.link, fontSize: '12px' }}>
                        {edge.callee_file}
                      </span>
                      {edge.count != null && edge.count > 0 && (
                        <span
                          style={{
                            color: COLORS.muted,
                            fontSize: '11px',
                            marginLeft: 'auto',
                            background: COLORS.surface,
                            border: `1px solid ${COLORS.border}`,
                            borderRadius: '10px',
                            padding: '1px 8px',
                          }}
                        >
                          {edge.count}x
                        </span>
                      )}
                    </a>
                  ))}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {!loading && !searched && !error && (
        <div style={{ color: COLORS.muted, fontSize: '14px', padding: '20px 0' }}>
          Enter a symbol name and click "Search" to explore the call graph.
          Use "Callees" to see what a function calls, or "Callers" to see what calls it.
        </div>
      )}
    </div>
  );
}
