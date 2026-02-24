import { useState, useEffect } from 'preact/hooks';
import { getToken, setToken, getUnreadNotificationsCount } from '../api/client';

const linkStyle = { color: '#c9d1d9', fontSize: '14px', textDecoration: 'none' };

export function Header() {
  const loggedIn = !!getToken();
  const [unread, setUnread] = useState(0);

  useEffect(() => {
    if (!loggedIn) return;
    getUnreadNotificationsCount().then(r => setUnread(r.count)).catch(() => setUnread(0));
  }, [loggedIn]);

  return (
    <header style={{ background: '#161b22', borderBottom: '1px solid #30363d', padding: '12px 20px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
      <a href="/" style={{ fontSize: '20px', fontWeight: 'bold', color: '#f0f6fc' }}>gothub</a>
      <nav style={{ display: 'flex', gap: '16px', alignItems: 'center' }}>
        {loggedIn ? (
          <>
            <a href="/notifications" style={linkStyle}>
              Notifications{unread > 0 && <span style={{ background: '#1f6feb', color: '#fff', borderRadius: '10px', padding: '1px 6px', fontSize: '11px', marginLeft: '4px' }}>{unread}</span>}
            </a>
            <a href="/settings" style={linkStyle}>Settings</a>
            <button onClick={() => { setToken(null); location.reload(); }}
              style={{ background: 'none', border: '1px solid #30363d', color: '#c9d1d9', padding: '6px 12px', borderRadius: '6px', cursor: 'pointer' }}>
              Sign out
            </button>
          </>
        ) : (
          <a href="/" style={{ color: '#c9d1d9' }}>Sign in</a>
        )}
      </nav>
    </header>
  );
}
