package contextwindow

import "testing"

func TestRetryGenerationDoesNotReuseCompletedIdentity(t *testing.T) {
	retry := PromptVersion + ":retry:example"
	if GenerationVersion(retry) != retry {
		t.Fatal("retry lost fresh identity")
	}
	if GenerationVersion("old-version") != PromptVersion {
		t.Fatal("ordinary generation should use current prompt")
	}
}
