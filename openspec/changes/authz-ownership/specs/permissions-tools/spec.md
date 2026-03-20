## ADDED Requirements

### Requirement: permissions_get returns current permissions for a tentacle
The system SHALL provide a `permissions_get` MCP tool that returns the owner, group, and mode for a deployed tentacle.

#### Scenario: Get permissions for owned tentacle
- **WHEN** an authenticated user calls permissions_get for a tentacle they have read access to
- **THEN** the tool SHALL return owner-sub, owner-email, owner-name, group, and mode

#### Scenario: Get permissions without read access
- **WHEN** an OIDC-authenticated user calls permissions_get for a tentacle they have no read access to
- **THEN** the tool SHALL return a permission-denied error

### Requirement: permissions_set modifies permissions for a tentacle
The system SHALL provide a `permissions_set` MCP tool that allows setting owner, group, or mode on a deployed tentacle.

#### Scenario: Owner changes mode
- **WHEN** the owner of a tentacle calls permissions_set with a new mode value
- **THEN** the tool SHALL update the `tentacular.io/mode` annotation and set updated-at, updated-by-sub, updated-by-email

#### Scenario: Owner changes group
- **WHEN** the owner of a tentacle calls permissions_set with a new group value
- **THEN** the tool SHALL update the `tentacular.io/group` annotation

#### Scenario: Non-owner cannot change permissions
- **WHEN** a non-owner OIDC-authenticated user calls permissions_set
- **THEN** the tool SHALL return a permission-denied error (only the owner can change permissions)

#### Scenario: Bearer token can change permissions
- **WHEN** a bearer-token-authenticated user calls permissions_set
- **THEN** the tool SHALL allow the change (bearer bypass)

### Requirement: permissions tools are registered
The permissions_get and permissions_set tools SHALL be registered in the MCP tool registry.

#### Scenario: Tools appear in tool list
- **WHEN** the MCP server starts
- **THEN** permissions_get and permissions_set SHALL be listed among available tools
