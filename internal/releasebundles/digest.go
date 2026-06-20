package releasebundles

import "regexp"

var sha256DigestPattern = regexp.MustCompile(`^sha256:[A-Fa-f0-9]{64}$`)

func IsSHA256Digest(digest string) bool {
	return sha256DigestPattern.MatchString(digest)
}
