import { createResource, type Resource } from "solid-js"

// Suspense-tracked fetch helper. Pages call this in component setup
// instead of managing loading/error states by hand:
//
//   const { data, refetch } = createQuery(() => api.get("/receipts/enriched"))
//   // in JSX, under <QueryBoundary>: data() (or <For each={data()}>)
//   // after a mutation: refetch()
//
// Solid 2.0 seam: 2.0 removes createResource in favor of async
// computations + <Loading>. When that upgrade happens, reimplement
// this ONE function on an async memo and swap QueryBoundary's
// <Suspense> for <Loading> — page call sites keep working
// (data() reads, refetch() after mutations).
export function createQuery<T>(fetcher: () => Promise<T>): {
  data: Resource<T>
  refetch: () => void
} {
  const [data, { refetch }] = createResource(fetcher)
  return { data, refetch: () => void refetch() }
}
