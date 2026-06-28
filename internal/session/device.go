package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	waTypes "go.mau.fi/whatsmeow/types"
)

// DeviceStatus represents the connection state of a WhatsApp device session.
type DeviceStatus string

const (
	DeviceStatusConnected    DeviceStatus = "connected"
	DeviceStatusDisconnected DeviceStatus = "disconnected"
	DeviceStatusTerminal     DeviceStatus = "terminal"
	DeviceStatusPending      DeviceStatus = "pending"
)

// Device represents a WhatsApp Web device (whatsmeow session) persisted in PostgreSQL.
// Maps to the `devices` table (001 + 003 migrations).
type Device struct {
	ID             uuid.UUID    `json:"id"`
	WorkspaceID    uuid.UUID    `json:"workspace_id"`
	Channel        string       `json:"channel"`         // "whatsapp", "waba", "telegram"
	JID            string       `json:"jid"`             // whatsmeow JID, e.g. "5511999999999@s.whatsapp.net"
	Phone          string       `json:"phone"`           // phone number
	Status         DeviceStatus `json:"status"`
	ConnectedSince *time.Time   `json:"connected_since"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// DeviceRepository provides CRUD operations for WhatsApp devices.
type DeviceRepository struct {
	pool *pgxpool.Pool
}

// NewDeviceRepository creates a device repository backed by pgxpool.
func NewDeviceRepository(pool *pgxpool.Pool) *DeviceRepository {
	return &DeviceRepository{pool: pool}
}

// Create persists a new device to the database.
func (r *DeviceRepository) Create(ctx context.Context, d *Device) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO devices (id, workspace_id, channel, device_id, jid, phone, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (jid) WHERE jid IS NOT NULL DO UPDATE SET
			status = EXCLUDED.status,
			updated_at = NOW()
	`, d.ID, d.WorkspaceID, d.Channel, d.ID.String(), d.JID, d.Phone, d.Status)
	return err
}

// GetByID retrieves a device by its UUID.
func (r *DeviceRepository) GetByID(ctx context.Context, id uuid.UUID) (*Device, error) {
	return r.getOne(ctx, "SELECT "+deviceColumns()+" FROM devices WHERE id = $1", id)
}

// GetByJID retrieves a device by its WhatsApp JID.
func (r *DeviceRepository) GetByJID(ctx context.Context, jid string) (*Device, error) {
	return r.getOne(ctx, "SELECT "+deviceColumns()+" FROM devices WHERE jid = $1", jid)
}

// ListByWorkspace returns all devices for a workspace.
func (r *DeviceRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*Device, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+deviceColumns()+" FROM devices WHERE workspace_id = $1 ORDER BY created_at", workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// ListAll returns all devices across all workspaces.
func (r *DeviceRepository) ListAll(ctx context.Context) ([]*Device, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+deviceColumns()+" FROM devices WHERE jid IS NOT NULL ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// UpdateStatus changes the device's connection status.
func (r *DeviceRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status DeviceStatus) error {
	var connectedSince interface{}
	if status == DeviceStatusConnected {
		t := time.Now().UTC()
		connectedSince = t
	}

	if status == DeviceStatusDisconnected {
		_, err := r.pool.Exec(ctx, `
			UPDATE devices SET status = $2, connected_since = COALESCE($3, connected_since), updated_at = NOW()
			WHERE id = $1 AND status != 'terminal'
		`, id, status, connectedSince)
		return err
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE devices SET status = $2, connected_since = COALESCE($3, connected_since), updated_at = NOW()
		WHERE id = $1
	`, id, status, connectedSince)
	return err
}

// Delete removes a device from the database.
func (r *DeviceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM devices WHERE id = $1", id)
	return err
}

// JIDToPhone extracts the phone number from a whatsmeow JID.
func JIDToPhone(jid waTypes.JID) string {
	return jid.User
}

func (r *DeviceRepository) getOne(ctx context.Context, query string, args ...interface{}) (*Device, error) {
	row := r.pool.QueryRow(ctx, query, args...)
	d, err := scanDeviceRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func deviceColumns() string {
	return "id, workspace_id, channel, jid, phone, status, connected_since, created_at, updated_at"
}

func scanDevice(rows pgx.Rows) (*Device, error) {
	var d Device
	var connectedSince sql.NullTime
	err := rows.Scan(&d.ID, &d.WorkspaceID, &d.Channel, &d.JID, &d.Phone, &d.Status, &connectedSince, &d.CreatedAt, &d.UpdatedAt)
	if connectedSince.Valid {
		d.ConnectedSince = &connectedSince.Time
	}
	return &d, err
}

func scanDeviceRow(row pgx.Row) (*Device, error) {
	var d Device
	var connectedSince sql.NullTime
	err := row.Scan(&d.ID, &d.WorkspaceID, &d.Channel, &d.JID, &d.Phone, &d.Status, &connectedSince, &d.CreatedAt, &d.UpdatedAt)
	if connectedSince.Valid {
		d.ConnectedSince = &connectedSince.Time
	}
	return &d, err
}
