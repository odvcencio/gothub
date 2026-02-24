import { useState, useEffect } from 'preact/hooks';
import {
  getUser,
  getToken,
  listSSHKeys,
  createSSHKey,
  deleteSSHKey,
  listUserOrgs,
  createOrg,
  beginWebAuthnRegistration,
  finishWebAuthnRegistration,
} from '../api/client';
import { browserSupportsPasskeys, createPasskeyCredential } from '../lib/webauthn';

interface Props {
  path?: string;
}

export function SettingsView({ path }: Props) {
  const loggedIn = !!getToken();

  if (!loggedIn) {
    return (
      <div style={{ maxWidth: '600px', margin: '60px auto', textAlign: 'center' }}>
        <p style={{ color: '#8b949e', fontSize: '16px' }}>Sign in to view settings</p>
      </div>
    );
  }

  return (
    <div style={{ maxWidth: '800px', margin: '0 auto' }}>
      <h1 style={{ fontSize: '24px', color: '#f0f6fc', marginBottom: '24px' }}>Settings</h1>
      <ProfileSection />
      <PasskeysSection />
      <SSHKeysSection />
      <OrganizationsSection />
    </div>
  );
}

function ProfileSection() {
  const [user, setUser] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    getUser()
      .then(setUser)
      .catch((e: any) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div style={{ marginBottom: '32px' }}>
      <h2 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '12px' }}>Profile</h2>
      {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}
      {loading ? (
        <div style={{ color: '#8b949e' }}>Loading...</div>
      ) : user ? (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', background: '#161b22' }}>
          <div style={{ marginBottom: '12px' }}>
            <span style={{ color: '#8b949e', fontSize: '13px' }}>Username</span>
            <div style={{ color: '#c9d1d9', fontSize: '15px', marginTop: '4px' }}>{user.username}</div>
          </div>
          <div>
            <span style={{ color: '#8b949e', fontSize: '13px' }}>Email</span>
            <div style={{ color: '#c9d1d9', fontSize: '15px', marginTop: '4px' }}>{user.email}</div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function SSHKeysSection() {
  const [keys, setKeys] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showAdd, setShowAdd] = useState(false);
  const [name, setName] = useState('');
  const [publicKey, setPublicKey] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const loadKeys = () => {
    setLoading(true);
    listSSHKeys()
      .then(setKeys)
      .catch((e: any) => setError(e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadKeys();
  }, []);

  const handleAdd = async (e: Event) => {
    e.preventDefault();
    if (!name.trim() || !publicKey.trim()) return;
    setSubmitting(true);
    setError('');
    try {
      await createSSHKey(name.trim(), publicKey.trim());
      setName('');
      setPublicKey('');
      setShowAdd(false);
      loadKeys();
    } catch (err: any) {
      setError(err.message || 'Failed to add SSH key');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: number, keyName: string) => {
    if (!window.confirm(`Delete SSH key "${keyName}"?`)) return;
    setError('');
    try {
      await deleteSSHKey(id);
      loadKeys();
    } catch (err: any) {
      setError(err.message || 'Failed to delete SSH key');
    }
  };

  return (
    <div style={{ marginBottom: '32px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
        <h2 style={{ fontSize: '20px', color: '#f0f6fc', margin: 0 }}>SSH Keys</h2>
        <button
          onClick={() => setShowAdd(!showAdd)}
          style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '13px' }}
        >
          {showAdd ? 'Cancel' : 'Add SSH Key'}
        </button>
      </div>

      {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}

      {showAdd && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', marginBottom: '16px', background: '#161b22' }}>
          <form onSubmit={handleAdd} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <input
              value={name}
              onInput={(e: any) => setName(e.target.value)}
              placeholder="Key name (e.g. work laptop)"
              style={inputStyle}
            />
            <textarea
              value={publicKey}
              onInput={(e: any) => setPublicKey(e.target.value)}
              placeholder="Paste your public key here (ssh-ed25519 AAAA... or ssh-rsa AAAA...)"
              style={{
                ...inputStyle,
                minHeight: '100px',
                fontFamily: 'monospace',
                resize: 'vertical' as any,
              }}
            />
            <button
              type="submit"
              disabled={submitting || !name.trim() || !publicKey.trim()}
              style={{
                background: '#238636',
                color: '#fff',
                border: 'none',
                padding: '8px 16px',
                borderRadius: '6px',
                cursor: submitting ? 'not-allowed' : 'pointer',
                fontWeight: 'bold',
                fontSize: '13px',
                alignSelf: 'flex-start',
                opacity: submitting || !name.trim() || !publicKey.trim() ? 0.6 : 1,
              }}
            >
              {submitting ? 'Adding...' : 'Add key'}
            </button>
          </form>
        </div>
      )}

      {loading ? (
        <div style={{ color: '#8b949e' }}>Loading...</div>
      ) : keys.length === 0 ? (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', color: '#8b949e' }}>
          No SSH keys configured
        </div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {keys.map((key, idx) => (
            <div
              key={key.id}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                padding: '12px 16px',
                borderTop: idx === 0 ? 'none' : '1px solid #30363d',
              }}
            >
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ color: '#f0f6fc', fontSize: '14px', fontWeight: 'bold', marginBottom: '4px' }}>
                  {key.name}
                </div>
                <div style={{ color: '#8b949e', fontSize: '12px' }}>
                  <span style={{ marginRight: '12px' }}>{key.key_type}</span>
                  <span style={{ fontFamily: 'monospace' }}>
                    {key.fingerprint && key.fingerprint.length > 32
                      ? key.fingerprint.substring(0, 32) + '...'
                      : key.fingerprint}
                  </span>
                </div>
                <div style={{ color: '#8b949e', fontSize: '11px', marginTop: '2px' }}>
                  Added {key.created_at ? new Date(key.created_at).toLocaleDateString() : 'unknown'}
                </div>
              </div>
              <button
                onClick={() => handleDelete(key.id, key.name)}
                style={{
                  background: 'transparent',
                  color: '#f85149',
                  border: '1px solid #f85149',
                  padding: '4px 12px',
                  borderRadius: '6px',
                  cursor: 'pointer',
                  fontSize: '12px',
                  marginLeft: '12px',
                  flexShrink: 0,
                }}
              >
                Delete
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function PasskeysSection() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const available = browserSupportsPasskeys();

  const handleRegister = async () => {
    setBusy(true);
    setError('');
    setSuccess('');
    try {
      const begin = await beginWebAuthnRegistration();
      const credential = await createPasskeyCredential(begin.options);
      const result = await finishWebAuthnRegistration(begin.session_id, credential);
      setSuccess(`Passkey registered (${result.credential_id.slice(0, 16)}...)`);
    } catch (err: any) {
      setError(err.message || 'Failed to register passkey');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ marginBottom: '32px' }}>
      <h2 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '12px' }}>Passkeys</h2>
      {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}
      {success && <div style={{ color: '#3fb950', marginBottom: '12px' }}>{success}</div>}
      <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', background: '#161b22' }}>
        <p style={{ marginTop: 0, marginBottom: '12px', color: '#8b949e', fontSize: '13px' }}>
          Passkeys are the preferred passwordless sign-in method for gothub.
        </p>
        <button
          onClick={handleRegister}
          disabled={!available || busy}
          style={{
            background: '#238636',
            color: '#fff',
            border: 'none',
            padding: '8px 16px',
            borderRadius: '6px',
            cursor: !available || busy ? 'not-allowed' : 'pointer',
            fontWeight: 'bold',
            fontSize: '13px',
            opacity: !available || busy ? 0.6 : 1,
          }}
        >
          {available ? (busy ? 'Registering...' : 'Register passkey') : 'Passkeys unavailable in this browser'}
        </button>
      </div>
    </div>
  );
}

function OrganizationsSection() {
  const [orgs, setOrgs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const loadOrgs = () => {
    setLoading(true);
    listUserOrgs()
      .then(setOrgs)
      .catch((e: any) => setError(e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadOrgs();
  }, []);

  const handleCreate = async (e: Event) => {
    e.preventDefault();
    if (!name.trim()) return;
    setSubmitting(true);
    setError('');
    try {
      await createOrg(name.trim(), displayName.trim());
      setName('');
      setDisplayName('');
      setShowCreate(false);
      loadOrgs();
    } catch (err: any) {
      setError(err.message || 'Failed to create organization');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ marginBottom: '32px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
        <h2 style={{ fontSize: '20px', color: '#f0f6fc', margin: 0 }}>Organizations</h2>
        <button
          onClick={() => setShowCreate(!showCreate)}
          style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', cursor: 'pointer', fontWeight: 'bold', fontSize: '13px' }}
        >
          {showCreate ? 'Cancel' : 'Create organization'}
        </button>
      </div>

      {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}

      {showCreate && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', marginBottom: '16px', background: '#161b22' }}>
          <form onSubmit={handleCreate} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <input
              value={name}
              onInput={(e: any) => setName(e.target.value)}
              placeholder="Organization name"
              style={inputStyle}
            />
            <input
              value={displayName}
              onInput={(e: any) => setDisplayName(e.target.value)}
              placeholder="Display name (optional)"
              style={inputStyle}
            />
            <button
              type="submit"
              disabled={submitting || !name.trim()}
              style={{
                background: '#238636',
                color: '#fff',
                border: 'none',
                padding: '8px 16px',
                borderRadius: '6px',
                cursor: submitting ? 'not-allowed' : 'pointer',
                fontWeight: 'bold',
                fontSize: '13px',
                alignSelf: 'flex-start',
                opacity: submitting || !name.trim() ? 0.6 : 1,
              }}
            >
              {submitting ? 'Creating...' : 'Create organization'}
            </button>
          </form>
        </div>
      )}

      {loading ? (
        <div style={{ color: '#8b949e' }}>Loading...</div>
      ) : orgs.length === 0 ? (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', color: '#8b949e' }}>
          Not a member of any organizations
        </div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {orgs.map((org, idx) => (
            <a
              key={org.id}
              href={`/orgs/${org.name}`}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '12px',
                padding: '12px 16px',
                borderTop: idx === 0 ? 'none' : '1px solid #30363d',
                color: '#58a6ff',
                textDecoration: 'none',
                fontWeight: 'bold',
                fontSize: '14px',
              }}
            >
              {org.display_name || org.name}
              {org.display_name && org.display_name !== org.name && (
                <span style={{ color: '#8b949e', fontWeight: 'normal', fontSize: '13px' }}>{org.name}</span>
              )}
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
