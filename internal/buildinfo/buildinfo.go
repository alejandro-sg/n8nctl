package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

const DevelopmentVersion = "0.1.0-dev"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	BuiltBy = "Alejandro Sierra"
)

var (
	pseudoVersionNoTagPattern      = regexp.MustCompile(`^0\.0\.0-\d{14}-[0-9a-f]{12,}$`)
	pseudoVersionTaggedBasePattern = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)-0\.\d{14}-[0-9a-f]{12,}$`)
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	BuiltBy string `json:"builtBy"`
}

func Current() Info {
	info := Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		BuiltBy: BuiltBy,
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		if info.Version == "" || info.Version == "dev" {
			info.Version = DevelopmentVersion
		}
		return info
	}

	settings := buildSettings(buildInfo)

	if info.Version == "" || info.Version == "dev" {
		info.Version = versionFromBuildInfo(buildInfo, settings)
	}
	if info.Commit == "" || info.Commit == "none" {
		info.Commit = settings["vcs.revision"]
		if info.Commit == "" {
			info.Commit = "none"
		}
	}
	if info.Date == "" || info.Date == "unknown" {
		info.Date = settings["vcs.time"]
		if info.Date == "" {
			info.Date = "unknown"
		}
	}
	if info.BuiltBy == "" || info.BuiltBy == "unknown" {
		info.BuiltBy = "Alejandro Sierra"
	}

	return info
}

func buildSettings(info *debug.BuildInfo) map[string]string {
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	return settings
}

func versionFromBuildInfo(info *debug.BuildInfo, settings map[string]string) string {
	version := info.Main.Version
	switch version {
	case "", "(devel)":
		version = DevelopmentVersion
	default:
		if normalized, ok := normalizePseudoVersion(version); ok {
			version = normalized
		} else {
			version = strings.TrimPrefix(version, "v")
		}
	}

	if settings["vcs.modified"] == "true" {
		version = appendBuildMetadata(version, "dirty")
	}

	return version
}

func normalizePseudoVersion(version string) (string, bool) {
	version = strings.TrimPrefix(version, "v")
	version = strings.SplitN(version, "+", 2)[0]

	if pseudoVersionNoTagPattern.MatchString(version) {
		return DevelopmentVersion, true
	}

	if matches := pseudoVersionTaggedBasePattern.FindStringSubmatch(version); len(matches) == 2 {
		return matches[1] + "-dev", true
	}

	return "", false
}

func appendBuildMetadata(version, metadata string) string {
	if strings.Contains(version, "+") {
		if strings.HasSuffix(version, "."+metadata) || strings.HasSuffix(version, "+"+metadata) {
			return version
		}
		return version + "." + metadata
	}
	return version + "+" + metadata
}
