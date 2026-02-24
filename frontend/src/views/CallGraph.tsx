import { useMemo, useState } from 'preact/hooks';
import { type CallGraphEdge, type SymbolResult, getCallGraph } from '../api/client';

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
  red: '#f85149',
  border: '#30363d',
  surface: '#161b22',
  active: '#1f6feb',
  nodeFill: '#22262d',
  nodeStroke: '#4a5260',
  nodeRelated: '#275da8',
  nodeDim: '#1a1f26',
  edgeDefault: '#6e7681',
  edgeDim: '#3b434f',
  edgeInbound: '#e3b341',
  edgeOutbound: '#58a6ff',
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

const NODE_WIDTH = 176;
const NODE_HEIGHT = 46;
const HORIZONTAL_GAP = 240;
const VERTICAL_GAP = 84;
const PADDING_X = 92;
const PADDING_Y = 68;

interface GraphNode {
  id: string;
  name: string;
  file: string;
  kind?: string;
}

interface GraphEdge {
  id: string;
  source: string;
  target: string;
  count?: number;
}

interface PositionedNode extends GraphNode {
  x: number;
  y: number;
}

interface GraphLayout {
  width: number;
  height: number;
  nodes: PositionedNode[];
}

function kindColor(kind?: string): string {
  if (!kind) return COLORS.muted;
  const lower = kind.toLowerCase();
  return KIND_COLORS[lower] || COLORS.muted;
}

function nodeId(name: string, file: string): string {
  return `${name}::${file}`;
}

function truncateLabel(label: string, max = 22): string {
  if (label.length <= max) return label;
  return `${label.slice(0, max - 1)}\u2026`;
}

function buildGraph(definitions: SymbolResult[], edges: CallGraphEdge[]) {
  const nodesById = new Map<string, GraphNode>();

  for (const def of definitions) {
    nodesById.set(nodeId(def.name, def.file), {
      id: nodeId(def.name, def.file),
      name: def.name,
      file: def.file,
      kind: def.kind,
    });
  }

  const graphEdges: GraphEdge[] = edges.map((edge, idx) => {
    const sourceId = nodeId(edge.caller_name, edge.caller_file);
    const targetId = nodeId(edge.callee_name, edge.callee_file);

    if (!nodesById.has(sourceId)) {
      nodesById.set(sourceId, {
        id: sourceId,
        name: edge.caller_name,
        file: edge.caller_file,
      });
    }

    if (!nodesById.has(targetId)) {
      nodesById.set(targetId, {
        id: targetId,
        name: edge.callee_name,
        file: edge.callee_file,
      });
    }

    return {
      id: `${sourceId}->${targetId}-${idx}`,
      source: sourceId,
      target: targetId,
      count: typeof edge.count === 'number' ? edge.count : undefined,
    };
  });

  return {
    nodes: Array.from(nodesById.values()),
    edges: graphEdges,
  };
}

function layoutGraph(nodes: GraphNode[], edges: GraphEdge[], focusNodeId: string | null): GraphLayout {
  if (nodes.length === 0) {
    return {
      width: 820,
      height: 340,
      nodes: [],
    };
  }

  const outgoing = new Map<string, string[]>();
  const indegree = new Map<string, number>();

  for (const node of nodes) {
    outgoing.set(node.id, []);
    indegree.set(node.id, 0);
  }

  for (const edge of edges) {
    outgoing.get(edge.source)?.push(edge.target);
    indegree.set(edge.target, (indegree.get(edge.target) || 0) + 1);
  }

  const layerByNode = new Map<string, number>();
  const queue: string[] = [];

  if (focusNodeId && outgoing.has(focusNodeId)) {
    layerByNode.set(focusNodeId, 0);
    queue.push(focusNodeId);
  } else {
    for (const node of nodes) {
      if ((indegree.get(node.id) || 0) === 0) {
        layerByNode.set(node.id, 0);
        queue.push(node.id);
      }
    }
  }

  if (queue.length === 0) {
    const fallbackId = nodes[0]?.id;
    if (fallbackId) {
      layerByNode.set(fallbackId, 0);
      queue.push(fallbackId);
    }
  }

  while (queue.length > 0) {
    const current = queue.shift();
    if (!current) continue;

    const layer = layerByNode.get(current) || 0;
    for (const next of outgoing.get(current) || []) {
      const nextLayer = layer + 1;
      const existing = layerByNode.get(next);
      if (existing == null || nextLayer < existing) {
        layerByNode.set(next, nextLayer);
        queue.push(next);
      }
    }
  }

  let maxLayer = 0;
  for (const layer of layerByNode.values()) {
    if (layer > maxLayer) maxLayer = layer;
  }

  const fallbackLayer = maxLayer + 1;
  for (const node of nodes) {
    if (!layerByNode.has(node.id)) {
      layerByNode.set(node.id, fallbackLayer);
    }
  }

  maxLayer = Math.max(maxLayer, fallbackLayer);

  const layers = new Map<number, GraphNode[]>();
  for (const node of nodes) {
    const layer = layerByNode.get(node.id) || 0;
    const list = layers.get(layer);
    if (list) {
      list.push(node);
    } else {
      layers.set(layer, [node]);
    }
  }

  const layerIds = Array.from(layers.keys()).sort((a, b) => a - b);
  for (const layerId of layerIds) {
    layers.get(layerId)?.sort((a, b) => a.name.localeCompare(b.name));
  }

  const maxNodesInLayer = Math.max(...Array.from(layers.values()).map((layer) => layer.length), 1);
  const width = PADDING_X * 2 + maxLayer * HORIZONTAL_GAP + NODE_WIDTH;
  const height = Math.max(340, PADDING_Y * 2 + (maxNodesInLayer - 1) * VERTICAL_GAP + NODE_HEIGHT);

  const positionedNodes: PositionedNode[] = [];
  for (const layerId of layerIds) {
    const layerNodes = layers.get(layerId) || [];
    const x = PADDING_X + layerId * HORIZONTAL_GAP + NODE_WIDTH / 2;
    const span = (layerNodes.length - 1) * VERTICAL_GAP;
    const startY = (height - span) / 2;

    layerNodes.forEach((node, idx) => {
      positionedNodes.push({
        ...node,
        x,
        y: startY + idx * VERTICAL_GAP,
      });
    });
  }

  return {
    width,
    height,
    nodes: positionedNodes,
  };
}

function edgePath(source: PositionedNode, target: PositionedNode): string {
  const dx = target.x - source.x;
  const dy = target.y - source.y;

  let startX = source.x;
  let startY = source.y;
  let endX = target.x;
  let endY = target.y;

  if (Math.abs(dx) >= Math.abs(dy)) {
    if (dx >= 0) {
      startX += NODE_WIDTH / 2;
      endX -= NODE_WIDTH / 2;
    } else {
      startX -= NODE_WIDTH / 2;
      endX += NODE_WIDTH / 2;
    }

    const curve = Math.max(40, Math.abs(dx) * 0.42);
    const c1x = startX + (dx >= 0 ? curve : -curve);
    const c2x = endX - (dx >= 0 ? curve : -curve);
    return `M ${startX} ${startY} C ${c1x} ${startY}, ${c2x} ${endY}, ${endX} ${endY}`;
  }

  if (dy >= 0) {
    startY += NODE_HEIGHT / 2;
    endY -= NODE_HEIGHT / 2;
  } else {
    startY -= NODE_HEIGHT / 2;
    endY += NODE_HEIGHT / 2;
  }

  const curve = Math.max(40, Math.abs(dy) * 0.42);
  const c1y = startY + (dy >= 0 ? curve : -curve);
  const c2y = endY - (dy >= 0 ? curve : -curve);
  return `M ${startX} ${startY} C ${startX} ${c1y}, ${endX} ${c2y}, ${endX} ${endY}`;
}

function markerIdForEdge(kind: 'default' | 'inbound' | 'outbound' | 'dim'): string {
  if (kind === 'outbound') return 'callgraph-arrow-outbound';
  if (kind === 'inbound') return 'callgraph-arrow-inbound';
  if (kind === 'dim') return 'callgraph-arrow-dim';
  return 'callgraph-arrow-default';
}

function defaultFocusNodeId(symbol: string, definitions: SymbolResult[], edges: CallGraphEdge[]): string | null {
  const target = symbol.trim().toLowerCase();

  if (target) {
    const defMatch = definitions.find((def) => def.name.toLowerCase() === target);
    if (defMatch) return nodeId(defMatch.name, defMatch.file);

    const callerMatch = edges.find((edge) => edge.caller_name.toLowerCase() === target);
    if (callerMatch) return nodeId(callerMatch.caller_name, callerMatch.caller_file);

    const calleeMatch = edges.find((edge) => edge.callee_name.toLowerCase() === target);
    if (calleeMatch) return nodeId(calleeMatch.callee_name, calleeMatch.callee_file);
  }

  if (definitions[0]) return nodeId(definitions[0].name, definitions[0].file);
  if (edges[0]) return nodeId(edges[0].caller_name, edges[0].caller_file);
  return null;
}

export function CallGraphView({ owner, repo, ref: gitRef }: Props) {
  const [symbol, setSymbol] = useState('');
  const [depth, setDepth] = useState(3);
  const [reverse, setReverse] = useState(false);
  const [definitions, setDefinitions] = useState<SymbolResult[]>([]);
  const [edges, setEdges] = useState<CallGraphEdge[]>([]);
  const [focusedNodeId, setFocusedNodeId] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [searched, setSearched] = useState(false);

  const graphData = useMemo(() => buildGraph(definitions, edges), [definitions, edges]);
  const layout = useMemo(
    () => layoutGraph(graphData.nodes, graphData.edges, focusedNodeId),
    [graphData, focusedNodeId],
  );

  const nodesById = useMemo(() => {
    const map = new Map<string, PositionedNode>();
    for (const node of layout.nodes) map.set(node.id, node);
    return map;
  }, [layout.nodes]);

  const focusInfo = useMemo(() => {
    const inbound = new Set<string>();
    const outbound = new Set<string>();
    const edgeIds = new Set<string>();

    if (!focusedNodeId) {
      return { inbound, outbound, edgeIds };
    }

    for (const edge of graphData.edges) {
      if (edge.target === focusedNodeId) {
        inbound.add(edge.source);
        edgeIds.add(edge.id);
      }
      if (edge.source === focusedNodeId) {
        outbound.add(edge.target);
        edgeIds.add(edge.id);
      }
    }

    return { inbound, outbound, edgeIds };
  }, [focusedNodeId, graphData.edges]);

  const hasResults = definitions.length > 0 || edges.length > 0;

  const doSearch = (e?: Event) => {
    if (e) e.preventDefault();
    if (!owner || !repo || !gitRef || !symbol.trim()) return;

    setLoading(true);
    setError('');
    setSearched(true);

    getCallGraph(owner, repo, gitRef, symbol.trim(), depth, reverse)
      .then((data) => {
        const nextDefinitions = data?.definitions || [];
        const nextEdges = data?.edges || [];

        setDefinitions(nextDefinitions);
        setEdges(nextEdges);
        setFocusedNodeId(defaultFocusNodeId(symbol, nextDefinitions, nextEdges));
      })
      .catch((err) => {
        setError(err.message || 'Failed to load call graph');
        setDefinitions([]);
        setEdges([]);
        setFocusedNodeId(null);
      })
      .finally(() => setLoading(false));
  };

  const setMode = (isReverse: boolean) => {
    setReverse(isReverse);
  };

  const clearFocus = () => setFocusedNodeId(null);

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
            onInput={(e) => setSymbol((e.currentTarget as HTMLInputElement).value)}
            style={{ ...inputStyle, flex: '1', minWidth: '200px' }}
          />
          <input
            type="number"
            min={1}
            max={10}
            value={depth}
            onInput={(e) => setDepth(parseInt((e.currentTarget as HTMLInputElement).value, 10) || 3)}
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

          <span style={{ color: COLORS.muted, fontSize: '13px' }}>Depth: {depth}</span>

          {focusedNodeId && (
            <button
              type="button"
              onClick={clearFocus}
              style={{
                background: COLORS.surface,
                color: COLORS.text,
                border: `1px solid ${COLORS.border}`,
                borderRadius: '6px',
                padding: '6px 10px',
                cursor: 'pointer',
                fontSize: '12px',
              }}
            >
              Clear focus
            </button>
          )}
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
          <div
            style={{
              border: `1px solid ${COLORS.border}`,
              borderRadius: '10px',
              background: COLORS.surface,
              overflowX: 'auto',
              marginBottom: '14px',
            }}
          >
            <svg
              width={layout.width}
              height={layout.height}
              viewBox={`0 0 ${layout.width} ${layout.height}`}
              role="img"
              aria-label="Interactive call graph"
              style={{ display: 'block', minWidth: '100%', background: COLORS.bg }}
            >
              <defs>
                <marker
                  id="callgraph-arrow-default"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="8"
                  markerHeight="8"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill={COLORS.edgeDefault} />
                </marker>
                <marker
                  id="callgraph-arrow-dim"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="8"
                  markerHeight="8"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill={COLORS.edgeDim} />
                </marker>
                <marker
                  id="callgraph-arrow-inbound"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="8"
                  markerHeight="8"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill={COLORS.edgeInbound} />
                </marker>
                <marker
                  id="callgraph-arrow-outbound"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="8"
                  markerHeight="8"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill={COLORS.edgeOutbound} />
                </marker>
              </defs>

              {graphData.edges.map((edge) => {
                const source = nodesById.get(edge.source);
                const target = nodesById.get(edge.target);
                if (!source || !target) return null;

                let styleKind: 'default' | 'inbound' | 'outbound' | 'dim' = 'default';
                if (focusedNodeId) {
                  if (edge.source === focusedNodeId) styleKind = 'outbound';
                  else if (edge.target === focusedNodeId) styleKind = 'inbound';
                  else styleKind = 'dim';
                }

                const strokeColor =
                  styleKind === 'outbound'
                    ? COLORS.edgeOutbound
                    : styleKind === 'inbound'
                      ? COLORS.edgeInbound
                      : styleKind === 'dim'
                        ? COLORS.edgeDim
                        : COLORS.edgeDefault;

                return (
                  <g key={edge.id}>
                    <path
                      d={edgePath(source, target)}
                      fill="none"
                      stroke={strokeColor}
                      strokeWidth={styleKind === 'dim' ? 1.2 : 1.8}
                      opacity={styleKind === 'dim' ? 0.35 : 0.9}
                      markerEnd={`url(#${markerIdForEdge(styleKind)})`}
                    />
                    {edge.count && edge.count > 1 && (
                      <text
                        x={(source.x + target.x) / 2}
                        y={(source.y + target.y) / 2 - 6}
                        textAnchor="middle"
                        fontSize="10"
                        fill={COLORS.muted}
                      >
                        {edge.count}x
                      </text>
                    )}
                  </g>
                );
              })}

              {layout.nodes.map((node) => {
                const isFocused = focusedNodeId === node.id;
                const isInbound = focusInfo.inbound.has(node.id);
                const isOutbound = focusInfo.outbound.has(node.id);
                const isRelated = isFocused || isInbound || isOutbound;
                const shouldDim = focusedNodeId != null && !isRelated;

                const fill = isFocused
                  ? COLORS.active
                  : isRelated
                    ? COLORS.nodeRelated
                    : shouldDim
                      ? COLORS.nodeDim
                      : COLORS.nodeFill;

                const stroke = isFocused ? '#9ecbff' : COLORS.nodeStroke;

                return (
                  <g
                    key={node.id}
                    transform={`translate(${node.x}, ${node.y})`}
                    role="button"
                    tabIndex={0}
                    style={{ cursor: 'pointer' }}
                    onClick={() => setFocusedNodeId((prev) => (prev === node.id ? null : node.id))}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        setFocusedNodeId((prev) => (prev === node.id ? null : node.id));
                      }
                    }}
                  >
                    <title>{`${node.name}\n${node.file}`}</title>
                    <rect
                      x={-NODE_WIDTH / 2}
                      y={-NODE_HEIGHT / 2}
                      width={NODE_WIDTH}
                      height={NODE_HEIGHT}
                      rx={10}
                      ry={10}
                      fill={fill}
                      stroke={stroke}
                      strokeWidth={isFocused ? 2.2 : 1.2}
                      opacity={shouldDim ? 0.55 : 1}
                    />
                    <text
                      x="0"
                      y="-2"
                      textAnchor="middle"
                      fill={COLORS.heading}
                      fontSize="12"
                      fontWeight="600"
                    >
                      {truncateLabel(node.name)}
                    </text>
                    <text
                      x="0"
                      y="13"
                      textAnchor="middle"
                      fill={kindColor(node.kind)}
                      fontSize="10"
                    >
                      {truncateLabel(node.file, 24)}
                    </text>
                  </g>
                );
              })}
            </svg>
          </div>

          <div
            style={{
              display: 'flex',
              gap: '14px',
              flexWrap: 'wrap',
              fontSize: '12px',
              marginBottom: '16px',
              color: COLORS.muted,
            }}
          >
            <span>
              <span style={{ color: COLORS.edgeOutbound, fontWeight: 700 }}>Outbound</span> edges from focused node
            </span>
            <span>
              <span style={{ color: COLORS.edgeInbound, fontWeight: 700 }}>Inbound</span> edges into focused node
            </span>
            <span>Click a node to focus or unfocus</span>
          </div>

          <div
            style={{
              border: `1px solid ${COLORS.border}`,
              borderRadius: '8px',
              padding: '12px 14px',
              background: COLORS.surface,
            }}
          >
            <h2 style={{ fontSize: '15px', color: COLORS.heading, margin: '0 0 8px 0' }}>Text fallback</h2>

            {definitions.length > 0 && (
              <div style={{ marginBottom: '10px' }}>
                <div style={{ color: COLORS.muted, fontSize: '12px', marginBottom: '4px' }}>
                  Definitions ({definitions.length})
                </div>
                <ul style={{ margin: 0, paddingLeft: '18px' }}>
                  {definitions.map((def, idx) => (
                    <li key={def.id || `${def.name}-${def.file}-${idx}`} style={{ marginBottom: '2px' }}>
                      <a
                        href={`/${owner}/${repo}/blob/${gitRef}/${def.file}`}
                        style={{ color: COLORS.link, textDecoration: 'none' }}
                      >
                        {def.name}
                      </a>{' '}
                      <span style={{ color: COLORS.muted }}>({def.kind}, {def.file})</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {graphData.edges.length > 0 && (
              <div>
                <div style={{ color: COLORS.muted, fontSize: '12px', marginBottom: '4px' }}>
                  Directed edges ({graphData.edges.length})
                </div>
                <ul style={{ margin: 0, paddingLeft: '18px' }}>
                  {graphData.edges.map((edge, idx) => {
                    const source = nodesById.get(edge.source);
                    const target = nodesById.get(edge.target);
                    if (!source || !target) return null;

                    return (
                      <li key={`${edge.id}-text-${idx}`} style={{ marginBottom: '2px' }}>
                        <a
                          href={`/${owner}/${repo}/blob/${gitRef}/${source.file}`}
                          style={{ color: COLORS.link, textDecoration: 'none' }}
                        >
                          {source.name}
                        </a>{' '}
                        <span style={{ color: COLORS.muted }}>({source.file})</span>
                        <span style={{ color: COLORS.muted }}> {'\u2192'} </span>
                        <a
                          href={`/${owner}/${repo}/blob/${gitRef}/${target.file}`}
                          style={{ color: COLORS.link, textDecoration: 'none' }}
                        >
                          {target.name}
                        </a>{' '}
                        <span style={{ color: COLORS.muted }}>({target.file})</span>
                      </li>
                    );
                  })}
                </ul>
              </div>
            )}
          </div>
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
