package valueobject

// Status represents the health status of a component or the overall service.
type Status string

const (
	// StatusUp indicates the component is healthy and functioning normally.
	StatusUp Status = "UP"
	// StatusDown indicates the component is unhealthy or unreachable.
	StatusDown Status = "DOWN"
)

// String returns the string representation of the Status.
func (s Status) String() string {
	return string(s)
}
