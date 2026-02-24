import { useState, useEffect } from 'preact/hooks';
import { login, register, setToken, getToken, listUserRepos, createRepo } from '../api/client';

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
  const [error, setError] = useState('');

  const submit = async (e: Event) => {
    e.preventDefault();
    setError('');
    try {
      const res = mode === 'login'
        ? await login(username, password)
        : await register(username, email, password);
      setToken(res.token);
      location.reload();
    } catch (err: any) {
      setError(err.message);
    }
  };

  return (
    <div style={{ maxWidth: '400px', margin: '60px auto' }}>
      <h1 style={{ fontSize: '24px', marginBottom: '24px', color: '#f0f6fc' }}>
        {mode === 'login' ? 'Sign in to gothub' : 'Create an account'}
      </h1>
      {error && <div style={{ color: '#f85149', marginBottom: '16px', padding: '12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px' }}>{error}</div>}
      <form onSubmit={submit} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username"
          style={inputStyle} />
        {mode === 'register' && (
          <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email"
            style={inputStyle} />
        )}
        <input value={password} onInput={(e: any) => setPassword(e.target.value)} placeholder="Password" type="password"
          style={inputStyle} />
        <button type="submit" style={{ background: '#238636', color: '#fff', border: 'none', padding: '10px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '14px' }}>
          {mode === 'login' ? 'Sign in' : 'Create account'}
        </button>
      </form>
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

  useEffect(() => { listUserRepos().then(setRepos).catch(() => {}); }, []);

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
