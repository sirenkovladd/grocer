import { ErrorBoundary, Suspense, type JSX } from "solid-js"

interface QueryBoundaryProps {
  pending?: JSX.Element
  children: JSX.Element
}

// Page-level async boundary. Replaces both the VanJS per-page
// loading/error states and main.ts's withErrorBoundary: data reads
// suspend on the fallback, and any fetch/render error shows a retry
// card while the sidebar stays usable.
//
// Solid 2.0 seam: <Suspense> becomes <Loading> and <ErrorBoundary>
// becomes <Errored>. Only this file changes in that upgrade.
export default function QueryBoundary(props: QueryBoundaryProps) {
  return (
    <ErrorBoundary
      fallback={(err: Error, reset) => (
        <div class="empty-state">
          <h3>Couldn't load this page</h3>
          <p>{err.message || "Something went wrong"}</p>
          <button onClick={reset}>Try Again</button>
        </div>
      )}
    >
      <Suspense fallback={props.pending}>{props.children}</Suspense>
    </ErrorBoundary>
  )
}
