package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrConnectionNotFound is returned when a connection cannot be found.
var ErrConnectionNotFound = errors.New("connection not found")

// Connection represents a unified channel connection instance.
type Connection struct {
	ID             uuid.UUID  `json:"id"`
	WorkspaceID    uuid.UUID  `json:"workspace_id"`
	Name           string     `json:"name"`
	Channel        string     `json:"channel"`
	SenderIdentity string     `json:"sender_identity"`
	Status         string     `json:"status"`
	IsDefault      bool       `json:"is_default"`
	Credentials    []byte     `json:"credentials,omitempty"`
	KeyID          string     `json:"key_id,omitempty"`
	KeyVersion     int        `json:"key_version,omitempty"`
	JID            *string    `json:"jid,omitempty"`
	ConnectedSince *time.Time `json:"connected_since,omitempty"`
	ProxyURL       *string    `json:"proxy_url,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ConnectionRepository manages CRUD operations and credentials crypto for Connection.
type ConnectionRepository struct {
	pool      *pgxpool.Pool
	provider CredentialProvider
}

// NewConnectionRepository creates a new ConnectionRepository.
func NewConnectionRepository(pool *pgxpool.Pool, provider CredentialProvider) *ConnectionRepository {
	return &ConnectionRepository{
		pool:     pool,
		provider: provider,
	}
}

// Create inserts a new connection into the database, encrypting credentials if present.
func (r *ConnectionRepository) Create(ctx context.Context, c *Connection) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}

	if c.ConnectedSince == nil && (c.Status == "connected" || c.Status == "active") {
		now := time.Now().UTC()
		c.ConnectedSince = &now
	}

	var ciphertext []byte
	var keyID string
	var keyVersion int = 1

	if len(c.Credentials) > 0 {
		var err error
		ciphertext, keyID, keyVersion, err = r.provider.Encrypt(c.Credentials)
		if err != nil {
			return fmt.Errorf("failed to encrypt credentials on create: %w", err)
		}
	}

	// If is_default is true, unset other defaults of same workspace and channel
	if c.IsDefault {
		_, err := r.pool.Exec(ctx,
			`UPDATE connections SET is_default = FALSE WHERE workspace_id = $1 AND channel = $2`,
			c.WorkspaceID, c.Channel,
		)
		if err != nil {
			return fmt.Errorf("failed to unset existing default connections: %w", err)
		}
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO connections (
			id, workspace_id, name, channel, sender_identity, status, is_default, 
			credentials, key_id, key_version, jid, connected_since, proxy_url, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
	`,
		c.ID, c.WorkspaceID, c.Name, c.Channel, c.SenderIdentity, c.Status, c.IsDefault,
		ciphertext, keyID, keyVersion, c.JID, c.ConnectedSince, c.ProxyURL,
	)
	if err != nil {
		return err
	}

	return nil
}

// GetByID retrieves a connection by ID, decrypting credentials if present.
func (r *ConnectionRepository) GetByID(ctx context.Context, id uuid.UUID) (*Connection, error) {
	query := `
		SELECT id, workspace_id, name, channel, sender_identity, status, is_default, 
		       credentials, key_id, key_version, jid, connected_since, proxy_url, created_at, updated_at
		FROM connections
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	return r.scanAndDecrypt(row)
}

// GetBySenderIdentity retrieves a connection by sender identity, decrypting credentials if present.
func (r *ConnectionRepository) GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*Connection, error) {
	query := `
		SELECT id, workspace_id, name, channel, sender_identity, status, is_default, 
		       credentials, key_id, key_version, jid, connected_since, proxy_url, created_at, updated_at
		FROM connections
		WHERE workspace_id = $1 AND sender_identity = $2
	`
	row := r.pool.QueryRow(ctx, query, workspaceID, senderIdentity)
	return r.scanAndDecrypt(row)
}

// GetDefaultChannelConnection retrieves the default connection for a given workspace and channel.
func (r *ConnectionRepository) GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*Connection, error) {
	query := `
		SELECT id, workspace_id, name, channel, sender_identity, status, is_default, 
		       credentials, key_id, key_version, jid, connected_since, proxy_url, created_at, updated_at
		FROM connections
		WHERE workspace_id = $1 AND channel = $2 AND is_default = TRUE
	`
	row := r.pool.QueryRow(ctx, query, workspaceID, channel)
	return r.scanAndDecrypt(row)
}

// ListByWorkspace returns all connections for a workspace.
func (r *ConnectionRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*Connection, error) {
	query := `
		SELECT id, workspace_id, name, channel, sender_identity, status, is_default, 
		       credentials, key_id, key_version, jid, connected_since, proxy_url, created_at, updated_at
		FROM connections
		WHERE workspace_id = $1
		ORDER BY created_at
	`
	rows, err := r.pool.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []*Connection
	for rows.Next() {
		c, err := r.scanRowAndDecrypt(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, c)
	}
	return connections, rows.Err()
}

// ListAll returns all connections across all workspaces.
func (r *ConnectionRepository) ListAll(ctx context.Context) ([]*Connection, error) {
	query := `
		SELECT id, workspace_id, name, channel, sender_identity, status, is_default, 
		       credentials, key_id, key_version, jid, connected_since, proxy_url, created_at, updated_at
		FROM connections
		ORDER BY created_at
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []*Connection
	for rows.Next() {
		c, err := r.scanRowAndDecrypt(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, c)
	}
	return connections, rows.Err()
}

// UpdateStatus changes the connection status.
func (r *ConnectionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	var connectedSince interface{}
	if status == "connected" || status == "active" {
		connectedSince = time.Now().UTC()
	}

	// Update status. If WhatsApp Web (whatsmeow), handle 'terminal' status locks.
	// (Note: device.go had logic preventing disconnect from overwriting terminal, let's keep that structure)
	if status == "disconnected" {
		_, err := r.pool.Exec(ctx, `
			UPDATE connections 
			SET status = $2, 
			    connected_since = COALESCE($3, connected_since), 
			    updated_at = NOW()
			WHERE id = $1 AND status != 'terminal'
		`, id, status, connectedSince)
		return err
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE connections 
		SET status = $2, 
		    connected_since = COALESCE($3, connected_since), 
		    updated_at = NOW()
		WHERE id = $1
	`, id, status, connectedSince)
	return err
}

// Delete removes a connection from the database.
func (r *ConnectionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM connections WHERE id = $1", id)
	return err
}

// SaveCredentials encrypts and updates the credentials for a connection.
func (r *ConnectionRepository) SaveCredentials(ctx context.Context, id uuid.UUID, plaintext []byte) error {
	if len(plaintext) == 0 {
		return errors.New("credentials payload cannot be empty")
	}

	ciphertext, keyID, keyVersion, err := r.provider.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt credentials: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE connections 
		SET credentials = $2, key_id = $3, key_version = $4, updated_at = NOW()
		WHERE id = $1
	`, id, ciphertext, keyID, keyVersion)
	return err
}

// GetCredentials retrieves and decrypts the credentials for a connection.
func (r *ConnectionRepository) GetCredentials(ctx context.Context, id uuid.UUID) ([]byte, error) {
	var ciphertext []byte
	var keyID string
	var keyVersion int

	err := r.pool.QueryRow(ctx,
		`SELECT credentials, key_id, key_version FROM connections WHERE id = $1`,
		id,
	).Scan(&ciphertext, &keyID, &keyVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, err
	}

	if len(ciphertext) == 0 {
		return nil, nil
	}

	return r.provider.Decrypt(ciphertext)
}

func (r *ConnectionRepository) scanAndDecrypt(row pgx.Row) (*Connection, error) {
	var c Connection
	var ciphertext []byte
	var keyID sql.NullString
	var keyVersion int
	var connectedSince sql.NullTime
	var jid sql.NullString
	var proxyURL sql.NullString

	err := row.Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.Channel, &c.SenderIdentity, &c.Status, &c.IsDefault,
		&ciphertext, &keyID, &keyVersion, &jid, &connectedSince, &proxyURL, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, err
	}

	c.KeyID = keyID.String
	c.KeyVersion = keyVersion

	if jid.Valid {
		c.JID = &jid.String
	}
	if connectedSince.Valid {
		c.ConnectedSince = &connectedSince.Time
	}
	if proxyURL.Valid {
		c.ProxyURL = &proxyURL.String
	}

	if len(ciphertext) > 0 {
		plaintext, err := r.provider.Decrypt(ciphertext)
		if err != nil {
			slog.Error("failed to decrypt connection credentials", "connection_id", c.ID, "error", err)
		} else {
			c.Credentials = plaintext
		}
	}

	return &c, nil
}

func (r *ConnectionRepository) scanRowAndDecrypt(rows pgx.Rows) (*Connection, error) {
	var c Connection
	var ciphertext []byte
	var keyID sql.NullString
	var keyVersion int
	var connectedSince sql.NullTime
	var jid sql.NullString
	var proxyURL sql.NullString

	err := rows.Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.Channel, &c.SenderIdentity, &c.Status, &c.IsDefault,
		&ciphertext, &keyID, &keyVersion, &jid, &connectedSince, &proxyURL, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	c.KeyID = keyID.String
	c.KeyVersion = keyVersion

	if jid.Valid {
		c.JID = &jid.String
	}
	if connectedSince.Valid {
		c.ConnectedSince = &connectedSince.Time
	}
	if proxyURL.Valid {
		c.ProxyURL = &proxyURL.String
	}

	if len(ciphertext) > 0 {
		plaintext, err := r.provider.Decrypt(ciphertext)
		if err != nil {
			slog.Error("failed to decrypt connection credentials", "connection_id", c.ID, "error", err)
		} else {
			c.Credentials = plaintext
		}
	}

	return &c, nil
}
