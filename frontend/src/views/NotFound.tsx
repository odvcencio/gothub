import { Home } from './Home';

interface Props {
  path?: string;
  default?: boolean;
}

export function NotFoundView({ path }: Props) {
  if (path === '/login') {
    return <Home />;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', minHeight: '60vh', textAlign: 'center' }}>
      <h1 style={{ fontSize: '72px', color: '#f0f6fc', margin: '0 0 8px 0', fontWeight: 'bold' }}>404</h1>
      <h2 style={{ fontSize: '24px', color: '#8b949e', margin: '0 0 16px 0', fontWeight: 'normal' }}>Page not found</h2>
      <p style={{ color: '#8b949e', fontSize: '16px', margin: '0 0 24px 0', maxWidth: '400px' }}>
        The page you're looking for doesn't exist or has been moved.
      </p>
      <a
        href="/"
        style={{
          color: '#58a6ff',
          fontSize: '14px',
          textDecoration: 'none',
        }}
      >
        Go to dashboard
      </a>
    </div>
  );
}
