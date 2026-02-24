declare global {
  interface Window {
    Go: any;
    gothubHighlight: (filename: string, source: string) => string;
    gothubExtractEntities: (filename: string, source: string) => string;
    gothubDiffEntities: (filename: string, before: string, after: string) => string;
    gothubSupportedLanguages: () => string;
  }
}

let loaded = false;
let loading: Promise<void> | null = null;

export async function loadWasm(): Promise<void> {
  if (loaded) return;
  if (loading) return loading;

  loading = (async () => {
    // Load wasm_exec.js (Go's WASM support script)
    const script = document.createElement('script');
    script.src = '/wasm_exec.js';
    await new Promise<void>((resolve, reject) => {
      script.onload = () => resolve();
      script.onerror = reject;
      document.head.appendChild(script);
    });

    const go = new window.Go();
    const result = await WebAssembly.instantiateStreaming(
      fetch('/gothub.wasm'),
      go.importObject,
    );
    go.run(result.instance);
    loaded = true;
  })();

  return loading;
}

export interface HighlightRange {
  start_byte: number;
  end_byte: number;
  capture: string;
}

export interface EntityInfo {
  kind: string;
  name: string;
  decl_kind: string;
  receiver?: string;
  start_line: number;
  end_line: number;
  key: string;
}

export async function highlight(filename: string, source: string): Promise<HighlightRange[]> {
  await loadWasm();
  const result = window.gothubHighlight(filename, source);
  return JSON.parse(result);
}

export async function extractEntities(filename: string, source: string): Promise<EntityInfo[]> {
  await loadWasm();
  const result = window.gothubExtractEntities(filename, source);
  return JSON.parse(result);
}

export async function diffEntities(filename: string, before: string, after: string) {
  await loadWasm();
  const result = window.gothubDiffEntities(filename, before, after);
  return JSON.parse(result);
}
