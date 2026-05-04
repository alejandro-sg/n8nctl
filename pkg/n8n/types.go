package n8n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

type ID string

func (id ID) String() string {
	return string(id)
}

func (id ID) IsZero() bool {
	return id == ""
}

func (id *ID) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}

	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*id = ID(s)
		return nil
	}

	*id = ID(string(data))
	return nil
}

func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(id))
}

type CredentialReference struct {
	ID   ID     `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type Node struct {
	ID               ID                             `json:"id,omitempty"`
	Name             string                         `json:"name"`
	WebhookID        string                         `json:"webhookId,omitempty"`
	Disabled         bool                           `json:"disabled,omitempty"`
	NotesInFlow      bool                           `json:"notesInFlow,omitempty"`
	Notes            string                         `json:"notes,omitempty"`
	Type             string                         `json:"type,omitempty"`
	TypeVersion      any                            `json:"typeVersion,omitempty"`
	ExecuteOnce      bool                           `json:"executeOnce,omitempty"`
	AlwaysOutputData bool                           `json:"alwaysOutputData,omitempty"`
	RetryOnFail      bool                           `json:"retryOnFail,omitempty"`
	MaxTries         int                            `json:"maxTries,omitempty"`
	WaitBetweenTries int                            `json:"waitBetweenTries,omitempty"`
	ContinueOnFail   bool                           `json:"continueOnFail,omitempty"`
	OnError          string                         `json:"onError,omitempty"`
	Position         []float64                      `json:"position,omitempty"`
	Parameters       map[string]any                 `json:"parameters,omitempty"`
	Credentials      map[string]CredentialReference `json:"credentials,omitempty"`
}

type Workflow struct {
	ID            ID             `json:"id,omitempty"`
	ProjectID     ID             `json:"projectId,omitempty"`
	Name          string         `json:"name"`
	Active        bool           `json:"active,omitempty"`
	CreatedAt     *time.Time     `json:"createdAt,omitempty"`
	UpdatedAt     *time.Time     `json:"updatedAt,omitempty"`
	IsArchived    bool           `json:"isArchived,omitempty"`
	VersionID     string         `json:"versionId,omitempty"`
	TriggerCount  int            `json:"triggerCount,omitempty"`
	Nodes         []Node         `json:"nodes"`
	Connections   map[string]any `json:"connections"`
	Settings      map[string]any `json:"settings"`
	StaticData    any            `json:"staticData,omitempty"`
	PinData       map[string]any `json:"pinData,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
	Tags          []Tag          `json:"tags,omitempty"`
	Shared        []SharedItem   `json:"shared,omitempty"`
	ActiveVersion map[string]any `json:"activeVersion,omitempty"`
}

type Tag struct {
	ID   ID     `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type SharedItem struct {
	Role       string         `json:"role,omitempty"`
	WorkflowID ID             `json:"workflowId,omitempty"`
	ProjectID  ID             `json:"projectId,omitempty"`
	Project    map[string]any `json:"project,omitempty"`
}

type WorkflowListResponse struct {
	Data       []Workflow `json:"data"`
	NextCursor string     `json:"nextCursor"`
}

type Execution struct {
	ID         ID             `json:"id,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Finished   bool           `json:"finished,omitempty"`
	Mode       string         `json:"mode,omitempty"`
	RetryOf    ID             `json:"retryOf,omitempty"`
	StartedAt  *time.Time     `json:"startedAt,omitempty"`
	StoppedAt  *time.Time     `json:"stoppedAt,omitempty"`
	WorkflowID ID             `json:"workflowId,omitempty"`
	WaitTill   *time.Time     `json:"waitTill,omitempty"`
	CustomData map[string]any `json:"customData,omitempty"`
	Status     string         `json:"status,omitempty"`
}

func (e Execution) WorkflowIDString() string {
	return e.WorkflowID.String()
}

type ExecutionListResponse struct {
	Data       []Execution `json:"data"`
	NextCursor string      `json:"nextCursor"`
}

type ExecutionRunResponse struct {
	ExecutionID ID             `json:"executionId,omitempty"`
	ID          ID             `json:"id,omitempty"`
	Finished    bool           `json:"finished,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Status      string         `json:"status,omitempty"`
}

func (r ExecutionRunResponse) ResolvedExecutionID() ID {
	if !r.ExecutionID.IsZero() {
		return r.ExecutionID
	}
	return r.ID
}

type Project struct {
	ID        ID             `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Role      string         `json:"role,omitempty"`
	Type      string         `json:"type,omitempty"`
	CreatedAt *time.Time     `json:"createdAt,omitempty"`
	UpdatedAt *time.Time     `json:"updatedAt,omitempty"`
	Raw       map[string]any `json:"-"`
}

type ProjectListResponse struct {
	Data       []Project `json:"data"`
	NextCursor string    `json:"nextCursor"`
}

type Credential struct {
	ID        ID             `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Type      string         `json:"type,omitempty"`
	ProjectID ID             `json:"projectId,omitempty"`
	Shared    []SharedItem   `json:"shared,omitempty"`
	CreatedAt *time.Time     `json:"createdAt,omitempty"`
	UpdatedAt *time.Time     `json:"updatedAt,omitempty"`
	Raw       map[string]any `json:"-"`
}

type CredentialListResponse struct {
	Data       []Credential `json:"data"`
	NextCursor string       `json:"nextCursor"`
}

type CredentialSchema struct {
	Type        string         `json:"type,omitempty"`
	DisplayName string         `json:"displayName,omitempty"`
	Properties  []Property     `json:"properties,omitempty"`
	Raw         map[string]any `json:"-"`
}

type Property struct {
	Name        string         `json:"name,omitempty"`
	DisplayName string         `json:"displayName,omitempty"`
	Type        string         `json:"type,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Default     any            `json:"default,omitempty"`
	Options     []Property     `json:"options,omitempty"`
	Raw         map[string]any `json:"-"`
}

type WorkflowDependency struct {
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func MustPrettyJSON(value any) string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}
