package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestReplaceUrlTag(t *testing.T) {
	t.Run("Test replacing tag with digest in URL", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))

		t.Logf("TEST: ReplaceUrlTag() with typical case - Should Succeed")
		updateUrl := ReplaceUrlTag("quay.io/test/image:latest", "sha256:01234567879")
		require.Equal(t, updateUrl, "quay.io/test/image@sha256:01234567879")

		t.Logf("TEST: ReplaceUrlTag() with tag with different chars - Should Succeed")
		updateUrl = ReplaceUrlTag("quay.io/test/image:v12-34", "sha256:22ae699979cf")
		require.Equal(t, updateUrl, "quay.io/test/image@sha256:22ae699979cf")

		t.Logf("TEST: ReplaceUrlTag() with no tag on URL - Should Succeed")
		updateUrl = ReplaceUrlTag("quay.io/test-33/image", "sha256:30c8d86826e6")
		require.Equal(t, updateUrl, "quay.io/test-33/image@sha256:30c8d86826e6")

		t.Logf("TEST: ReplaceUrlTag() with no image - Should fail (empty string)")
		updateUrl = ReplaceUrlTag("", "30c8d86826e6")
		require.Equal(t, updateUrl, "")

		t.Logf("TEST: ReplaceUrlTag() with no digest - Should fail (empty string)")
		updateUrl = ReplaceUrlTag("quay.io/test-33/image", "")
		require.Equal(t, updateUrl, "")

		t.Logf("TEST: ReplaceUrlTag() with existing digest (Kyverno case) - same digest - Should return unchanged")
		updateUrl = ReplaceUrlTag("quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c", "sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c")
		require.Equal(t, updateUrl, "quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:bf6f7ea60274882031ad81434aa9c9ac0e4ff280cd1513db239dbbd705b6511c")

		t.Logf("TEST: ReplaceUrlTag() with existing digest (Kyverno case) - different digest - Should replace digest")
		updateUrl = ReplaceUrlTag("quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:olddigest", "sha256:newdigest")
		require.Equal(t, updateUrl, "quay.io/gkm/cache-examples:vector-add-cache-rocm@sha256:newdigest")
	})
}
