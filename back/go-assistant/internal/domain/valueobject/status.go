// Package valueobject contains domain value objects for the go-assistant service.
package valueobject

// Status represents the health status of a service or component.
type Status string

const (
	// StatusUp indicates the component is healthy and operational.
	StatusUp Status = "UP"
	// StatusDown indicates the component is unhealthy or unavailable.
	StatusDown Status = "DOWN"
)

// String returns the string representation of the Status.
func (s Status) String() string {
	return string(s)
}
