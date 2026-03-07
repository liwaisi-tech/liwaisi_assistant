package tool

// Set groups related tools for batch registration into a Registry.
type Set interface {
	Register(registry *Registry)
}
