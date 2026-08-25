package litellm

import (
	"regexp"
	"testing"
)

func TestUUID4ReturnsRFC4122Version4UUID(t *testing.T) {
	value, err := UUID4()
	if err != nil {
		t.Fatalf("UUID4() error = %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(value) {
		t.Fatalf("UUID4() = %q, want RFC 4122 version 4 UUID", value)
	}
}
