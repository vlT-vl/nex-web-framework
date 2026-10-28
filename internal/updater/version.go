package updater

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tagPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)-R(\d{2})(\d{2})(\d{2})$`)
var versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
var buildPattern = regexp.MustCompile(`^R(\d{2})(\d{2})(\d{2})$`)

func ParseTag(tag string) (version, build string, ok bool) {
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", "", false
	}
	if _, valid := parseBuild("R" + m[4] + m[5] + m[6]); !valid {
		return "", "", false
	}
	return m[1] + "." + m[2] + "." + m[3], "R" + m[4] + m[5] + m[6], true
}

func Compare(aVersion, aBuild, bVersion, bBuild string) int {
	a := parseVersion(aVersion)
	b := parseVersion(bVersion)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	ad, aok := parseBuild(aBuild)
	bd, bok := parseBuild(bBuild)
	if aok && bok {
		if ad.Before(bd) {
			return -1
		}
		if ad.After(bd) {
			return 1
		}
		return 0
	}
	if aok {
		return 1
	}
	if bok {
		return -1
	}
	return strings.Compare(aBuild, bBuild)
}

func parseVersion(value string) [3]uint64 {
	m := versionPattern.FindStringSubmatch(strings.TrimPrefix(value, "v"))
	if m == nil {
		return [3]uint64{}
	}
	var result [3]uint64
	for i := range result {
		result[i], _ = strconv.ParseUint(m[i+1], 10, 64)
	}
	return result
}

func parseBuild(value string) (time.Time, bool) {
	m := buildPattern.FindStringSubmatch(value)
	if m == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	year, _ := strconv.Atoi(m[3])
	result := time.Date(2000+year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if result.Day() != day || int(result.Month()) != month || result.Year() != 2000+year {
		return time.Time{}, false
	}
	return result, true
}
