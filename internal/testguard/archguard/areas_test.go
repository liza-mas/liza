package archguard

import "strings"

// areas lists the nine area values in run order. Written once by the S1 guard
// unit and never edited afterwards: area files change, this list does not.
var areas = []*Area{
	&areaA1, &areaA2, &areaA3, &areaA4, &areaA5, &areaA6, &areaA7, &areaA8, &areaA9,
}

// areaFilePrefix is the repo-relative directory of the area data files.
const areaFilePrefix = "internal/testguard/archguard/"

// areaFile returns the repo-relative slash path of the data file of an area.
func areaFile(id AreaID) string {
	return areaFilePrefix + "area_" + strings.ToLower(string(id)) + "_test.go"
}
