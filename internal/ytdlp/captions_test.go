package ytdlp

import "testing"

func TestBuildSubLangs(t *testing.T) {
	tests := []struct {
		name         string
		preferred    []string
		originalLang string
		wantPrimary  string
		wantFallback string
	}{
		{"no preferred", nil, "", "en.*,en", ""},
		{"english preferred", []string{"en-US", "en"}, "", "en-US", "en.*,en"},
		{"non-english preferred", []string{"de"}, "de", "de", "en.*,en"},
		{"multiple non-english", []string{"de", "fr"}, "de", "de", "en.*,en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary, fallback := buildSubLangs(tt.preferred, tt.originalLang)
			if primary != tt.wantPrimary {
				t.Errorf("buildSubLangs() primary = %q, want %q", primary, tt.wantPrimary)
			}
			if fallback != tt.wantFallback {
				t.Errorf("buildSubLangs() fallback = %q, want %q", fallback, tt.wantFallback)
			}
		})
	}
}

func TestPrioritizeCaptionLanguages(t *testing.T) {
	tests := []struct {
		name         string
		preferred    []string
		originalLang string
		want         []string
	}{
		{"empty", nil, "", nil},
		{"english first match", []string{"en-US", "en-GB", "de"}, "", []string{"en-US"}},
		{"original lang", []string{"de", "fr"}, "de", []string{"de"}},
		{"first non-english", []string{"de", "fr"}, "es", []string{"de"}},
		{"dedup", []string{"de", "de", "fr"}, "es", []string{"de"}},
		{"skip live_chat", []string{"live_chat", "de"}, "es", []string{"de"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prioritizeCaptionLanguages(tt.preferred, tt.originalLang)
			if len(got) != len(tt.want) {
				t.Errorf("prioritizeCaptionLanguages() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("prioritizeCaptionLanguages() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestExtractCaptionLanguages(t *testing.T) {
	tests := []struct {
		name         string
		subtitles    map[string]any
		autoCaptions map[string]any
		want         []string
	}{
		{"empty", nil, nil, nil},
		{"manual only", map[string]any{"en": nil, "de": nil}, nil, []string{"de", "en"}},
		{"auto only", nil, map[string]any{"en": nil, "fr": nil}, []string{"en", "fr"}},
		{"combined", map[string]any{"en": nil}, map[string]any{"de": nil}, []string{"de", "en"}},
		{"skip live_chat", map[string]any{"en": nil, "live_chat": nil}, nil, []string{"en"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCaptionLanguages(tt.subtitles, tt.autoCaptions)
			if len(got) != len(tt.want) {
				t.Errorf("extractCaptionLanguages() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractCaptionLanguages() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestSetSubLangsArg(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		value   string
		want    []string
		wantErr bool
	}{
		{
			name:    "update existing",
			args:    []string{"--write-subs", "--sub-langs", "en", "--skip-download"},
			value:   "en.*,en",
			want:    []string{"--write-subs", "--sub-langs", "en.*,en", "--skip-download"},
			wantErr: false,
		},
		{
			name:    "flag not found",
			args:    []string{"--write-subs", "--skip-download"},
			value:   "en",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := make([]string, len(tt.args))
			copy(args, tt.args)
			err := setSubLangsArg(args, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setSubLangsArg() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				for i := range args {
					if args[i] != tt.want[i] {
						t.Errorf("setSubLangsArg() args[%d] = %q, want %q", i, args[i], tt.want[i])
					}
				}
			}
		})
	}
}
