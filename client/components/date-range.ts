import van from "vanjs-core"

const { div, button, input, span } = van.tags

interface DateRangeProps {
  from: any
  to: any
  onChange?: () => void
}

const DateRange = ({ from, to, onChange }: DateRangeProps) => {
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
    from.val = newFrom
    to.val = newTo
    if (onChange) onChange()
  }

  return div({ class: "date-range" },
    div({ class: "date-presets" },
      ...presets.map(p =>
        button({
          class: () => `preset-btn ${from.val === p.from && to.val === p.to ? "active" : ""}`,
          onclick: () => setRange(p.from, p.to),
        }, p.label)
      ),
    ),
    div({ class: "date-inputs" },
      input({
        type: "date",
        value: from,
        onchange: (e: Event) => {
          from.val = (e.target as HTMLInputElement).value
          if (onChange) onChange()
        },
      }),
      span("to"),
      input({
        type: "date",
        value: to,
        onchange: (e: Event) => {
          to.val = (e.target as HTMLInputElement).value
          if (onChange) onChange()
        },
      }),
    ),
  )
}

export default DateRange
