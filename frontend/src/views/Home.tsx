import { useState, useEffect } from 'preact/hooks';
import {
  login,
  register,
  requestMagicLink,
  verifyMagicLink,
  beginWebAuthnLogin,
  finishWebAuthnLogin,
  setToken,
  getToken,
  listUserRepos,
  createRepo,
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
  const [info, setInfo] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const sessionExpired = typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('session') === 'expired';
  const passkeysAvailable = browserSupportsPasskeys();

  const completeAuth = (tokenValue: string) => {
    setToken(tokenValue);
    if (typeof window !== 'undefined') {
      window.history.replaceState(null, '', '/');
      window.location.reload();
    }
  };

  const submitLegacy = async (e: Event) => {
    e.preventDefault();
    setError('');
    setInfo('');
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
              {passkeysAvailable ? 'Sign in with passkey' : 'Passkeys unavailable in this browser'}
            </button>
          </form>

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
        </div>
      ) : (
        <form onSubmit={submitLegacy} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
          <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email" style={inputStyle} />
          <input value={password} onInput={(e: any) => setPassword(e.target.value)} placeholder="Password (optional)" type="password" style={inputStyle} />
          <button type="submit" disabled={submitting || !username || !email} style={{ ...primaryButtonStyle, opacity: submitting || !username || !email ? 0.6 : 1 }}>
            Create account
          </button>
          <p style={{ color: '#8b949e', margin: 0, fontSize: '12px' }}>
            Leave password blank for a passwordless account, then add a passkey in Settings.
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
  const [repos, setRepos] = useState<any[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [priv, setPriv] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    listUserRepos()
      .then(setRepos)
      .catch((e: any) => setError(e.message || 'failed to load repositories'));
  }, []);

  const handleCreate = async (e: Event) => {
    e.preventDefault();
    setError('');
    try {
      await createRepo(name, desc, priv);
      setShowCreate(false);
      setName(''); setDesc('');
      listUserRepos().then(setRepos);
    } catch (err: any) {
      setError(err.message);
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2 style={{ fontSize: '20px', color: '#f0f6fc' }}>Your repositories</h2>
        <button onClick={() => setShowCreate(!showCreate)}
          style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold' }}>
          New
        </button>
      </div>

      {showCreate && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', marginBottom: '20px' }}>
          {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}
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
          {repos.map(r => (
            <a key={r.id} href={`/${r.owner_name || r.name}/${r.name}`}
              style={{ display: 'block', padding: '12px 16px', borderBottom: '1px solid #21262d', color: '#58a6ff', fontWeight: 'bold' }}>
              {r.owner_name && <span style={{ color: '#8b949e', fontWeight: 'normal' }}>{r.owner_name}/</span>}
              {r.name}
              {r.description && <span style={{ color: '#8b949e', fontWeight: 'normal', marginLeft: '12px', fontSize: '13px' }}>{r.description}</span>}
              {r.is_private && <span style={{ color: '#8b949e', fontWeight: 'normal', marginLeft: '8px', fontSize: '11px', border: '1px solid #30363d', padding: '1px 6px', borderRadius: '12px' }}>Private</span>}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}

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
