import { For } from "solid-js"

interface DateRangeProps {
  from: () => string
  setFrom: (v: string) => void
  to: () => string
  setTo: (v: string) => void
  onChange?: () => void
}

export default function DateRange(props: DateRangeProps) {
  const today = () => new Date().toISOString().split("T")[0]

  const startOfWeek = () => {
    const d = new Date()
    const day = d.getDay()
    const diff = d.getDate() - day + (day === 0 ? -6 : 1)
    return new Date(d.setDate(diff)).toISOString().split("T")[0]
  }

  const startOfMonth = () => {
    const d = new Date()
    return new Date(d.getFullYear(), d.getMonth(), 1).toISOString().split("T")[0]
  }

  const monthsAgo = (n: number) => {
    const d = new Date()
    d.setMonth(d.getMonth() - n)
    return d.toISOString().split("T")[0]
  }

  const startOfYear = () => {
    const d = new Date()
    return new Date(d.getFullYear(), 0, 1).toISOString().split("T")[0]
  }

  const presets = [
    { label: "This week", from: startOfWeek(), to: today() },
    { label: "This month", from: startOfMonth(), to: today() },
    { label: "Last 3 months", from: monthsAgo(3), to: today() },
    { label: "This year", from: startOfYear(), to: today() },
    { label: "All time", from: "", to: "" },
  ]

  const setRange = (newFrom: string, newTo: string) => {
    props.setFrom(newFrom)
    props.setTo(newTo)
    props.onChange?.()
  }

  return (
    <div class="date-range">
      <div class="date-presets">
        <For each={presets}>
          {(p) => (
            <button
              class={`preset-btn ${props.from() === p.from && props.to() === p.to ? "active" : ""}`}
              onClick={() => setRange(p.from, p.to)}
            >
              {p.label}
            </button>
          )}
        </For>
      </div>
      <div class="date-inputs">
        <input
          type="date"
          value={props.from()}
          onChange={(e) => {
            props.setFrom(e.currentTarget.value)
            props.onChange?.()
          }}
        />
        <span>to</span>
        <input
          type="date"
          value={props.to()}
          onChange={(e) => {
            props.setTo(e.currentTarget.value)
            props.onChange?.()
          }}
        />
      </div>
    </div>
  )
}
