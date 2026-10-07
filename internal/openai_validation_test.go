package internal

import "testing"

func TestValidateModel(t *testing.T) {
	for _, model := range []string{"gpt-6-luna", "gpt-5.4-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4o", "gpt-4o-mini"} {
		if err := ValidateModel(model); err != nil {
			t.Fatalf("ValidateModel(%q) error = %v", model, err)
		}
	}
	for _, model := range []string{"", "GPT-4o", "gpt-3.5-turbo", "o1", "o1-mini", "o3", "o3-mini", "o4-mini", "gpt-4.1-nano", "gpt-5", "gpt-5-mini", "gpt-5-nano"} {
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
