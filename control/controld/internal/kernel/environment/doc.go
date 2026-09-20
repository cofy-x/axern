// Package environment owns normalized environment source rules in controld.
//
// Put environment source validation and normalization here when the logic
// decides how an image-backed Environment becomes one resolved runtime
// specification. That includes image resolution inputs and normalized execution
// defaults.
//
// Keep persistence in runkernel, API request shaping in internal/api/*, and
// image transport/runtime execution details out of this package.
package environment
