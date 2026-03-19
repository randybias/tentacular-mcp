## ADDED Requirements

### Requirement: Write tool descriptions SHALL include expected effects
The system SHALL enrich the `Description` field of all write and destructive tools with a brief statement of expected effects. Descriptions SHALL remain concise (2-3 sentences maximum).

#### Scenario: Write tool description includes effects
- **WHEN** the tool `ns_create` description is retrieved
- **THEN** the description mentions the resources that will be created (namespace, network policies, resource quotas, RBAC)

#### Scenario: Destructive tool description includes safety note
- **WHEN** the tool `ns_delete` description is retrieved
- **THEN** the description includes a note about permanent removal of the namespace and its contents

#### Scenario: Destructive tool cred_rotate description includes safety note
- **WHEN** the tool `cred_rotate` description is retrieved
- **THEN** the description includes a note about credential invalidation

### Requirement: Tool descriptions SHALL include prerequisite constraints
The system SHALL enrich tool descriptions with prerequisite or constraint information where applicable (e.g., "Only works on tentacular-managed namespaces", "Namespace must exist").

#### Scenario: Namespace tool includes managed-by constraint
- **WHEN** the tool `ns_delete` description is retrieved
- **THEN** the description mentions that only tentacular-managed namespaces can be deleted

#### Scenario: Workflow tool includes namespace prerequisite
- **WHEN** the tool `wf_apply` description is retrieved
- **THEN** the description mentions that a managed namespace is required

### Requirement: Descriptions SHALL NOT exceed three sentences
The system SHALL keep all tool descriptions to three sentences or fewer to minimize token consumption in agent context windows.

#### Scenario: Description length check
- **WHEN** any tool description is retrieved from the server's tool list
- **THEN** the description contains no more than three sentences
