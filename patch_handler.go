package main
import (
	"io/ioutil"
	"strings"
)
func main() {
	b, _ := ioutil.ReadFile("internal/api/handler/message.go")
	s := string(b)
	old := `		var routeErr *outbound.RouteError
		if errors.As(err, &routeErr) {`
	new := `		if errors.Is(err, outbound.ErrTemplateNotFound) {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "template_not_found",
				Message: "The requested template was not found in the connection cache",
			})
		}

		var tmplNotAppErr *outbound.ErrTemplateNotApproved
		if errors.As(err, &tmplNotAppErr) {
			reason := "Template is not approved"
			if tmplNotAppErr.RejectionReason != nil {
				reason = *tmplNotAppErr.RejectionReason
			}
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "template_not_approved",
				Message: reason,
			})
		}

		var invalidParamErr *outbound.ErrInvalidTemplateParameters
		if errors.As(err, &invalidParamErr) {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "invalid_template_parameters",
				Message: invalidParamErr.Message,
			})
		}

		var routeErr *outbound.RouteError
		if errors.As(err, &routeErr) {`
	s = strings.Replace(s, old, new, 1)
	ioutil.WriteFile("internal/api/handler/message.go", []byte(s), 0644)
}
