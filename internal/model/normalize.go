package model

import "strings"

// NormalizeModel strips a trailing -YYYYMMDD snapshot suffix.
func NormalizeModel(id string) string {
	i := strings.LastIndexByte(id, '-')
	if i < 0 || i == len(id)-1 {
		return id
	}
	suffix := id[i+1:]
	if len(suffix) != 8 {
		return id
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return id
		}
	}
	return id[:i]
}
