package main

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
)

var (
	ErrCastFailure                  = errors.New("failed to cast to hinted type")
	ErrFieldTagDeterminationFailure = errors.New("failed to determine field/tag isa")
)

type ParseResult struct {
	Timestamp time.Time
	Fields    map[string]any
	Tags      map[string]string
}

func MsgParse(msg map[string]any) (ParseResult, error) {
	retv := ParseResult{
		Timestamp: time.Now().UTC(),
		Fields:    make(map[string]any),
		Tags:      make(map[string]string),
	}

	var errs error
	for k, v := range msg {
		if k == "at" || k == "ts" || k == "time" {
			// support for other formats tracked at: https://github.com/cdzombak/mqtt2influxdb/issues/3
			if s, ok := v.(string); ok {
				result, err := time.Parse(time.RFC3339, s)
				if err != nil {
					errs = multierror.Append(errs, fmt.Errorf("failed to parse timestamp key %s (= %v) as RFC3339 time: %w", k, v, err))
				} else {
					retv.Timestamp = result
				}
			} else {
				errs = multierror.Append(errs, fmt.Errorf("timestamp key %s (= %v) is not a string: %w", k, v, ErrCastFailure))
			}
			continue
		}

		var additionalTags map[string]string
		var additionalFields map[string]any

		if k == "t" || k == "tags" {
			pr, err := parseKV(parseContext{
				isa: IsTag,
			}, "", v)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				additionalTags = pr.Tags
			}
		} else if k == "f" || k == "fields" {
			pr, err := parseKV(parseContext{
				isa: IsField,
			}, "", v)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				additionalFields = pr.Fields
			}
		} else if strings.HasPrefix(k, "t_") {
			pr, err := parseKV(parseContext{
				isa: IsTag,
			}, k[2:], v)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				additionalTags = pr.Tags
			}
		} else if strings.HasPrefix(k, "f_") {
			pr, err := parseKV(parseContext{
				isa: IsField,
			}, k[2:], v)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				additionalFields = pr.Fields
			}
		} else {
			pr, err := parseKV(parseContext{}, k, v)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				additionalFields = pr.Fields
				additionalTags = pr.Tags
			}
		}

		if additionalTags != nil {
			maps.Copy(retv.Tags, additionalTags)
		}
		if additionalFields != nil {
			maps.Copy(retv.Fields, additionalFields)
		}
	}

	return retv, errs
}

func SinglePayloadParse(fieldName string, payload string) (ParseResult, error) {
	retv := ParseResult{
		Timestamp: time.Now().UTC(),
		Fields:    make(map[string]any),
		Tags:      make(map[string]string),
	}

	pr, err := parseKV(parseContext{isa: IsField}, fieldName, payload)
	if err != nil {
		return retv, err
	}
	maps.Copy(retv.Fields, pr.Fields)

	return retv, nil
}

type parseContext struct {
	isa  FieldTagHint
	path []string
}

func parseKV(ctx parseContext, k string, v any) (ParseResult, error) {
	retv := ParseResult{
		Fields: make(map[string]any),
		Tags:   make(map[string]string),
	}

	myPath := append(ctx.path, k)
	myPathCanonical, err := Canonicalize(myPath)
	if err != nil {
		return retv, fmt.Errorf("failed to canonicalize path for %s: %w", strings.Join(myPath, "."), err)
	}
	myIsa := ctx.isa
	if myIsa == FieldTagUnhinted {
		myIsa = FieldTagHintFor(myPathCanonical)
	}

	if _, ok := v.([]any); ok {
		var errs error
		for i, item := range v.([]any) {
			pr, err := parseKV(parseContext{
				isa:  myIsa,
				path: myPath,
			}, strconv.Itoa(i), item)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("failed processing %s[%d]: %w", myPathCanonical, i, err))
			} else {
				maps.Copy(retv.Fields, pr.Fields)
				maps.Copy(retv.Tags, pr.Tags)
			}
		}
		return retv, errs
	}

	if _, ok := v.(map[string]any); ok {
		var errs error
		for subk, subv := range v.(map[string]any) {
			pr, err := parseKV(parseContext{
				isa:  myIsa,
				path: myPath,
			}, subk, subv)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("failed processing %s.%s: %w", myPathCanonical, subk, err))
			} else {
				maps.Copy(retv.Fields, pr.Fields)
				maps.Copy(retv.Tags, pr.Tags)
			}
		}
		return retv, errs
	}

	switch myIsa {
	case FieldTagUnhinted:
		return retv, fmt.Errorf("%s has unknown field/tag status: %w", myPathCanonical, ErrFieldTagDeterminationFailure)
	case IsTag:
		retv.Tags[string(myPathCanonical)] = fmt.Sprintf("%v", v)
	case IsField:
		var parsedV any
		switch TypeHintFor(myPathCanonical) {
		case TypeUnhinted:
			if i, ok := v.(int); ok && DefaultNumbersToFloat() {
				parsedV = float64(i)
			} else {
				parsedV = v
			}
		case HintedString:
			parsedV = fmt.Sprintf("%v", v)
		case HintedInt:
			if s, ok := v.(string); ok {
				i, err := strconv.Atoi(s)
				if err != nil {
					return retv, fmt.Errorf("failed to parse %s (= %v) to int: %w", myPathCanonical, v, ErrCastFailure)
				}
				parsedV = i
			} else if d, ok := v.(float64); ok {
				parsedV = int(math.Round(d))
			} else if i, ok := v.(int); ok {
				parsedV = i
			} else if b, ok := v.(bool); ok {
				if b {
					parsedV = 1
				} else {
					parsedV = 0
				}
			} else {
				return retv, fmt.Errorf("failed to convert %s (= %v) to int: %w", myPathCanonical, v, ErrCastFailure)
			}
		case HintedDouble:
			if s, ok := v.(string); ok {
				d, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return retv, fmt.Errorf("failed to parse %s (= %v) to double: %w", myPathCanonical, v, ErrCastFailure)
				}
				parsedV = d
			} else if d, ok := v.(float64); ok {
				parsedV = d
			} else if i, ok := v.(int); ok {
				parsedV = float64(i)
			} else if b, ok := v.(bool); ok {
				if b {
					parsedV = 1.0
				} else {
					parsedV = 0.0
				}
			} else {
				return retv, fmt.Errorf("failed to convert %s (= %v) to double: %w", myPathCanonical, v, ErrCastFailure)
			}
		case HintedBool:
			if s, ok := v.(string); ok {
				b, err := ParseBool(s)
				if err != nil {
					return retv, fmt.Errorf("failed to parse %s (= %v) to bool: %w", myPathCanonical, v, ErrCastFailure)
				}
				parsedV = b
			} else if i, ok := v.(int); ok && i >= 0 {
				if i == 0 {
					parsedV = false
				} else {
					parsedV = true
				}
			} else if b, ok := v.(bool); ok {
				parsedV = b
			} else {
				return retv, fmt.Errorf("failed to convert %s (= %v) to bool: %w", myPathCanonical, v, ErrCastFailure)
			}
		}
		retv.Fields[string(myPathCanonical)] = parsedV
	default:
		panic("unhandled default case")
	}

	return retv, nil
}

func ParseBool(str string) (bool, error) {
	switch strings.ToLower(str) {
	case "1", "t", "true", "on", "online":
		return true, nil
	case "0", "f", "false", "off", "offline":
		return false, nil
	}
	return false, fmt.Errorf("failed to parse '%s' as bool", str)
}
