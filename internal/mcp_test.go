package internal

import "testing"

func TestMCPToolsDeclareOutputSchemasAndReadOnlyAnnotations(t *testing.T) {
	server := NewMCPServer(&App{config: &Config{}})
	tools := server.GetServer().ListTools()

	wantFields := map[string][]string{
		"get_youtube_metadata": {
			"title",
			"channel",
			"duration_seconds",
			"description",
			"has_captions",
		},
		"get_youtube_transcript": {
			"url",
			"transcript",
			"source",
			"include_timestamps",
		},
		"transcribe_youtube_whisper": {
			"url",
			"transcript",
			"source",
			"include_timestamps",
		},
	}

	for name, fields := range wantFields {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("tool %q is not registered", name)
		}

		if tool.Tool.OutputSchema.Type != "object" {
			t.Errorf("%s output schema type = %q, want object", name, tool.Tool.OutputSchema.Type)
		}

		for _, field := range fields {
			if _, ok := tool.Tool.OutputSchema.Properties[field]; !ok {
				t.Errorf("%s output schema missing field %q", name, field)
			}
		}

		if tool.Tool.Annotations.ReadOnlyHint == nil || !*tool.Tool.Annotations.ReadOnlyHint {
			t.Errorf("%s readOnlyHint is not true", name)
		}

		if tool.Tool.Annotations.DestructiveHint == nil || *tool.Tool.Annotations.DestructiveHint {
			t.Errorf("%s destructiveHint is not false", name)
		}

		if tool.Tool.Annotations.OpenWorldHint == nil || !*tool.Tool.Annotations.OpenWorldHint {
			t.Errorf("%s openWorldHint is not true", name)
		}
	}
}
