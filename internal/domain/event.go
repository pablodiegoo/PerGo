package domain

type EventType string

const (
	EventTypeFlowCompleted EventType = "flow.completed"
	EventTypeOrderCreated  EventType = "order.created"
)

type FlowCompletedEvent struct {
	Screen    string                 `json:"screen"`
	Data      map[string]interface{} `json:"data"`
	FlowToken string                 `json:"flow_token"`
	ContactID string                 `json:"contact_id"`
	Wamid     string                 `json:"wamid"`
	TraceID   string                 `json:"trace_id,omitempty"`
}

type OrderProductItem struct {
	ProductRetailerID string  `json:"product_retailer_id"`
	Quantity          int     `json:"quantity"`
	ItemPrice         float64 `json:"item_price"`
	Currency          string  `json:"currency"`
}

type OrderCreatedEvent struct {
	OrderID    string             `json:"order_id"`
	CatalogID  string             `json:"catalog_id"`
	Items      []OrderProductItem `json:"items"`
	TotalPrice float64            `json:"total_price"`
	Currency   string             `json:"currency"`
	Wamid      string             `json:"wamid"`
	ContactID  string             `json:"contact_id"`
	TraceID    string             `json:"trace_id"`
}
