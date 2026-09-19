package runtime_api

import "testing"

func TestAssignmentRequiresInstalledCapabilities(t *testing.T) {
	raw := []byte(`{"ollama":{"models":[{"name":"chat"},{"name":"embed"}]},"details":{"chat":{"capabilities":["completion","tools"]},"embed":{"capabilities":["embedding"]}},"whisper":[{"name":"small","installed":true},{"name":"large","installed":false},{"name":"large-v3-turbo","installed":true}]}`)
	for _, tc := range []struct {
		key, model string
		valid      bool
	}{
		{"agent.model", "chat", true}, {"ml.context_model", "chat", true}, {"agent.model", "embed", false}, {"agent.model", "missing", false}, {"whisper.model", "small", true}, {"whisper.model", "large", false}, {"whisper.model", "large-v3-turbo", true},
	} {
		if err := validateAssignment(raw, tc.key, tc.model); (err == nil) != tc.valid {
			t.Errorf("%s/%s: %v", tc.key, tc.model, err)
		}
	}
}
