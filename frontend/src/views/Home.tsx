import { useState, useEffect } from 'preact/hooks';
import {
  login,
  register,
  requestMagicLink,
  verifyMagicLink,
  beginWebAuthnLogin,
  finishWebAuthnLogin,
  getAuthCapabilities,
  setToken,
  getToken,
  listUserRepos,
  createRepo,
  getRepo,
  listPRs,
  listTree,
  searchSymbols,
  type PullRequest,
  type Repository,
  type TreeEntry,
} from '../api/client';
import { browserSupportsPasskeys, getPasskeyAssertion } from '../lib/webauthn';

export function Home() {
  const loggedIn = !!getToken();

  if (loggedIn) return <Dashboard />;
  return <AuthForm />;
}

function AuthForm() {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [magicToken, setMagicToken] = useState('');
  const [magicSent, setMagicSent] = useState(false);
  const [authCapabilities, setAuthCapabilities] = useState({
    passwordAuthEnabled: false,
    magicLinkEnabled: true,
    passkeyEnabled: true,
  });
  const [info, setInfo] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const sessionExpired = typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('session') === 'expired';
  const passkeysAvailable = browserSupportsPasskeys() && authCapabilities.passkeyEnabled;
  const passwordAuthEnabled = authCapabilities.passwordAuthEnabled;
  const passkeyLabel = !authCapabilities.passkeyEnabled
    ? 'Passkeys disabled by server'
    : passkeysAvailable
      ? 'Sign in with passkey'
      : 'Passkeys unavailable in this browser';

  useEffect(() => {
    getAuthCapabilities()
      .then((caps) =>
        setAuthCapabilities({
          passwordAuthEnabled: !!caps.password_auth_enabled,
          magicLinkEnabled: !!caps.magic_link_enabled,
          passkeyEnabled: !!caps.passkey_enabled,
        })
      )
      .catch((e: any) => {
        setAuthCapabilities({
          passwordAuthEnabled: false,
          magicLinkEnabled: true,
          passkeyEnabled: true,
        });
        setNotice(e?.message || 'Some sign-in methods could not be verified. Showing fallback options.');
      });
  }, []);

  const completeAuth = (tokenValue: string) => {
    setToken(tokenValue);
    if (typeof window !== 'undefined') {
      window.location.assign(resolvePostAuthRedirect());
    }
  };

  const submitLegacy = async (e: Event) => {
    e.preventDefault();
    setError('');
    setInfo('');
    setNotice('');
    setSubmitting(true);
    try {
      const res = mode === 'login'
        ? await login(username, password)
        : await register(username, email, password);
      completeAuth(res.token);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  const submitMagicRequest = async (e: Event) => {
    e.preventDefault();
    setError('');
    setInfo('');
    setNotice('');
    setSubmitting(true);
    try {
      const res = await requestMagicLink(email);
      setMagicSent(true);
      if (res.token) setMagicToken(res.token);
      setInfo(res.token
        ? 'Magic link token generated for local/dev mode. Verify below.'
        : 'Magic link sent. Check your inbox.');
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  const submitMagicVerify = async (e: Event) => {
    e.preventDefault();
    setError('');
    setInfo('');
    setNotice('');
    setSubmitting(true);
    try {
      const res = await verifyMagicLink(magicToken);
      completeAuth(res.token);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  const submitPasskey = async (e: Event) => {
    e.preventDefault();
    setError('');
    setInfo('');
    setNotice('');
    setSubmitting(true);
    try {
      const begin = await beginWebAuthnLogin(username);
      const credential = await getPasskeyAssertion(begin.options);
      const res = await finishWebAuthnLogin(begin.session_id, credential);
      completeAuth(res.token);
    } catch (err: any) {
      setError(err.message || 'passkey sign-in failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ maxWidth: '400px', margin: '60px auto' }}>
      <h1 style={{ fontSize: '24px', marginBottom: '24px', color: '#f0f6fc' }}>
        {mode === 'login' ? 'Sign in to gothub' : 'Create an account'}
      </h1>
      {sessionExpired && (
        <div style={{ color: '#f0f6fc', marginBottom: '16px', padding: '12px', background: '#1b2a42', border: '1px solid #1f6feb', borderRadius: '6px' }}>
          Session expired. Sign in again.
        </div>
      )}
      {notice && <div style={{ color: '#d29922', marginBottom: '16px', padding: '12px', background: '#2b230f', border: '1px solid #d29922', borderRadius: '6px' }}>{notice}</div>}
      {info && <div style={{ color: '#3fb950', marginBottom: '16px', padding: '12px', background: '#132a1d', border: '1px solid #3fb950', borderRadius: '6px' }}>{info}</div>}
      {error && <div style={{ color: '#f85149', marginBottom: '16px', padding: '12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px' }}>{error}</div>}
      {mode === 'login' ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
          <form onSubmit={submitPasskey} style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
            <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
            <button
              type="submit"
              disabled={submitting || !username || !passkeysAvailable}
              style={{ ...primaryButtonStyle, opacity: submitting || !username || !passkeysAvailable ? 0.6 : 1 }}
            >
              {passkeyLabel}
            </button>
          </form>

          {authCapabilities.magicLinkEnabled && (
            <div style={{ borderTop: '1px solid #30363d', paddingTop: '14px' }}>
              <form onSubmit={submitMagicRequest} style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
                <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email" style={inputStyle} />
                <button type="submit" disabled={submitting || !email} style={{ ...secondaryButtonStyle, opacity: submitting || !email ? 0.6 : 1 }}>
                  Send magic link
                </button>
              </form>
              {magicSent && (
                <form onSubmit={submitMagicVerify} style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginTop: '10px' }}>
                  <input value={magicToken} onInput={(e: any) => setMagicToken(e.target.value)} placeholder="Magic token" style={inputStyle} />
                  <button type="submit" disabled={submitting || !magicToken} style={{ ...secondaryButtonStyle, opacity: submitting || !magicToken ? 0.6 : 1 }}>
                    Verify magic link
                  </button>
                </form>
              )}
            </div>
          )}

          {passwordAuthEnabled && (
            <details style={{ borderTop: '1px solid #30363d', paddingTop: '14px' }}>
              <summary style={{ color: '#8b949e', cursor: 'pointer', fontSize: '13px' }}>Legacy password sign-in</summary>
              <form onSubmit={submitLegacy} style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginTop: '10px' }}>
                <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
                <input value={password} onInput={(e: any) => setPassword(e.target.value)} placeholder="Password" type="password" style={inputStyle} />
                <button type="submit" disabled={submitting || !username || !password} style={{ ...secondaryButtonStyle, opacity: submitting || !username || !password ? 0.6 : 1 }}>
                  Sign in with password
                </button>
              </form>
            </details>
          )}
        </div>
      ) : (
        <form onSubmit={submitLegacy} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
          <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email" style={inputStyle} />
          {passwordAuthEnabled && (
            <input value={password} onInput={(e: any) => setPassword(e.target.value)} placeholder="Password (optional)" type="password" style={inputStyle} />
          )}
          <button type="submit" disabled={submitting || !username || !email || (!passwordAuthEnabled && !!password)} style={{ ...primaryButtonStyle, opacity: submitting || !username || !email ? 0.6 : 1 }}>
            Create account
          </button>
          <p style={{ color: '#8b949e', margin: 0, fontSize: '12px' }}>
            {passwordAuthEnabled
              ? 'Leave password blank for a passwordless account, then add a passkey in Settings.'
              : 'Password auth is disabled on this instance. Accounts are passwordless by default.'}
          </p>
        </form>
      )}
      <p style={{ marginTop: '16px', color: '#8b949e', fontSize: '13px' }}>
        {mode === 'login' ? (
          <span>New to gothub? <a href="#" onClick={(e) => { e.preventDefault(); setMode('register'); }} style={{ color: '#58a6ff' }}>Create an account</a></span>
        ) : (
          <span>Already have an account? <a href="#" onClick={(e) => { e.preventDefault(); setMode('login'); }} style={{ color: '#58a6ff' }}>Sign in</a></span>
        )}
      </p>
    </div>
  );
}

function Dashboard() {
  const [repos, setRepos] = useState<Repository[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [priv, setPriv] = useState(false);
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
    listUserRepos()
      .then((items) => {
        setRepos(items);
        setError('');
      })
      .catch((e: any) => setError(e.message || 'failed to load repositories'));
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

  const handleCreate = async (e: Event) => {
    e.preventDefault();
    setError('');
    try {
      await createRepo(name, desc, priv);
      setShowCreate(false);
      setName(''); setDesc('');
      listUserRepos()
        .then((items) => {
          setRepos(items);
          setError('');
        })
        .catch((err: any) => setError(err?.message || 'repository created but failed to refresh repository list'));
    } catch (err: any) {
      setError(err.message);
    }
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
          <button
            onClick={() => setShowCreate(!showCreate)}
            style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold' }}
          >
            New
          </button>
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

      {showCreate && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', marginBottom: '20px' }}>
          <form onSubmit={handleCreate} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <input value={name} onInput={(e: any) => setName(e.target.value)} placeholder="Repository name" style={inputStyle} />
            <input value={desc} onInput={(e: any) => setDesc(e.target.value)} placeholder="Description (optional)" style={inputStyle} />
            <label style={{ color: '#c9d1d9', fontSize: '13px', display: 'flex', alignItems: 'center', gap: '8px' }}>
              <input type="checkbox" checked={priv} onChange={(e: any) => setPriv(e.target.checked)} /> Private
            </label>
            <button type="submit" style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', alignSelf: 'flex-start' }}>
              Create repository
            </button>
          </form>
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

function resolvePostAuthRedirect(): string {
  if (typeof window === 'undefined') return '/';

  const params = new URLSearchParams(window.location.search);
  const returnTo = params.get('returnTo') || '/';

  if (!returnTo.startsWith('/') || returnTo.startsWith('//') || returnTo.startsWith('/login')) {
    return '/';
  }
  return returnTo;
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

const inputStyle = {
  background: '#0d1117',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  color: '#c9d1d9',
  fontSize: '14px',
};

const primaryButtonStyle = {
  background: '#238636',
  color: '#fff',
  border: 'none',
  padding: '10px',
  borderRadius: '6px',
  cursor: 'pointer',
  fontWeight: 'bold',
  fontSize: '14px',
};

const secondaryButtonStyle = {
  background: '#21262d',
  color: '#c9d1d9',
  border: '1px solid #30363d',
  padding: '10px',
  borderRadius: '6px',
  cursor: 'pointer',
  fontWeight: 'bold',
  fontSize: '14px',
};
