import { useState, useEffect } from 'preact/hooks';
import {
  getToken,
  getRepoCreationPolicy,
  getUser,
  createRepo,
  type RepoCreationPolicy,
  type APIUser,
} from '../api/client';

interface Props {
  path?: string;
}

const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/;
const MAX_NAME_LENGTH = 100;

function validateName(name: string): string | null {
  if (!name) return 'Repository name is required';
  if (name.length > MAX_NAME_LENGTH) return `Name must be ${MAX_NAME_LENGTH} characters or fewer`;
  if (!NAME_PATTERN.test(name)) return 'Name must start with a letter or number and contain only alphanumeric characters, hyphens, dots, or underscores';
  return null;
}

export function NewRepoView(_props: Props) {
  const [user, setUser] = useState<APIUser | null>(null);
  const [policy, setPolicy] = useState<RepoCreationPolicy | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [visibility, setVisibility] = useState<'public' | 'private'>('public');
  const [initReadme, setInitReadme] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const [nameTouched, setNameTouched] = useState(false);

  useEffect(() => {
    if (!getToken()) {
      window.location.assign('/login?returnTo=/new');
      return;
    }

    const load = async () => {
      try {
        const [u, p] = await Promise.all([getUser(), getRepoCreationPolicy()]);
        setUser(u);
        setPolicy(p);
      } catch (e: any) {
        setError(e.message || 'Failed to load account information');
      } finally {
        setLoading(false);
      }
    };
    load();
  }, []);

  const handleNameInput = (e: any) => {
    const val = e.target.value as string;
    setName(val);
    if (nameTouched) {
      setNameError(validateName(val));
    }
  };

  const handleNameBlur = () => {
    setNameTouched(true);
    setNameError(validateName(name));
  };

  const isPrivate = visibility === 'private';
  const canCreatePrivate = policy?.can_create_private !== false;
  const showUpgradePrompt = isPrivate && policy && !canCreatePrivate;

  const formValid = !!name && !validateName(name) && !(isPrivate && !canCreatePrivate);

  const handleSubmit = async (e: Event) => {
    e.preventDefault();
    if (!formValid || submitting) return;

    setError('');
    setSubmitting(true);
    try {
      await createRepo(name, description, isPrivate);
      const username = user?.username || '';
      window.location.assign(`/${username}/${name}`);
    } catch (err: any) {
      setError(err.message || 'Failed to create repository');
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div style={{ maxWidth: '600px', margin: '60px auto', textAlign: 'center', color: '#8b949e' }}>
        Loading...
      </div>
    );
  }

  return (
    <div style={{ maxWidth: '600px', margin: '40px auto' }}>
      <h1 style={{ fontSize: '24px', fontWeight: 'bold', color: '#f0f6fc', marginBottom: '8px' }}>
        Create a new repository
      </h1>
      <p style={{ color: '#8b949e', fontSize: '14px', marginBottom: '24px' }}>
        A repository contains all project files, including the revision history.
      </p>

      {error && (
        <div style={errorBannerStyle}>
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
        {/* Owner / Name */}
        <div>
          <label style={labelStyle}>
            Repository name <span style={{ color: '#f85149' }}>*</span>
          </label>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            {user && (
              <span style={{ color: '#c9d1d9', fontSize: '14px', whiteSpace: 'nowrap' }}>
                {user.username} /
              </span>
            )}
            <input
              type="text"
              value={name}
              onInput={handleNameInput}
              onBlur={handleNameBlur}
              placeholder="my-cool-repo"
              maxLength={MAX_NAME_LENGTH}
              style={{
                ...inputStyle,
                flex: 1,
                borderColor: nameTouched && nameError ? '#f85149' : '#30363d',
              }}
            />
          </div>
          {nameTouched && nameError && (
            <div style={{ color: '#f85149', fontSize: '12px', marginTop: '4px' }}>{nameError}</div>
          )}
        </div>

        {/* Description */}
        <div>
          <label style={labelStyle}>Description <span style={{ color: '#8b949e', fontWeight: 'normal' }}>(optional)</span></label>
          <input
            type="text"
            value={description}
            onInput={(e: any) => setDescription(e.target.value)}
            placeholder="Short description of your repository"
            style={inputStyle}
          />
        </div>

        {/* Visibility radio cards */}
        <div>
          <label style={labelStyle}>Visibility</label>
          <div style={{ display: 'flex', gap: '12px' }}>
            <label
              style={{
                ...radioCardStyle,
                borderColor: visibility === 'public' ? '#1f6feb' : '#30363d',
                background: visibility === 'public' ? '#0d2240' : '#161b22',
              }}
            >
              <input
                type="radio"
                name="visibility"
                value="public"
                checked={visibility === 'public'}
                onChange={() => setVisibility('public')}
                style={{ marginRight: '8px', accentColor: '#1f6feb' }}
              />
              <div>
                <div style={{ color: '#f0f6fc', fontWeight: 'bold', fontSize: '14px' }}>Public</div>
                <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '2px' }}>
                  Anyone can see this repository.
                </div>
              </div>
            </label>
            <label
              style={{
                ...radioCardStyle,
                borderColor: visibility === 'private' ? '#1f6feb' : '#30363d',
                background: visibility === 'private' ? '#0d2240' : '#161b22',
              }}
            >
              <input
                type="radio"
                name="visibility"
                value="private"
                checked={visibility === 'private'}
                onChange={() => setVisibility('private')}
                style={{ marginRight: '8px', accentColor: '#1f6feb' }}
              />
              <div>
                <div style={{ color: '#f0f6fc', fontWeight: 'bold', fontSize: '14px' }}>Private</div>
                <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '2px' }}>
                  Only you and collaborators can see this repository.
                </div>
              </div>
            </label>
          </div>
        </div>

        {/* Upgrade prompt when private not available */}
        {showUpgradePrompt && (
          <div style={upgradePromptStyle}>
            <div style={{ color: '#d29922', fontWeight: 'bold', fontSize: '14px', marginBottom: '6px' }}>
              Private repositories require a GotHub Pro subscription.
            </div>
            {policy?.private_reason && (
              <div style={{ color: '#d29922', fontSize: '13px', opacity: 0.85, marginBottom: '8px' }}>
                {policy.private_reason}
              </div>
            )}
            <div id="polar-checkout-mount" />
          </div>
        )}

        {/* Initialize with README */}
        <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={initReadme}
            onChange={(e: any) => setInitReadme(e.target.checked)}
            style={{ accentColor: '#1f6feb' }}
          />
          <span style={{ color: '#c9d1d9', fontSize: '14px' }}>Initialize this repository with a README</span>
        </label>

        {/* Quota display */}
        {policy && (
          <div style={quotaContainerStyle}>
            <div style={{ color: '#8b949e', fontSize: '12px', fontWeight: 'bold', textTransform: 'uppercase', letterSpacing: '0.04em', marginBottom: '6px' }}>
              Repository quota
            </div>
            <div style={{ display: 'flex', gap: '24px' }}>
              {policy.max_public_repos > 0 && (
                <div style={{ color: '#8b949e', fontSize: '13px' }}>
                  <span style={{ color: '#c9d1d9' }}>Public:</span>{' '}
                  {policy.public_repo_count} / {policy.max_public_repos}
                </div>
              )}
              {policy.max_private_repos > 0 && (
                <div style={{ color: '#8b949e', fontSize: '13px' }}>
                  <span style={{ color: '#c9d1d9' }}>Private:</span>{' '}
                  {policy.private_repo_count} / {policy.max_private_repos}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Divider */}
        <div style={{ borderTop: '1px solid #30363d' }} />

        {/* Submit */}
        <button
          type="submit"
          disabled={!formValid || submitting}
          style={{
            background: '#238636',
            color: '#fff',
            border: 'none',
            padding: '10px 20px',
            borderRadius: '6px',
            fontWeight: 'bold',
            fontSize: '14px',
            cursor: !formValid || submitting ? 'not-allowed' : 'pointer',
            opacity: !formValid || submitting ? 0.6 : 1,
            alignSelf: 'flex-start',
          }}
        >
          {submitting ? 'Creating...' : 'Create repository'}
        </button>
      </form>
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
  width: '100%',
  boxSizing: 'border-box' as const,
};

const labelStyle = {
  display: 'block',
  color: '#f0f6fc',
  fontSize: '14px',
  fontWeight: 'bold',
  marginBottom: '6px',
};

const radioCardStyle = {
  flex: 1,
  display: 'flex',
  alignItems: 'flex-start',
  padding: '12px',
  border: '1px solid #30363d',
  borderRadius: '6px',
  cursor: 'pointer',
};

const upgradePromptStyle = {
  border: '1px solid #d29922',
  borderRadius: '6px',
  padding: '14px',
  background: '#2b230f',
};

const errorBannerStyle = {
  color: '#f85149',
  marginBottom: '16px',
  padding: '10px 12px',
  background: '#1c1214',
  border: '1px solid #f85149',
  borderRadius: '6px',
  fontSize: '13px',
};

const quotaContainerStyle = {
  padding: '12px',
  background: '#161b22',
  border: '1px solid #30363d',
  borderRadius: '6px',
};
