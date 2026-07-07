package admission

// Priority class for PGKeeper-style admission control.
type Priority int

const (
	PriorityHigh Priority = iota
	PriorityLow
)

func (p Priority) String() string {
	if p == PriorityHigh {
		return "high"
	}
	return "low"
}

// ParsePriority maps "high" / "low" strings; unknown values default to low.
func ParsePriority(s string) Priority {
	switch s {
	case "high", "HIGH", "critical":
		return PriorityHigh
	default:
		return PriorityLow
	}
}
