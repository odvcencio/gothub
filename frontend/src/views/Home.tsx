import { useState, useEffect } from 'preact/hooks';
import {
  getToken,
  subscribeAuthTokenChange,
  listUserRepos,
  getRepo,
  listPRs,
  listTree,
  searchSymbols,
  type PullRequest,
  type Repository,
  type TreeEntry,
} from '../api/client';
import { Landing } from './Landing';

export function Home() {
  const [authToken, setAuthToken] = useState<string | null>(() => getToken());

  useEffect(() => subscribeAuthTokenChange(() => setAuthToken(getToken())), []);

  const loggedIn = !!authToken;

  if (loggedIn) return <Dashboard />;
  return <Landing />;
}

function Dashboard() {
  const [repos, setRepos] = useState<Repository[]>([]);
  const [error, setError] = useState('');
  const [showOnboarding, setShowOnboarding] = useState(() => !hasSeenOnboardingDemo());
  const [demo, setDemo] = useState<DemoContext | null>(null);
  const [demoSteps, setDemoSteps] = useState<DemoStep[]>([]);
  const [demoStepIndex, setDemoStepIndex] = useState(0);
  const [demoLoading, setDemoLoading] = useState(false);
  const [demoError, setDemoError] = useState('');
  const [demoNotice, setDemoNotice] = useState('');
  const appendDemoNotice = (message: string) => {
    const next = message.trim();
    if (!next) return;
    setDemoNotice((current) => {
      if (!current) return next;
      if (current.includes(next)) return current;
      return `${current} • ${next}`;
    });
  };

  useEffect(() => {
    const loadDashboard = async () => {
      try {
        const items = await listUserRepos();
        setRepos(Array.isArray(items) ? items : []);
        setError('');
      } catch (e: any) {
        setError(e.message || 'failed to load dashboard');
      }
    };
    loadDashboard();
  }, []);

  const startDemo = async () => {
    if (demoLoading) return;
    setShowOnboarding(true);
    setDemoError('');
    setDemoNotice('');
    setDemoLoading(true);
    try {
      const context = await resolveDemoContext(repos, appendDemoNotice);
      if (!context) {
        setDemo(null);
        setDemoSteps([]);
        setDemoError('Create or fork a repository first, then run Try Demo again.');
        return;
      }
      setDemo(context);
      setDemoSteps(buildDemoSteps(context));
      setDemoStepIndex(0);
      markOnboardingDemoSeen();
    } catch (err: any) {
      setDemoError(err.message || 'failed to prepare onboarding demo');
    } finally {
      setDemoLoading(false);
    }
  };

  const hideOnboarding = () => {
    setShowOnboarding(false);
    markOnboardingDemoSeen();
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2 style={{ fontSize: '20px', color: '#f0f6fc' }}>Your repositories</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <button
            onClick={startDemo}
            disabled={demoLoading}
            style={{ background: '#1f6feb', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: demoLoading ? 'not-allowed' : 'pointer', fontWeight: 'bold', opacity: demoLoading ? 0.7 : 1 }}
          >
            {demoLoading ? 'Preparing demo...' : 'Try Demo'}
          </button>
          <a href="/new" style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', fontWeight: 'bold', textDecoration: 'none', display: 'inline-block' }}>New</a>
        </div>
      </div>
      {error && (
        <div style={{ color: '#f85149', marginBottom: '12px', padding: '10px 12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px', fontSize: '13px' }}>
          {error}
        </div>
      )}

      {showOnboarding && (
        <div style={{ border: '1px solid #1f6feb', borderRadius: '8px', padding: '16px', marginBottom: '20px', background: '#111b2e' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '12px', flexWrap: 'wrap' }}>
            <div>
              <div style={{ color: '#58a6ff', fontSize: '12px', textTransform: 'uppercase', letterSpacing: '0.04em', marginBottom: '4px', fontWeight: 'bold' }}>First-run guide</div>
              <h3 style={{ color: '#f0f6fc', margin: 0, fontSize: '18px' }}>Walk structural review in 4 steps</h3>
              <p style={{ color: '#8b949e', margin: '8px 0 0', fontSize: '13px' }}>
                Jump to PR impact, semver recommendations, call graph navigation, and structural blame with one curated flow.
              </p>
              {demo && (
                <p style={{ color: '#8b949e', margin: '8px 0 0', fontSize: '12px' }}>
                  Using <a href={`/${demo.owner}/${demo.repo}`} style={{ color: '#58a6ff' }}>{demo.owner}/{demo.repo}</a> on <code style={{ background: '#0d1117', borderRadius: '4px', padding: '2px 4px' }}>{demo.ref}</code>
                </p>
              )}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <button
                onClick={startDemo}
                disabled={demoLoading}
                style={{ background: '#1f6feb', color: '#fff', border: 'none', padding: '7px 14px', borderRadius: '6px', cursor: demoLoading ? 'not-allowed' : 'pointer', fontWeight: 'bold', opacity: demoLoading ? 0.7 : 1 }}
              >
                {demoLoading ? 'Loading...' : demoSteps.length > 0 ? 'Refresh path' : 'Try Demo'}
              </button>
              <button
                onClick={hideOnboarding}
                style={{ background: 'transparent', color: '#8b949e', border: '1px solid #30363d', padding: '7px 12px', borderRadius: '6px', cursor: 'pointer', fontSize: '13px' }}
              >
                Close
              </button>
            </div>
          </div>

          {demoError && (
            <div style={{ marginTop: '12px', color: '#f85149', padding: '10px 12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px', fontSize: '13px' }}>
              {demoError}
            </div>
          )}
          {demoNotice && (
            <div style={{ marginTop: '12px', color: '#d29922', padding: '10px 12px', background: '#2b230f', border: '1px solid #d29922', borderRadius: '6px', fontSize: '13px' }}>
              {demoNotice}
            </div>
          )}

          {demoSteps.length > 0 && !demoLoading && (
            <div style={{ marginTop: '14px', display: 'grid', gap: '10px' }}>
              <div style={{ color: '#8b949e', fontSize: '12px' }}>Step {demoStepIndex + 1} of {demoSteps.length}</div>
              <div style={{ border: '1px solid #30363d', borderRadius: '6px', background: '#0d1117', padding: '12px' }}>
                <div style={{ color: '#f0f6fc', fontSize: '14px', fontWeight: 'bold', marginBottom: '6px' }}>
                  {demoSteps[demoStepIndex].title}
                </div>
                <div style={{ color: '#8b949e', fontSize: '13px', marginBottom: '10px' }}>
                  {demoSteps[demoStepIndex].description}
                </div>
                {demoSteps[demoStepIndex].hint && (
                  <div style={{ color: '#d29922', fontSize: '12px', marginBottom: '10px' }}>
                    {demoSteps[demoStepIndex].hint}
                  </div>
                )}
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                  <a href={demoSteps[demoStepIndex].href} style={demoActionLinkStyle}>
                    {demoSteps[demoStepIndex].cta}
                  </a>
                  <button
                    onClick={() => setDemoStepIndex((idx) => Math.max(0, idx - 1))}
                    disabled={demoStepIndex === 0}
                    style={{ ...demoNavButtonStyle, opacity: demoStepIndex === 0 ? 0.6 : 1 }}
                  >
                    Previous
                  </button>
                  <button
                    onClick={() => setDemoStepIndex((idx) => Math.min(demoSteps.length - 1, idx + 1))}
                    disabled={demoStepIndex >= demoSteps.length - 1}
                    style={{ ...demoNavButtonStyle, opacity: demoStepIndex >= demoSteps.length - 1 ? 0.6 : 1 }}
                  >
                    Next
                  </button>
                </div>
              </div>
              <div style={{ display: 'grid', gap: '6px' }}>
                {demoSteps.map((step, idx) => (
                  <button
                    key={step.title}
                    onClick={() => setDemoStepIndex(idx)}
                    style={{
                      ...demoStepButtonStyle,
                      borderColor: idx === demoStepIndex ? '#1f6feb' : '#30363d',
                      color: idx === demoStepIndex ? '#f0f6fc' : '#8b949e',
                      background: idx === demoStepIndex ? '#161b22' : '#0d1117',
                    }}
                  >
                    {idx + 1}. {step.title}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {repos.length === 0 ? (
        <div style={{ color: '#8b949e', padding: '40px 0', textAlign: 'center' }}>No repositories yet</div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {repos.map((r, idx) => {
            const repoOwner = getRepoOwner(r);
            const repoHref = repoOwner ? `/${repoOwner}/${r.name}` : `/${r.name}`;

            return (
              <div
                key={r.id}
                style={{
                  display: 'block',
                  padding: '12px 16px',
                  borderBottom: idx === repos.length - 1 ? 'none' : '1px solid #21262d',
                }}
              >
                <div style={{ marginBottom: '6px' }}>
                  <a href={repoHref} style={{ color: '#58a6ff', fontWeight: 'bold', textDecoration: 'none' }}>
                    {repoOwner && <span style={{ color: '#8b949e', fontWeight: 'normal' }}>{repoOwner}/</span>}
                    {r.name}
                  </a>
                  {r.is_private && (
                    <span style={{ color: '#8b949e', fontWeight: 'normal', marginLeft: '8px', fontSize: '11px', border: '1px solid #30363d', padding: '1px 6px', borderRadius: '12px' }}>
                      Private
                    </span>
                  )}
                </div>

                {r.description && (
                  <div style={{ color: '#8b949e', fontWeight: 'normal', fontSize: '13px' }}>
                    {r.description}
                  </div>
                )}

                {r.parent_owner && r.parent_name && (
                  <div style={{ color: '#8b949e', fontWeight: 'normal', marginTop: '4px', fontSize: '13px' }}>
                    forked from{' '}
                    <a href={`/${r.parent_owner}/${r.parent_name}`} style={{ color: '#58a6ff', textDecoration: 'none' }}>
                      {r.parent_owner}/{r.parent_name}
                    </a>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

type DemoContext = {
  owner: string;
  repo: string;
  ref: string;
  prNumber?: number;
  symbol?: string;
  filePath?: string;
};

type DemoStep = {
  title: string;
  description: string;
  href: string;
  cta: string;
  hint?: string;
};

const ONBOARDING_DEMO_KEY = 'gothub_onboarding_demo_seen_v1';

function hasSeenOnboardingDemo(): boolean {
  if (typeof window === 'undefined') return false;
  return window.localStorage.getItem(ONBOARDING_DEMO_KEY) === '1';
}

function markOnboardingDemoSeen() {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(ONBOARDING_DEMO_KEY, '1');
}

async function resolveDemoContext(
  repos: Repository[],
  onNotice?: (message: string) => void,
): Promise<DemoContext | null> {
  const candidates = repos
    .map((repo) => {
      const owner = getRepoOwner(repo);
      if (!owner || !repo.name) return null;
      return { owner, repo: repo.name, defaultBranch: repo.default_branch || '' };
    })
    .filter((entry): entry is { owner: string; repo: string; defaultBranch: string } => entry !== null);

  if (candidates.length === 0) return null;

  const repoSample = candidates.slice(0, 6);
  let selected = repoSample[0];
  let selectedPR: PullRequest | null = null;

  for (const candidate of repoSample) {
    const pr = await findOnboardingPR(candidate.owner, candidate.repo, onNotice);
    if (pr) {
      selected = candidate;
      selectedPR = pr;
      break;
    }
  }

  let ref = selected.defaultBranch || 'main';
  if (!selected.defaultBranch) {
    try {
      const repo = await getRepo(selected.owner, selected.repo);
      if (repo.default_branch) ref = repo.default_branch;
    } catch (err: any) {
      onNotice?.(err?.message || `Failed to resolve default branch for ${selected.owner}/${selected.repo}; using main.`);
      ref = 'main';
    }
  }

  const [filePath, symbol] = await Promise.all([
    findOnboardingFile(selected.owner, selected.repo, ref, onNotice),
    findOnboardingSymbol(selected.owner, selected.repo, ref, onNotice),
  ]);

  return {
    owner: selected.owner,
    repo: selected.repo,
    ref,
    prNumber: selectedPR?.number,
    filePath: filePath || undefined,
    symbol: symbol || undefined,
  };
}

function buildDemoSteps(context: DemoContext): DemoStep[] {
  const prHref = context.prNumber
    ? `/${context.owner}/${context.repo}/pulls/${context.prNumber}`
    : `/${context.owner}/${context.repo}/pulls`;
  const callGraphHint = context.symbol
    ? `Use symbol "${context.symbol}" for a quick graph.`
    : 'Pick a function name from Symbols, then run it in Call Graph.';
  const blameHref = context.filePath
    ? `/${context.owner}/${context.repo}/blob/${context.ref}/${context.filePath}`
    : `/${context.owner}/${context.repo}/tree/${context.ref}`;
  const blameDescription = context.filePath
    ? `Open ${context.filePath}. The Structural Blame panel auto-selects the first entity and shows attribution.`
    : 'Open any source file and use the Structural Blame panel to inspect entity-level attribution.';

  return [
    {
      title: 'PR Impact',
      description: context.prNumber
        ? `Open PR #${context.prNumber}, switch to "Files changed", and inspect the PR Impact summary.`
        : 'Open pull requests, pick one PR, and inspect the PR Impact summary in "Files changed".',
      href: prHref,
      cta: context.prNumber ? `Open PR #${context.prNumber}` : 'Open pull requests',
    },
    {
      title: 'SemVer Recommendation',
      description: context.prNumber
        ? 'From that PR, switch to "Merge preview" to view structural SemVer recommendations.'
        : 'After opening a PR, switch to "Merge preview" to view structural SemVer recommendations.',
      href: prHref,
      cta: context.prNumber ? 'Open merge preview' : 'Open pull requests',
    },
    {
      title: 'Call Graph',
      description: 'Explore caller/callee relationships to quickly understand behavioral impact.',
      href: `/${context.owner}/${context.repo}/callgraph/${context.ref}`,
      cta: 'Open call graph',
      hint: callGraphHint,
    },
    {
      title: 'Structural Blame',
      description: blameDescription,
      href: blameHref,
      cta: context.filePath ? 'Open blame-ready file' : 'Open repository files',
    },
  ];
}

async function findOnboardingPR(
  owner: string,
  repo: string,
  onNotice?: (message: string) => void,
): Promise<PullRequest | null> {
  try {
    const open = await listPRs(owner, repo, 'open', 1, 10);
    if (open.length > 0) return open[0];
    const recent = await listPRs(owner, repo, undefined, 1, 10);
    return recent[0] || null;
  } catch (err: any) {
    onNotice?.(err?.message || `Failed to inspect pull requests for ${owner}/${repo}.`);
    return null;
  }
}

async function findOnboardingFile(
  owner: string,
  repo: string,
  ref: string,
  onNotice?: (message: string) => void,
): Promise<string | null> {
  const queue: string[] = [''];
  let scanned = 0;
  let warnedListTreeFailure = false;

  while (queue.length > 0 && scanned < 12) {
    const current = queue.shift() || '';
    let entries: TreeEntry[];
    try {
      entries = await listTree(owner, repo, ref, current || undefined);
    } catch (err: any) {
      if (!warnedListTreeFailure) {
        warnedListTreeFailure = true;
        onNotice?.(err?.message || `Failed to inspect files for ${owner}/${repo}; demo hints may be limited.`);
      }
      continue;
    }
    scanned++;

    const file = entries.find((entry) => !entry.is_dir);
    if (file) {
      return current ? `${current}/${file.name}` : file.name;
    }

    for (const entry of entries) {
      if (!entry.is_dir) continue;
      const next = current ? `${current}/${entry.name}` : entry.name;
      queue.push(next);
      if (queue.length >= 20) break;
    }
  }

  return null;
}

async function findOnboardingSymbol(
  owner: string,
  repo: string,
  ref: string,
  onNotice?: (message: string) => void,
): Promise<string | null> {
  try {
    const symbols = await searchSymbols(owner, repo, ref);
    if (!symbols || symbols.length === 0) return null;
    const preferred = symbols.find((sym) => {
      const kind = typeof sym.kind === 'string' ? sym.kind.toLowerCase() : '';
      return (kind === 'function' || kind === 'method') && typeof sym.name === 'string' && sym.name.trim().length > 0;
    });
    if (preferred?.name) return preferred.name;
    const fallback = symbols.find((sym) => typeof sym.name === 'string' && sym.name.trim().length > 0);
    return fallback?.name || null;
  } catch (err: any) {
    onNotice?.(err?.message || `Failed to inspect symbols for ${owner}/${repo}; call-graph hint may be limited.`);
    return null;
  }
}

function getRepoOwner(repo: Repository): string | null {
  if (typeof repo.owner_name === 'string' && repo.owner_name.trim()) return repo.owner_name;
  return null;
}

const demoActionLinkStyle = {
  display: 'inline-block',
  textDecoration: 'none',
  background: '#1f6feb',
  color: '#fff',
  border: 'none',
  borderRadius: '6px',
  padding: '8px 12px',
  fontWeight: 'bold',
  fontSize: '13px',
};

const demoNavButtonStyle = {
  background: '#21262d',
  color: '#c9d1d9',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '8px 12px',
  cursor: 'pointer',
  fontSize: '13px',
};

const demoStepButtonStyle = {
  textAlign: 'left' as const,
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '8px 10px',
  cursor: 'pointer',
  fontSize: '13px',
};

