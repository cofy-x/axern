// Package environment owns normalized environment source rules in controld.
//
// Put environment source validation and normalization here when the logic
// decides how template-backed and image-backed environments become one resolved
// runtime snapshot. That includes template lookup, image resolution inputs, and
// synthesized runtime-template defaults for image-backed environments.
//
// Keep persistence in runkernel, API request shaping in internal/api/*, and
// image transport/runtime execution details out of this package.
package environment
