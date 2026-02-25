import { useState, useEffect } from 'preact/hooks';
import {
  register,
  setToken,
  getAuthCapabilities,
  beginWebAuthnLogin,
  finishWebAuthnLogin,
  listExploreRepos,
  createInterestSignup,
  type Repository,
} from '../api/client';
import { browserSupportsPasskeys, getPasskeyAssertion } from '../lib/webauthn';

export function Landing() {
  return (
    <div>
      <HeroSection />
      <FeatureCards />
      <ExploreSection />
      <FooterCTA />
    </div>
  );
}

/* ── Hero ─────────────────────────────────────────────────────────────── */

function HeroSection() {
  return (
    <section style={{ display: 'flex', gap: '48px', alignItems: 'flex-start', flexWrap: 'wrap', padding: '48px 0 32px' }}>
      <div style={{ flex: '1 1 400px', minWidth: '300px' }}>
        <h1 style={{ fontSize: '64px', fontWeight: 900, color: '#f0f6fc', lineHeight: 1.05, margin: '0 0 28px' }}>
          GOT HUB?
        </h1>
        <div style={{ color: '#8b949e', fontSize: '18px', lineHeight: 1.7, marginBottom: '24px' }}>
          <p style={{ margin: '0 0 4px' }}>Your code's conflicted.</p>
          <p style={{ margin: '0 0 4px' }}>Your merges are lying.</p>
          <p style={{ margin: '0 0 16px' }}>Your diffs are screaming.</p>
        </div>
        <div style={{ marginBottom: '24px' }}>
          <span style={{ fontSize: '22px', fontWeight: 700, color: '#58a6ff' }}>GotHub.dev</span>
          <div style={{ width: '48px', height: '3px', background: '#1f6feb', borderRadius: '2px', margin: '6px 0 16px' }} />
        </div>
        <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 20px', color: '#c9d1d9', fontSize: '15px', lineHeight: 1.8 }}>
          <li>Merges functions, not lines.</li>
          <li>Imports don't "conflict." They combine.</li>
          <li>Refactors don't look like chaos.</li>
          <li>Your repo, but with a spine.</li>
        </ul>
        <p style={{ color: '#8b949e', fontSize: '14px', margin: '0 0 20px', maxWidth: '480px' }}>
          A GotHub repo parses code as real structures — functions, types, imports — so changes actually make sense.
        </p>
        <p style={{ fontSize: '18px', fontWeight: 700, color: '#f0f6fc', margin: 0 }}>
          GotHub. Got VCS?
        </p>
      </div>
      <div style={{ flex: '0 0 340px', minWidth: '300px' }}>
        <HeroSignupCard />
      </div>
    </section>
  );
}

function HeroSignupCard() {
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [passkeyEnabled, setPasskeyEnabled] = useState(true);
  const [passkeyUser, setPasskeyUser] = useState('');

  const passkeysAvailable = browserSupportsPasskeys() && passkeyEnabled;

  useEffect(() => {
    getAuthCapabilities()
      .then((caps) => setPasskeyEnabled(!!caps.passkey_enabled))
      .catch(() => {});
  }, []);

  const handleRegister = async (e: Event) => {
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

  const handlePasskey = async (e: Event) => {
    e.preventDefault();
    setError(''); setSubmitting(true);
    try {
      const begin = await beginWebAuthnLogin(passkeyUser);
      const credential = await getPasskeyAssertion(begin.options);
      const res = await finishWebAuthnLogin(begin.session_id, credential);
      setToken(res.token);
      window.location.assign('/');
    } catch (err: any) {
      setError(err.message || 'Passkey sign-in failed');
    } finally { setSubmitting(false); }
  };

  return (
    <div style={{ background: '#161b22', border: '1px solid #30363d', borderRadius: '8px', padding: '24px' }}>
      <h2 style={{ fontSize: '18px', color: '#f0f6fc', margin: '0 0 16px' }}>Create your account</h2>
      {error && <div style={{ color: '#f85149', marginBottom: '12px', padding: '10px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px', fontSize: '13px' }}>{error}</div>}

      <form onSubmit={handleRegister} style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginBottom: '14px' }}>
        <input value={username} onInput={(e: any) => setUsername(e.target.value)} placeholder="Username" style={inputStyle} />
        <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email" type="email" style={inputStyle} />
        <button type="submit" disabled={submitting || !username || !email}
          style={{ ...btnPrimary, opacity: submitting || !username || !email ? 0.6 : 1 }}>
          Create account
        </button>
      </form>

      <div style={{ borderTop: '1px solid #30363d', paddingTop: '12px' }}>
        <form onSubmit={handlePasskey} style={{ display: 'flex', gap: '8px' }}>
          <input value={passkeyUser} onInput={(e: any) => setPasskeyUser(e.target.value)} placeholder="Username" style={{ ...inputStyle, flex: 1 }} />
          <button type="submit" disabled={submitting || !passkeyUser || !passkeysAvailable}
            style={{ ...btnSecondary, opacity: submitting || !passkeyUser || !passkeysAvailable ? 0.6 : 1, whiteSpace: 'nowrap', width: 'auto', padding: '10px 14px' }}>
            Passkey sign-in
          </button>
        </form>
      </div>

      <p style={{ color: '#8b949e', fontSize: '12px', margin: '12px 0 0', textAlign: 'center' }}>
        Already have an account? <a href="/login" style={{ color: '#58a6ff' }}>Sign in</a>
      </p>
    </div>
  );
}

/* ── Feature Cards ────────────────────────────────────────────────────── */

const features = [
  { title: 'Structural Blame', desc: 'Attribution at the function level, not line-by-line. See who wrote each entity and when it last changed.' },
  { title: 'Call Graphs', desc: 'Explore caller/callee relationships across your codebase. Understand impact before you merge.' },
  { title: 'Entity-Aware PRs', desc: 'PR diffs that show which functions were added, modified, or removed — not just which lines changed.' },
  { title: 'SemVer Impact', desc: 'Automatic semantic versioning recommendations based on structural changes to your public API.' },
];

function FeatureCards() {
  return (
    <section style={{ padding: '32px 0' }}>
      <h2 style={{ fontSize: '28px', fontWeight: 700, color: '#f0f6fc', marginBottom: '20px' }}>What makes GotHub different</h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: '16px' }}>
        {features.map((f) => (
          <div key={f.title} style={{ background: '#161b22', border: '1px solid #30363d', borderRadius: '8px', padding: '20px' }}>
            <h3 style={{ fontSize: '16px', fontWeight: 700, color: '#f0f6fc', margin: '0 0 8px' }}>{f.title}</h3>
            <p style={{ fontSize: '14px', color: '#8b949e', margin: 0, lineHeight: 1.5 }}>{f.desc}</p>
          </div>
        ))}
      </div>
    </section>
  );
}

/* ── Explore ──────────────────────────────────────────────────────────── */

function ExploreSection() {
  const [repos, setRepos] = useState<Repository[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listExploreRepos(1, 6, 'created')
      .then(setRepos)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <section style={{ padding: '32px 0', color: '#8b949e' }}>Loading repositories...</section>;
  if (repos.length === 0) return null;

  return (
    <section style={{ padding: '32px 0' }}>
      <h2 style={{ fontSize: '28px', fontWeight: 700, color: '#f0f6fc', marginBottom: '20px' }}>Explore public repos</h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '12px' }}>
        {repos.map((r) => (
          <a key={r.id} href={`/${r.owner_name}/${r.name}`}
            style={{ display: 'block', background: '#161b22', border: '1px solid #30363d', borderRadius: '8px', padding: '16px', textDecoration: 'none', color: 'inherit' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
              <span style={{ fontSize: '15px', fontWeight: 600, color: '#58a6ff' }}>
                {r.owner_name}/{r.name}
              </span>
              {(r as any).star_count > 0 && (
                <span style={{ fontSize: '12px', color: '#8b949e' }}>{(r as any).star_count} stars</span>
              )}
            </div>
            {r.description && (
              <p style={{ fontSize: '13px', color: '#8b949e', margin: 0, lineHeight: 1.4 }}>{r.description}</p>
            )}
          </a>
        ))}
      </div>
    </section>
  );
}

/* ── Footer CTA ───────────────────────────────────────────────────────── */

function FooterCTA() {
  const [email, setEmail] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const [error, setError] = useState('');

  const handleMailingList = async (e: Event) => {
    e.preventDefault();
    setError('');
    try {
      await createInterestSignup({ email, source: 'landing_footer' });
      setSubmitted(true);
    } catch (err: any) {
      setError(err.message);
    }
  };

  return (
    <section style={{ padding: '48px 0', textAlign: 'center', borderTop: '1px solid #21262d' }}>
      <h2 style={{ fontSize: '24px', fontWeight: 700, color: '#f0f6fc', marginBottom: '12px' }}>
        Ready to try entity-aware version control?
      </h2>
      <a href="/signup" style={{ display: 'inline-block', background: '#238636', color: '#fff', padding: '12px 32px', borderRadius: '6px', fontWeight: 'bold', fontSize: '16px', textDecoration: 'none', marginBottom: '32px' }}>
        Get started
      </a>
      <div style={{ maxWidth: '400px', margin: '0 auto' }}>
        {submitted ? (
          <p style={{ color: '#3fb950', fontSize: '14px' }}>Thanks! We'll keep you posted.</p>
        ) : (
          <form onSubmit={handleMailingList} style={{ display: 'flex', gap: '8px' }}>
            <input value={email} onInput={(e: any) => setEmail(e.target.value)} placeholder="Email for updates" type="email"
              style={{ ...inputStyle, flex: 1 }} />
            <button type="submit" disabled={!email}
              style={{ ...btnSecondary, opacity: !email ? 0.6 : 1, whiteSpace: 'nowrap', width: 'auto', padding: '10px 16px' }}>
              Join mailing list
            </button>
          </form>
        )}
        {error && <p style={{ color: '#f85149', fontSize: '13px', marginTop: '8px' }}>{error}</p>}
      </div>
    </section>
  );
}

/* ── Shared styles ────────────────────────────────────────────────────── */

const inputStyle: Record<string, string> = { background: '#0d1117', border: '1px solid #30363d', borderRadius: '6px', padding: '10px 12px', color: '#c9d1d9', fontSize: '14px', width: '100%', boxSizing: 'border-box' };
const btnPrimary: Record<string, string> = { background: '#238636', color: '#fff', border: 'none', padding: '10px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '14px', width: '100%' };
const btnSecondary: Record<string, string> = { background: '#21262d', color: '#c9d1d9', border: '1px solid #30363d', padding: '10px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '14px', width: '100%' };
