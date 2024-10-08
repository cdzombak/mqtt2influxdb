package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"mqtt2influxdb/cache"
)

type FieldTagHint int

const (
	FieldTagUnhinted FieldTagHint = iota
	IsField
	IsTag
)

var (
	fieldTagHintCache = cache.New[FieldTagHint]()
)

func FieldTagHintFor(s CanonicalizedKey) FieldTagHint {
	if cached, ok := fieldTagHintCache.Get(string(s)); ok {
		return cached
	}

	envResult := strings.ToLower(os.Getenv(
		fmt.Sprintf("M2I_%s_ISA",
			strings.ToUpper(string(s)),
		)))

	retv := FieldTagUnhinted
	switch envResult {
	case "field":
		retv = IsField
	case "tag":
		retv = IsTag
	}

	fieldTagHintCache.Set(string(s), retv)
	return retv
}

type HintedType int

const (
	TypeUnhinted HintedType = iota
	HintedInt
	HintedDouble
	HintedString
	HintedBool
)

var (
	typeHintCache = cache.New[HintedType]()
)

func TypeHintFor(s CanonicalizedKey) HintedType {
	if cached, ok := typeHintCache.Get(string(s)); ok {
		return cached
	}

	envResult := strings.ToLower(os.Getenv(
		fmt.Sprintf("M2I_%s_TYPE",
			strings.ToUpper(string(s)),
		)))

	retv := TypeUnhinted
	switch envResult {
	case "int":
		retv = HintedInt
	case "float":
		fallthrough
	case "double":
		retv = HintedDouble
	case "string":
		retv = HintedString
	case "bool":
		retv = HintedBool
	}

	typeHintCache.Set(string(s), retv)
	return retv
}

var (
	defaultNumbersToFloatMu       sync.Mutex
	didCacheDefaultNumbersToFloat bool
	defaultNumbersToFloatCache    bool
)

func DefaultNumbersToFloat() bool {
	defaultNumbersToFloatMu.Lock()
	defer defaultNumbersToFloatMu.Unlock()

	if didCacheDefaultNumbersToFloat {
		return defaultNumbersToFloatCache
	}

	b, err := strconv.ParseBool(os.Getenv("DEFAULT_NUMBERS_TO_FLOAT"))
	if err != nil && os.Getenv("DEFAULT_NUMBERS_TO_FLOAT") != "" {
		log.Printf("warning: failed to parse DEFAULT_NUMBERS_TO_FLOAT (= %s): %s", os.Getenv("DEFAULT_NUMBERS_TO_FLOAT"), err)
	}

	defaultNumbersToFloatCache = b
	didCacheDefaultNumbersToFloat = true
	return b
}
