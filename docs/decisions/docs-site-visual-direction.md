# Documentation Site Visual Direction

## Decision

The Axern documentation site presents the product first as open-source
sandboxes for AI agents and defines its broader boundary as agentic execution
infrastructure. Its visual language is technical, direct, and product-specific:
strong monospace typography, square geometry, restrained color, real code, and
observable runtime behavior.

## Design Contract

- Lead with the AI Sandbox category and product outcome, then provide a local
  path to a working sandbox. Show real SDK and CLI usage immediately after the
  hero, and route readers into the documentation by intent (quickstart, SDK,
  self-hosting, Axrun) rather than by marketing narrative.
- Use a compact grid, square borders, high-contrast controls, and a small set
  of reusable spacing and color tokens.
- Use code, terminal recordings, and runtime state as the primary product
  imagery.
- Use the homepage live execution deck to combine runtime events, isolation,
  lifecycle, and output in one observable surface. Keep terminal recordings
  with the code or guide they substantiate.
- Keep the category headline stable while user-selectable Agent Sandbox and
  Durable Service scenes explain the execution modes. The selected mode may
  replay its internal lifecycle, but the page does not switch modes without
  user input. Present `runsc` as the recommended isolation boundary for
  untrusted agent code and `runc` as the performance-oriented choice for
  trusted long-running services; keep runtime selection driven by trust rather
  than workload duration alone.
- Represent the Agent Sandbox as a horizontal, nested execution chamber. Keep
  `runsc` attached to the outer isolation boundary while code and process
  activity remain inside the inner execution layer.
- Use WebGL only as progressive enhancement. It must remain subtle, pause when
  hidden, respect reduced motion, and never carry essential information.
- Keep English and Simplified Chinese pages on shared components and the same
  information hierarchy.
- Prefer generic OCI images in introductory examples. Introduce templates only
  where their catalog and reuse semantics matter.
- Give every decorative mark a purpose. Avoid ornamental flow diagrams,
  competing arrows, unexplained symbols, mixed corner geometry, and visual
  effects that weaken code readability.

The site remains static, accessible, responsive, and compatible with the
Starlight documentation shell. Visual additions must not add a server runtime
or become a second representation of Axern architecture.

## Ownership

- `apps/docs` owns components, styles, localized public journeys, and assets.
- Axern product modules own API and runtime semantics; examples must match
  those implementations.
- Forge owns publication and cross-project operational coordination, not the
  site design source of truth.

## Acceptance

Homepage changes must be checked in English and Simplified Chinese, light and
dark themes, and desktop and mobile layouts. Keyboard navigation, reduced
motion, code copying, internal links, and the static production build must
remain functional.

## Revisit Condition

Revisit this direction when Axern adopts a shared Cofy-X brand system or when
measured accessibility, performance, or comprehension evidence requires a
different visual model.
