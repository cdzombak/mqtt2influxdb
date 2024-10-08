package main

import (
	"fmt"
	"strings"

	"mqtt2influxdb/cache"
)

var (
	canonicalizedKeysCache = cache.New[CanonicalizedKey]()
)

type CanonicalizedKey string

func Canonicalize(path []string) (CanonicalizedKey, error) {
	cacheKey := strings.Join(path, "|ck|")
	if cached, ok := canonicalizedKeysCache.Get(cacheKey); ok {
		return cached, nil
	}

	retv := ""
	for i, k := range path {
		key := strings.ToLower(k)
		if key == "" {
			continue
		}
		if key == "time" || key == "_measurement" || key == "_field" {
			return "", fmt.Errorf("invalid key: %s", key)
		}
		for _, c := range key {
			if c <= 31 {
				// ASCII control chars
				retv += "_"
				continue
			}
			switch c {
			case '=':
				fallthrough
			case '#':
				retv += "_"
			default:
				retv += string(c)
			}
		}
		if i != len(path)-1 {
			retv += "."
		}
	}

	canonicalizedKeysCache.Set(cacheKey, CanonicalizedKey(retv))
	return CanonicalizedKey(retv), nil
}
