package slack

import "encoding/json"

// Header returns a header block.
func Header(text string) Block {
	return must(map[string]any{
		"type": "header",
		"text": map[string]any{"type": "plain_text", "text": text, "emoji": true},
	})
}

// Section returns a section with markdown text.
func Section(markdown string) Block {
	return must(map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": markdown},
	})
}

// Field returns a single mrkdwn field block (used inside Fields).
func Field(label, value string) Block {
	return must(map[string]any{
		"type": "mrkdwn",
		"text": label + "\n" + value,
	})
}

// MrkdwnField returns a single mrkdwn field block with a single string (no label/value split).
func MrkdwnField(text string) Block {
	return must(map[string]any{
		"type": "mrkdwn",
		"text": text,
	})
}

// Fields returns a section block containing up to 10 two-column fields.
func Fields(fields ...Block) Block {
	raw := make([]json.RawMessage, len(fields))
	for i, f := range fields {
		raw[i] = json.RawMessage(f)
	}
	return must(map[string]any{
		"type":   "section",
		"fields": raw,
	})
}

// SectionWithButton returns a section with markdown text and an inline link button accessory.
func SectionWithButton(text, buttonLabel, url, actionID string) Block {
	return must(map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": text},
		"accessory": map[string]any{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": buttonLabel, "emoji": true},
			"url":       url,
			"action_id": actionID,
		},
	})
}

// Divider returns a divider block.
func Divider() Block { return must(map[string]any{"type": "divider"}) }

// Context returns a context block with one or more markdown strings.
func Context(strs ...string) Block {
	elems := make([]map[string]any, 0, len(strs))
	for _, s := range strs {
		elems = append(elems, map[string]any{"type": "mrkdwn", "text": s})
	}
	return must(map[string]any{"type": "context", "elements": elems})
}

// LinkButton returns an actions block with a single link button.
func LinkButton(text, url string) Block {
	return must(map[string]any{
		"type": "actions",
		"elements": []map[string]any{
			{
				"type": "button",
				"text": map[string]any{"type": "plain_text", "text": text},
				"url":  url,
			},
		},
	})
}

func must(v any) Block {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
