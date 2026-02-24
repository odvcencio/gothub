// Preact-router injects `path` and `matches` into routed components.
// Declare module augmentation so TSC accepts path= on components.
import 'preact';

declare module 'preact' {
  namespace JSX {
    interface IntrinsicAttributes {
      path?: string;
    }
  }
}
