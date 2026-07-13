package session

import (
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

// JIDToPhone extracts the phone number from a whatsmeow JID.
func JIDToPhone(jid waTypes.JID) string {
	return jid.User
}
