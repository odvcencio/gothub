import { useState } from 'preact/hooks';
import { register, setToken } from '../api/client';

interface Props {
  path?: string;
}

export function SignupView({ path }: Props) {
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (e: Event) => {
    e.preventDefault();
    setError(''); setSubmitting(true);
    try {
      const res = await register(username, email);
      setToken(res.token);
      window.location.assign('/');
    } catch (err: any) {
      setError(err.message);
    } finally { setSubmitting(false); }
  };

  return (
    <div style={{ maxWidth: '400px', margin: '60px auto', padding: '0 16px' }}>
      <h1 style={{ fontSize: '24px', marginBottom: '24px', color: '#f0f6fc' }}>Create your account</h1>
      {error && <div style={{ color: '#f85149', marginBottom: '16px', padding: '12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px' }}>{error}</div>}
      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
        <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email" style={inputStyle} />
        <button type="submit" disabled={submitting || !username || !email}
          style={{ ...btnPrimary, opacity: submitting || !username || !email ? 0.6 : 1 }}>
          Create account
        </button>
        <p style={{ color: '#8b949e', margin: 0, fontSize: '12px' }}>
          After creating your account, add a passkey in Settings for fast sign-in.
        </p>
      </form>
      <p style={{ marginTop: '16px', color: '#8b949e', fontSize: '13px' }}>
        Already have an account? <a href="/login" style={{ color: '#58a6ff' }}>Sign in</a>
      </p>
    </div>
  );
}

const inputStyle: Record<string, string> = { background: '#0d1117', border: '1px solid #30363d', borderRadius: '6px', padding: '10px 12px', color: '#c9d1d9', fontSize: '14px', width: '100%', boxSizing: 'border-box' };
const btnPrimary: Record<string, string> = { background: '#238636', color: '#fff', border: 'none', padding: '10px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '14px', width: '100%' };
