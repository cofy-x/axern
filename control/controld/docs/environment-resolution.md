# Environment Resolution

An Environment has one explicit OCI image source. Controld resolves the caller-visible reference to an immutable descriptor, then stores both the normalized source specification and the resolved runtime input.

Run admission copies the Environment source and resolved input into the Run transaction. Allocation start and final rootfs sealing therefore remain valid after the reusable Environment row is deleted and never infer identity from an OCI annotation, node-local tag, or runtime label.

The image source may reference a stored registry credential by ID. Credential material is resolved only at the control-to-node boundary and is never embedded in the public Environment or Run resource. Generic read-only image mounts are separate execution inputs and create no additional image lifecycle.

The resolved specification contains the image descriptor and normalized execution policy required by the node. Nodes consume this immutable input; they do not apply a second deployment-specific workload profile.
