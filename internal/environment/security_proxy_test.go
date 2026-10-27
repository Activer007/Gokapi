//go:build test

package environment

import (
	"os"
	"testing"
)

func TestSEC013DockerTrustRequiresOptIn(t *testing.T) {
	t.Setenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY", "")
	if err := os.Unsetenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY"); err != nil {
		t.Fatal(err)
	}
	if !New().DisableDockerTrustedProxy {
		t.Fatal("Docker subnet is implicitly trusted by default")
	}
	t.Setenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY", "false")
	if New().DisableDockerTrustedProxy {
		t.Fatal("explicit legacy opt-in was ignored")
	}
}
