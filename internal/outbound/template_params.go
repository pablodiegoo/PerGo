package outbound

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"github.com/pablojhp.pergo/internal/domain"
)

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
			if tp.Type == "" && tp.Text == "" {
				// Maybe it's a value? 
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
