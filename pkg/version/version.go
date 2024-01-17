package version

import "strings"

var version string = "dev"

func Full() string {
	return version
}

func Display() string {
	if strings.Contains(version, ".") {
		return version
	} else if len(version) > 7 {
		return version[:7]
	} else {
		return version
	}
}
