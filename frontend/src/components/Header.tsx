import { getToken, setToken } from '../api/client';

export function Header() {
  const loggedIn = !!getToken();

  return (
    <header style={{ background: '#161b22', borderBottom: '1px solid #30363d', padding: '12px 20px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
      <a href="/" style={{ fontSize: '20px', fontWeight: 'bold', color: '#f0f6fc' }}>gothub</a>
      <nav style={{ display: 'flex', gap: '16px', alignItems: 'center' }}>
        {loggedIn ? (
          <button onClick={() => { setToken(null); location.reload(); }}
            style={{ background: 'none', border: '1px solid #30363d', color: '#c9d1d9', padding: '6px 12px', borderRadius: '6px', cursor: 'pointer' }}>
            Sign out
          </button>
        ) : (
          <a href="/" style={{ color: '#c9d1d9' }}>Sign in</a>
        )}
      </nav>
    </header>
  );
}
