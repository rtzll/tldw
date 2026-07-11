package internal

import "testing"

func TestValidateModel(t *testing.T) {
	for _, model := range []string{"gpt-5.4-mini", "gpt-4o"} {
		if err := ValidateModel(model); err != nil {
			t.Fatalf("ValidateModel(%q) error = %v", model, err)
		}
	}
	for _, model := range []string{"", "GPT-4o", "gpt-3.5-turbo"} {
		if err := ValidateModel(model); err == nil {
			t.Fatalf("ValidateModel(%q) succeeded", model)
		}
	}
}

func TestValidateOpenAIAPIKey(t *testing.T) {
	if err := ValidateOpenAIAPIKey("sk-test123"); err != nil {
		t.Fatalf("ValidateOpenAIAPIKey() error = %v", err)
	}
	if err := ValidateOpenAIAPIKey(""); err == nil {
		t.Fatal("ValidateOpenAIAPIKey() accepted an empty key")
	}
}
