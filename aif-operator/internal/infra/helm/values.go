package helm

import (
	"encoding/json"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func ConvertHelmValues(in map[string]apixv1.JSON) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		var val interface{}
		if err := json.Unmarshal(v.Raw, &val); err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}
