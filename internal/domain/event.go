package domain

type EventType string

const (
	EventTypeFlowCompleted EventType = "flow.completed"
)

type FlowCompletedEvent struct {
	Screen    string                 `json:"screen"`
	Data      map[string]interface{} `json:"data"`
	FlowToken string                 `json:"flow_token"`
	ContactID string                 `json:"contact_id"`
	Wamid     string                 `json:"wamid"`
}
