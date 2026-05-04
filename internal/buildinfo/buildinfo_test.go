package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfoUsesTaggedVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.2.3",
		},
	}

	got := versionFromBuildInfo(info, map[string]string{})

	if got != "1.2.3" {
		t.Fatalf("expected trimmed tag version, got %q", got)
	}
}

func TestVersionFromBuildInfoUsesDevelopmentVersionForDevelBuilds(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "(devel)",
		},
	}

	got := versionFromBuildInfo(info, map[string]string{})

	if got != DevelopmentVersion {
		t.Fatalf("expected development version, got %q", got)
	}
}

func TestVersionFromBuildInfoMarksDirtyBuilds(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v0.1.0",
		},
	}

	got := versionFromBuildInfo(info, map[string]string{
		"vcs.modified": "true",
	})

	if got != "0.1.0+dirty" {
		t.Fatalf("expected dirty suffix, got %q", got)
	}
}

func TestVersionFromBuildInfoNormalizesPseudoVersions(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v0.0.0-20260424061934-a29b4bbadc2d+dirty",
		},
	}

	got := versionFromBuildInfo(info, map[string]string{
		"vcs.modified": "true",
	})

	if got != DevelopmentVersion+"+dirty" {
		t.Fatalf("expected normalized development version, got %q", got)
	}
}

func TestVersionFromBuildInfoNormalizesPseudoVersionFromTaggedBase(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v0.1.1-0.20260424061934-a29b4bbadc2d",
		},
	}

	got := versionFromBuildInfo(info, map[string]string{})

	if got != "0.1.1-dev" {
		t.Fatalf("expected semver dev version, got %q", got)
	}
}
