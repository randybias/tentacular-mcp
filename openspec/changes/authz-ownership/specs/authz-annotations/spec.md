## ADDED Requirements

### Requirement: Authz annotation constants defined under tentacular.io
The system SHALL define annotation constants for all authz annotations under the `tentacular.io/` prefix: owner-sub, owner-email, owner-name, group, mode, auth-provider, idp-provider, default-mode, default-group, created-at, updated-at, updated-by-sub, updated-by-email.

#### Scenario: Annotation keys use tentacular.io prefix
- **WHEN** the annotation constants are referenced
- **THEN** all keys SHALL start with `tentacular.io/` (e.g., `tentacular.io/owner-sub`)

### Requirement: Existing annotations migrate from tentacular.dev to tentacular.io
The system SHALL migrate the following annotations: `tentacular.dev/environment` to `tentacular.io/environment`, `tentacular.dev/tags` to `tentacular.io/tags`, `tentacular.dev/cron-schedule` to `tentacular.io/cron-schedule`.

#### Scenario: Read with fallback
- **WHEN** reading an annotation and only the old `tentacular.dev/*` key is present
- **THEN** the system SHALL return the value from the old key

#### Scenario: Read prefers new key
- **WHEN** reading an annotation and both `tentacular.dev/*` and `tentacular.io/*` keys are present
- **THEN** the system SHALL return the value from the `tentacular.io/*` key

#### Scenario: Write uses new key
- **WHEN** writing an annotation
- **THEN** the system SHALL write only the `tentacular.io/*` key

### Requirement: Dropped annotations not written
The system SHALL NOT write `tentacular.dev/owner` or `tentacular.dev/team` annotations. These are replaced by `tentacular.io/owner-sub`, `tentacular.io/owner-email`, `tentacular.io/owner-name`, and `tentacular.io/group`.

#### Scenario: Old owner annotation ignored on read
- **WHEN** a deployment has `tentacular.dev/owner` but no `tentacular.io/owner-sub`
- **THEN** the system SHALL NOT use the old owner value for authz evaluation
