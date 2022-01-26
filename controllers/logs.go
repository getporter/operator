package controllers

const (
	// Includes information about the code path
	Log5Trace = iota

	// Includes additional logs and data that help with debugging
	Log4Debug

	// Includes information about the state of the host
	Log3SystemState

	// Includes information about the state of the controllers
	Log2ApplicationState

	// Includes information about notable actions performed
	Log1Info

	// Error and warning messages only
	Log0Error
)
