import { useState, useEffect } from 'preact/hooks';
import {
  requestMagicLink,
  verifyMagicLink,
  beginWebAuthnLogin,
  finishWebAuthnLogin,
  getAuthCapabilities,
  setToken,
} from '../api/client';
import { browserSupportsPasskeys, getPasskeyAssertion } from '../lib/webauthn';

interface Props {
  path?: string;
}

export function LoginView({ path }: Props) {
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [magicToken, setMagicToken] = useState('');
  const [magicSent, setMagicSent] = useState(false);
  const [passkeyEnabled, setPasskeyEnabled] = useState(true);
  const [error, setError] = useState('');
  const [info, setInfo] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const passkeysAvailable = browserSupportsPasskeys() && passkeyEnabled;

  useEffect(() => {
    getAuthCapabilities()
      .then((caps) => setPasskeyEnabled(!!caps.passkey_enabled))
      .catch(() => setPasskeyEnabled(true));
  }, []);

  const sessionExpired = typeof window !== 'undefined' &&
    new URLSearchParams(window.location.search).get('session') === 'expired';

  const completeAuth = (token: string) => {
    setToken(token);
    const params = new URLSearchParams(window.location.search);
    const returnTo = params.get('returnTo') || '/';
    window.location.assign(returnTo.startsWith('/') && !returnTo.startsWith('//') ? returnTo : '/');
  };

  const submitPasskey = async (e: Event) => {
    e.preventDefault();
    setError(''); setInfo(''); setSubmitting(true);
    try {
      const begin = await beginWebAuthnLogin(username);
      const credential = await getPasskeyAssertion(begin.options);
      const res = await finishWebAuthnLogin(begin.session_id, credential);
      completeAuth(res.token);
    } catch (err: any) {
      setError(err.message || 'Passkey sign-in failed');
    } finally { setSubmitting(false); }
  };

  const submitMagicRequest = async (e: Event) => {
    e.preventDefault();
    setError(''); setInfo(''); setSubmitting(true);
    try {
      const res = await requestMagicLink(email);
      setMagicSent(true);
      if (res.token) setMagicToken(res.token);
      setInfo(res.token ? 'Magic link token generated (dev mode). Verify below.' : 'Check your inbox for the magic link.');
    } catch (err: any) {
      setError(err.message);
    } finally { setSubmitting(false); }
  };

  const submitMagicVerify = async (e: Event) => {
    e.preventDefault();
    setError(''); setInfo(''); setSubmitting(true);
    try {
      const res = await verifyMagicLink(magicToken);
      completeAuth(res.token);
    } catch (err: any) {
      setError(err.message);
    } finally { setSubmitting(false); }
  };

  return (
    <div style={{ maxWidth: '400px', margin: '60px auto', padding: '0 16px' }}>
      <h1 style={{ fontSize: '24px', marginBottom: '24px', color: '#f0f6fc' }}>Sign in to GotHub</h1>

      {sessionExpired && (
        <div style={{ color: '#f0f6fc', marginBottom: '16px', padding: '12px', background: '#1b2a42', border: '1px solid #1f6feb', borderRadius: '6px' }}>
          Session expired. Sign in again.
        </div>
      )}
      {info && <div style={{ color: '#3fb950', marginBottom: '16px', padding: '12px', background: '#132a1d', border: '1px solid #3fb950', borderRadius: '6px' }}>{info}</div>}
      {error && <div style={{ color: '#f85149', marginBottom: '16px', padding: '12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px' }}>{error}</div>}

      <form onSubmit={submitPasskey} style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
        <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
        <button type="submit" disabled={submitting || !username || !passkeysAvailable}
          style={{ ...btnPrimary, opacity: submitting || !username || !passkeysAvailable ? 0.6 : 1 }}>
          {passkeysAvailable ? 'Sign in with passkey' : 'Passkeys unavailable in this browser'}
        </button>
      </form>

      <div style={{ borderTop: '1px solid #30363d', marginTop: '14px', paddingTop: '14px' }}>
        <form onSubmit={submitMagicRequest} style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
          <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email" style={inputStyle} />
          <button type="submit" disabled={submitting || !email}
            style={{ ...btnSecondary, opacity: submitting || !email ? 0.6 : 1 }}>
            Send magic link
          </button>
        </form>
        {magicSent && (
          <form onSubmit={submitMagicVerify} style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginTop: '10px' }}>
            <input value={magicToken} onInput={(e: any) => setMagicToken(e.target.value)} placeholder="Magic token" style={inputStyle} />
            <button type="submit" disabled={submitting || !magicToken}
              style={{ ...btnSecondary, opacity: submitting || !magicToken ? 0.6 : 1 }}>
              Verify
            </button>
          </form>
        )}
      </div>

      <p style={{ marginTop: '16px', color: '#8b949e', fontSize: '13px' }}>
        New to GotHub? <a href="/signup" style={{ color: '#58a6ff' }}>Create an account</a>
      </p>
    </div>
  );
}

const inputStyle: Record<string, string> = { background: '#0d1117', border: '1px solid #30363d', borderRadius: '6px', padding: '10px 12px', color: '#c9d1d9', fontSize: '14px', width: '100%', boxSizing: 'border-box' };
const btnPrimary: Record<string, string> = { background: '#238636', color: '#fff', border: 'none', padding: '10px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '14px', width: '100%' };
const btnSecondary: Record<string, string> = { background: '#21262d', color: '#c9d1d9', border: '1px solid #30363d', padding: '10px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '14px', width: '100%' };
