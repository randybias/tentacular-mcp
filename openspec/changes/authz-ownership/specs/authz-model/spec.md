## ADDED Requirements

### Requirement: Mode type represents POSIX permission bits
The system SHALL define a Mode type as a uint16 representing POSIX-style permission bits with three scopes (owner, group, others) and three permissions (read, write, execute).

#### Scenario: Mode parses from numeric string
- **WHEN** a mode string "0750" is parsed
- **THEN** the Mode value SHALL represent owner=rwx, group=rx, others=none

#### Scenario: Mode checks individual permissions
- **WHEN** a Mode value of 0750 is checked for owner-write permission
- **THEN** the check SHALL return true
- **WHEN** a Mode value of 0750 is checked for others-read permission
- **THEN** the check SHALL return false

### Requirement: Permission constants are defined
The system SHALL define constants for Read (4), Write (2), and Execute (1) permissions, and for Owner, Group, and Others bit positions.

#### Scenario: Permission bit arithmetic
- **WHEN** owner Read+Write+Execute bits are combined
- **THEN** the result SHALL equal 0700

### Requirement: Preset modes exist for common patterns
The system SHALL provide named presets for common permission patterns.

#### Scenario: Private preset
- **WHEN** the Private preset is used
- **THEN** the mode SHALL be 0700 (owner-only, full access)

#### Scenario: Team preset
- **WHEN** the Team preset is used
- **THEN** the mode SHALL be 0750 (owner full, group read+execute)

#### Scenario: Shared preset
- **WHEN** the Shared preset is used
- **THEN** the mode SHALL be 0755 (owner full, group and others read+execute)

### Requirement: Permission mapping to tentacle operations
The system SHALL map permission types to tentacle operations: read = list/status/health/describe/logs/events/pods/jobs, write = deploy/update/remove/annotate, execute = run/restart.

#### Scenario: Run operation requires execute permission
- **WHEN** a user attempts to run a tentacle
- **THEN** the system SHALL require execute permission for that user's scope (owner, group, or others)
