function decodeBase64URL(value: string): Uint8Array {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const pad = normalized.length % 4;
  const padded = pad === 0 ? normalized : normalized + '='.repeat(4 - pad);
  const raw = atob(padded);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function encodeBase64URL(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let raw = '';
  for (let i = 0; i < bytes.length; i++) raw += String.fromCharCode(bytes[i]);
  return btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function toBytes(value: unknown): Uint8Array {
  if (value instanceof Uint8Array) return value;
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (typeof value === 'string') return decodeBase64URL(value);
  throw new Error('invalid binary value in WebAuthn options');
}

export function browserSupportsPasskeys(): boolean {
  return typeof window !== 'undefined' && typeof window.PublicKeyCredential !== 'undefined';
}

function toCreationOptions(options: any): PublicKeyCredentialCreationOptions {
  const publicKey = { ...(options?.publicKey ?? options) };
  publicKey.challenge = toBytes(publicKey.challenge);
  if (publicKey.user?.id !== undefined) {
    publicKey.user = { ...publicKey.user, id: toBytes(publicKey.user.id) };
  }
  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map((cred: any) => ({
      ...cred,
      id: toBytes(cred.id),
    }));
  }
  return publicKey;
}

function toRequestOptions(options: any): PublicKeyCredentialRequestOptions {
  const publicKey = { ...(options?.publicKey ?? options) };
  publicKey.challenge = toBytes(publicKey.challenge);
  if (Array.isArray(publicKey.allowCredentials)) {
    publicKey.allowCredentials = publicKey.allowCredentials.map((cred: any) => ({
      ...cred,
      id: toBytes(cred.id),
    }));
  }
  return publicKey;
}

export async function createPasskeyCredential(options: any): Promise<any> {
  const credential = (await navigator.credentials.create({
    publicKey: toCreationOptions(options),
  })) as PublicKeyCredential | null;
  if (!credential) throw new Error('passkey creation was cancelled');

  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    response: {
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      attestationObject: encodeBase64URL(response.attestationObject),
    },
    clientExtensionResults: credential.getClientExtensionResults?.() || {},
  };
}

export async function getPasskeyAssertion(options: any): Promise<any> {
  const credential = (await navigator.credentials.get({
    publicKey: toRequestOptions(options),
  })) as PublicKeyCredential | null;
  if (!credential) throw new Error('passkey sign-in was cancelled');

  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    response: {
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      authenticatorData: encodeBase64URL(response.authenticatorData),
      signature: encodeBase64URL(response.signature),
      userHandle: response.userHandle ? encodeBase64URL(response.userHandle) : null,
    },
    clientExtensionResults: credential.getClientExtensionResults?.() || {},
  };
}
