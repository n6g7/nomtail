package version

import "strings"

var Version string = "dev"

func Display() string {
	if strings.Contains(Version, ".") {
		return Version
	} else if len(Version) > 7 {
		return Version[:7]
	} else {
		return Version
	}
}
