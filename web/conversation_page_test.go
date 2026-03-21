package web

import (
	"strings"
	"testing"
)

func TestConversationPageEmitsRawScript(t *testing.T) {
	t.Parallel()

	rendered := ConversationPage().Render()
	if !strings.Contains(rendered, "fetch('/conversation/data'") {
		t.Fatalf("expected raw fetch call in rendered page, got: %s", rendered)
	}
	if strings.Contains(rendered, "fetch(&#39;/conversation/data&#39;") {
		t.Fatalf("conversation page script was HTML-escaped: %s", rendered)
	}
	if !strings.Contains(rendered, `data-theme="dark"`) {
		t.Fatalf("expected dark theme default in rendered page, got: %s", rendered)
	}
}
