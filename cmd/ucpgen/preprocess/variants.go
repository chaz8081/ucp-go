package preprocess

// RequiredOps scans a schema's properties for ucp_request markers and
// returns the operations needing distinct request variants
// (preprocess_schemas.py:374-391). A string marker implies create+update;
// a dict marker contributes its keys. Order is unspecified; callers sort.
func RequiredOps(schema map[string]any) []string {
	props, _ := schema["properties"].(map[string]any)
	seen := map[string]bool{}
	for _, v := range props {
		data, ok := v.(map[string]any)
		if !ok {
			continue
		}
		switch m := data["ucp_request"].(type) {
		case string:
			seen["create"], seen["update"] = true, true
		case map[string]any:
			for op := range m {
				seen[op] = true
			}
		}
	}
	var out []string
	for op := range seen {
		out = append(out, op)
	}
	return out
}

// EvalPropInclusion decides whether a property is included and required for
// one operation, per the ucp_request rules (preprocess_schemas.py:394-423):
// string markers — "omit" excludes, "required"/"optional" override the base
// required list; dict markers — the op's value applies, and a missing op
// key or "omit" excludes.
func EvalPropInclusion(name string, data map[string]any, op string, baseRequired []any) (include, required bool) {
	inBase := false
	for _, r := range baseRequired {
		if r == name {
			inBase = true
			break
		}
	}
	if data == nil {
		return true, inBase
	}
	include, required = true, inBase
	switch m := data["ucp_request"].(type) {
	case string:
		switch m {
		case "omit":
			include = false
		case "required":
			required = true
		case "optional":
			required = false
		}
	case map[string]any:
		v, present := m[op]
		if v == "omit" || !present || v == nil {
			include = false
		} else if v == "required" {
			required = true
		} else if v == "optional" {
			required = false
		}
		// any other value: include stays true, required stays base — python parity
	}
	return include, required
}
