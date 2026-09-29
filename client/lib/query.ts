import { createResource, type Resource } from "solid-js"

// Suspense-tracked fetch helper. Pages call this in component setup
// instead of managing loading/error states by hand:
//
//   const receipts = createQuery(() => api.get("/receipts/enriched"))
//   // in JSX, under <QueryBoundary>: receipts() (or <For each={receipts()}>)
//
// Solid 2.0 seam: 2.0 removes createResource in favor of async
// computations + <Loading>. When that upgrade happens, reimplement
// this ONE function on an async memo and swap QueryBoundary's
// <Suspense> for <Loading> — no page call site changes.
export function createQuery<T>(fetcher: () => Promise<T>): Resource<T> {
  const [data] = createResource(fetcher)
  return data
}
