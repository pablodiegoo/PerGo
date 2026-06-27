package types

type NoSuchKey struct {
	Message *string
	Code    *string
}

func (e *NoSuchKey) Error() string {
	if e.Message != nil {
		return *e.Message
	}
	return "NoSuchKey"
}
