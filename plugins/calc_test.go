package plugins

import (
	"strings"
	"testing"
)

func TestCalculateExpression(t *testing.T) {
	for expression, want := range map[string]string{
		"2+2*5":    "12",
		"(3+4)*2":  "14",
		"-2.5 + 1": "-1.5",
	} {
		got, err := calculateExpression(expression)
		if err != nil || !strings.HasSuffix(got, "= "+want) {
			t.Fatalf("calculateExpression(%q) = %q, %v", expression, got, err)
		}
	}
}

func TestCalculateExpressionRejectsUnsafeOrInvalidInput(t *testing.T) {
	for _, expression := range []string{"", "2/0", "os.execute(1)", "2+"} {
		if _, err := calculateExpression(expression); err == nil {
			t.Fatalf("calculateExpression(%q) unexpectedly succeeded", expression)
		}
	}
}

func TestConvertExpression(t *testing.T) {
	got, err := convertExpression("10 km to mi")
	if err != nil || !strings.Contains(got, "6.213712 mi") {
		t.Fatalf("convertExpression returned %q, %v", got, err)
	}
	got, err = convertExpression("32 f to c")
	if err != nil || !strings.HasSuffix(got, "= 0 c") {
		t.Fatalf("temperature conversion returned %q, %v", got, err)
	}
}
