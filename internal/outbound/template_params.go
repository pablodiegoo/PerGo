package outbound

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pablojhp.pergo/internal/domain"
)

var templateVarRegex = regexp.MustCompile(`\{\{(\d+)\}\}`)

// NormalizeTemplateParams converts map format to ordered positional array by sorting keys numerically.
// It also handles arrays of strings or structs, and returns []domain.TemplateParameter.
func NormalizeTemplateParams(params interface{}) ([]domain.TemplateParameter, error) {
	if params == nil {
		return nil, nil
	}

	toParam := func(v interface{}) (domain.TemplateParameter, error) {
		switch val := v.(type) {
		case string:
			return domain.TemplateParameter{Type: "text", Text: val}, nil
		case map[string]interface{}:
			// Could be {"type": "text", "text": "foo"}
			b, err := json.Marshal(val)
			if err != nil {
				return domain.TemplateParameter{}, err
			}
			var tp domain.TemplateParameter
			if err := json.Unmarshal(b, &tp); err != nil {
				return domain.TemplateParameter{}, err
			}
			if tp.Type == "" && tp.Text != "" {
				tp.Type = "text"
			}
			return tp, nil
		case domain.TemplateParameter:
			return val, nil
		default:
			return domain.TemplateParameter{Type: "text", Text: fmt.Sprintf("%v", val)}, nil
		}
	}

	switch v := params.(type) {
	case []interface{}:
		var out []domain.TemplateParameter
		for _, item := range v {
			p, err := toParam(item)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	case map[string]interface{}:
		// Check if it's a key-value map like {"1": "John", "2": "101"}
		// Or maybe it's a single TemplateParameter?
		// We expect keys to be numeric for key-value map
		var keys []int
		for k := range v {
			ki, err := strconv.Atoi(k)
			if err != nil {
				// If not numeric, maybe this map IS the array's single item? No, the plan says {"1": "John"}.
				return nil, fmt.Errorf("invalid map key: %s (must be numeric)", k)
			}
			keys = append(keys, ki)
		}
		sort.Ints(keys)
		var out []domain.TemplateParameter
		for _, k := range keys {
			p, err := toParam(v[strconv.Itoa(k)])
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	case []domain.TemplateParameter:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported parameter format: %T", params)
	}
}

// CountTemplateVariables extracts the number of expected variables for each component type
// from the raw Meta component JSON. It parses {{N}} placeholders from text fields and URL buttons.
func CountTemplateVariables(components json.RawMessage) (map[string]int, error) {
	if len(components) == 0 {
		return nil, nil
	}

	var parsed []map[string]interface{}
	if err := json.Unmarshal(components, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse template components: %w", err)
	}

	expected := make(map[string]int)

	for _, c := range parsed {
		cType, ok := c["type"].(string)
		if !ok {
			continue
		}
		cType = strings.ToLower(cType)

		switch cType {
		case "header", "body":
			if text, ok := c["text"].(string); ok {
				matches := templateVarRegex.FindAllStringSubmatch(text, -1)
				uniqueVars := make(map[string]bool)
				for _, match := range matches {
					if len(match) > 1 {
						uniqueVars[match[1]] = true
					}
				}
				if len(uniqueVars) > 0 {
					expected[cType] = len(uniqueVars)
				}
			}
		case "buttons":
			if buttons, ok := c["buttons"].([]interface{}); ok {
				var count int
				for _, btnIntf := range buttons {
					if btn, ok := btnIntf.(map[string]interface{}); ok {
						if btnType, ok := btn["type"].(string); ok && strings.ToLower(btnType) == "url" {
							if url, ok := btn["url"].(string); ok {
								if templateVarRegex.MatchString(url) {
									count++
								}
							}
						}
					}
				}
				if count > 0 {
					expected["button"] = count
				}
			}
		}
	}

	return expected, nil
}

// ValidateParameterCounts compares the provided parameter counts against the expected counts
// extracted from the template components.
func ValidateParameterCounts(provided []domain.TemplateComponent, expected map[string]int) error {
	if len(expected) == 0 {
		return nil
	}

	// Map provided counts
	providedCounts := make(map[string]int)
	for _, c := range provided {
		cType := strings.ToLower(c.Type)
		// Parameters are already normalized to []domain.TemplateParameter
		if params, ok := c.Parameters.([]domain.TemplateParameter); ok {
			providedCounts[cType] = len(params)
		} else if params, ok := c.Parameters.([]interface{}); ok {
			providedCounts[cType] = len(params)
		}
	}

	for cType, expectedCount := range expected {
		providedCount := providedCounts[cType]
		if providedCount != expectedCount {
			return fmt.Errorf("component %s expects %d parameters, got %d", strings.ToUpper(cType), expectedCount, providedCount)
		}
	}

	return nil
}
