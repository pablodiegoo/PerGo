package repository

import "errors"

// ErrInvalidWorkspaceID is returned when an operation is attempted with uuid.Nil as the workspace ID.
var ErrInvalidWorkspaceID = errors.New("invalid workspace id")
