package maxclient

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DescribeAttachments lists attachment field names and JSON value types.
// It never includes the values, so a phone number or a vCard cannot appear.
func DescribeAttachments(items []Attachment) string {
	if len(items) == 0 {
		return "attachments=0"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		var payload any
		if len(item.Payload) > 0 {
			_ = json.Unmarshal(item.Payload, &payload)
		}
		parts = append(parts, fmt.Sprintf("type=%s payload=%s", item.Type, describeValue(payload)))
	}
	return strings.Join(parts, "; ")
}

// DescribeUpdate lists the fields of a callback update and the types of their values.
// chat_type is appended as its value when MAX sent one. Payload text and message ids are not included.
func DescribeUpdate(u *Update) string {
	if u == nil {
		return "update=null"
	}
	raw, err := json.Marshal(u)
	if err != nil {
		return "update=other"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "update=other"
	}
	shape := describeValue(v)
	if u.Message != nil && u.Message.Recipient != nil && u.Message.Recipient.ChatType != "" {
		shape += " chat_type=" + u.Message.Recipient.ChatType
	}
	return shape
}

func describeValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		bits := make([]string, 0, len(keys))
		for _, k := range keys {
			bits = append(bits, k+"="+describeValue(x[k]))
		}
		return "object(" + strings.Join(bits, ",") + ")"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return "other"
	}
}
