package google

var aspectRatioResolutions = map[string]string{
	"1:1":  "1024x1024",
	"3:4":  "768x1024",
	"4:3":  "1024x768",
	"9:16": "576x1024",
	"16:9": "1024x576",
}

func mapAspectRatioToResolution(aspectRatio string) string {
	if res, ok := aspectRatioResolutions[aspectRatio]; ok {
		return res
	}
	return aspectRatio
}
