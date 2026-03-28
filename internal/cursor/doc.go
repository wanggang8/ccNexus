// Package cursor implements a dedicated Cursor data plane.
//
// The goal of this package is to make /cursor/* semantics converge on
// api2cursor while keeping ccNexus-specific control-plane capabilities
// (endpoint selection, auth, model override, logging, traffic recording)
// outside of the package.
package cursor
