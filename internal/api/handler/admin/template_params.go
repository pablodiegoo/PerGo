package admin

import (
	"fmt"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/domain"
)

// ExtractFormTemplateParams extracts dynamic template parameters (param_1, param_2, ...)
// from form values and returns the body description and structured TemplateComponent list.
func ExtractFormTemplateParams(c *echo.Context, templateName string) (string, []domain.TemplateComponent) {
	var params []domain.TemplateParameter
	for i := 1; i <= 50; i++ {
		val := c.FormValue(fmt.Sprintf("param_%d", i))
		if val != "" {
			params = append(params, domain.TemplateParameter{
				Type: "text",
				Text: val,
			})
		}
	}

	if len(params) > 0 {
		componentsList := []domain.TemplateComponent{
			{
				Type:       "body",
				Parameters: params,
			},
		}
		body := fmt.Sprintf("[Template: %s] Params: %v", templateName, params)
		return body, componentsList
	}

	return fmt.Sprintf("[Template: %s]", templateName), nil
}
