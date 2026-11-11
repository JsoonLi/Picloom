package desktop

import (
	"testing"

	"image2ai/internal/locale"
)

func TestLanguageFollowsComputerUnlessChosen(t *testing.T) {
	cases := []struct {
		setting   string
		preferred []string
		want      string
	}{
		{"system", []string{"en-US", "zh-Hans-CN"}, locale.English},
		{"", []string{"en-GB"}, locale.English},
		{"system", []string{"zh-Hans-CN", "en-US"}, locale.Chinese},
		{"system", []string{"zh-Hant-TW"}, locale.Chinese},
		{"system", []string{"fr-FR", "en-US"}, locale.English},
		{"system", []string{"fr-FR", "de-DE"}, locale.Chinese},
		{"system", nil, locale.Chinese},
		{locale.Chinese, []string{"en-US"}, locale.Chinese},
		{locale.English, []string{"zh-Hans-CN"}, locale.English},
	}
	for _, c := range cases {
		if got := Language(c.setting, c.preferred); got != c.want {
			t.Errorf("Language(%q, %v) = %q, want %q", c.setting, c.preferred, got, c.want)
		}
	}
}
